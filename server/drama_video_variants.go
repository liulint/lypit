package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/gin-gonic/gin"
)

type DramaVideoOptions struct {
	VendorID   string `json:"vendor_id"`
	Model      string `json:"model"`
	Resolution string `json:"resolution"`
}
type DramaVideoInfo struct {
	DramaVideoOptions
	Vendor  string `json:"vendor"`
	Created string `json:"created"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
}

func dramaProductionResolution(p *DramaProduction) string {
	if p != nil && p.VideoOptions != nil {
		return p.VideoOptions.Resolution
	}
	return "480p"
}

func videoResolution(v string) string {
	if v == "" {
		return "480p"
	}
	return v
}

// Conservative adapter capabilities, separate from pricing and creator profiles.
// https://api.volcengine.com/api-docs/view?action=CreateContentsGenerationsTasks&serviceCode=ark&version=2024-01-01
// Reviewed 2026-11-08. Fast/Mini do not advertise 1080p; 4K is not exposed here.
func dramaVideoResolutions(model string) []string {
	m := strings.ToLower(model)
	if !strings.Contains(m, "seedance") {
		return nil
	}
	if strings.Contains(m, "fast") || strings.Contains(m, "mini") {
		return []string{"480p", "720p"}
	}
	if strings.Contains(m, "seedance-2") || strings.Contains(m, "seedance-1-0-pro") || strings.Contains(m, "seedance-1.0-pro") {
		return []string{"480p", "720p", "1080p"}
	}
	return nil
}

func validateDramaVideoOptions(cfg Settings, p DramaProduction, o DramaVideoOptions) error {
	if !contains(dramaVideoResolutions(o.Model), o.Resolution) && cfg.Mode != "mock" {
		return errors.New("所选模型不支持此清晰度")
	}
	if cfg.Mode == "mock" && !contains([]string{"480p", "720p", "1080p"}, o.Resolution) {
		return errors.New("清晰度无效")
	}
	pol := dramaProfile(cfg, p.Mode, "")
	if !pol.Known || p.Duration < float64(pol.Min) || p.Duration > float64(pol.Max) {
		return errors.New("镜长不在所选模型支持范围内，请先修订分镜")
	}
	if cfg.Mode != "mock" {
		if strings.Contains(strings.ToLower(o.Model), "seedance-1") {
			return errors.New("当前试版要求同步声音，请选择支持同步声音的 Seedance 2 系列模型")
		}
		if p.Mode == "edit_video" {
			return errors.New("视频编辑不能作为保持输入不变的模型试版，请使用视频修改入口")
		}
	}
	return nil
}

func (s *Studio) dramaVideoConfig(cfg Settings, p DramaProduction) (Settings, error) {
	if p.VideoOptions == nil {
		return cfg, nil
	}
	if p.Kind != "video" {
		return cfg, errors.New("本次视频配置仅用于视频任务")
	}
	o := *p.VideoOptions
	if o.VendorID == "" || o.Model == "" || !contains([]string{"480p", "720p", "1080p"}, o.Resolution) {
		return cfg, errors.New("请选择厂商、模型和清晰度")
	}
	cfg.Video = Endpoint{VendorID: o.VendorID, Model: o.Model}
	ep, err := resolveEndpoint(cfg, "video")
	if err != nil {
		return cfg, err
	}
	if cfg.Mode != "mock" && ep.Transport != "api" {
		return cfg, errors.New("当前视频执行仅支持兼容API")
	}
	if err = s.validateCatalogBindings(cfg, "video"); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func (s *Studio) buildDramaVideoComparison(w DramaWork, p DramaProduction, cfg Settings) (DramaProduction, map[string]string, error) {
	if p.VideoOptions == nil {
		return p, nil, errors.New("试版须明确本次模型和清晰度")
	}
	m, err := s.dramaMediaByID(w.ID, p.CompareSource.ID)
	if err != nil || m.Kind != "video" || m.Entry != p.Entry || m.Hash != p.CompareSource.Hash || !s.dramaMediaCurrent(m) {
		return p, nil, errors.New("试版来源已变化，请重新选择")
	}
	if err = s.verifyDramaMedia(m); err != nil {
		return p, nil, err
	}
	j, err := s.dramaJob(m.TaskID)
	if err != nil || j.Production == nil {
		return p, nil, errors.New("旧版本缺少原始制作输入，请按当前分镜重新报价")
	}
	original := j.Production
	if original.Kind == "batch" {
		original = nil
		for i := range j.Production.Items {
			if j.Production.Items[i].Entry == p.Entry {
				original = &j.Production.Items[i]
				break
			}
		}
	}
	if original == nil || original.Kind != "video" || original.Prompt == "" || original.Mode == "edit_video" {
		return p, nil, errors.New("该版本没有可复用的生成输入，请按当前分镜重新报价")
	}
	copySpec, err := decodeBody[DramaProduction](jsonBytes(*original))
	if err != nil {
		return p, nil, err
	}
	copySpec.VideoOptions = p.VideoOptions
	copySpec.CompareSource = &m
	copySpec.Operation = "compare"
	copySpec.Entries = nil
	inputs := map[string]string{}
	for k, h := range j.Inputs {
		if !s.dramaDependencyCurrent(w.ID, k, h, true) {
			return p, nil, errConflict
		}
		inputs[k] = h
	}
	inputs["@video-edit:"+m.ID] = dramaEditSourceHash(m)
	for _, b := range copySpec.Bindings {
		if !s.dramaReferenceCurrent(w.ID, b.Media.ID, b.Media.Hash) || !s.dramaMediaCurrent(b.Media) {
			return p, nil, errConflict
		}
		inputs["@media:"+b.Media.ID] = b.Media.Hash
		if err = s.verifyDramaMedia(b.Media); err != nil {
			return p, nil, err
		}
	}
	if copySpec.Mode == "continue" {
		oldTask, e := s.store.task(m.TaskID)
		if e != nil {
			return p, nil, e
		}
		oldEP, e := resolveEndpoint(oldTask.Config, "video")
		newEP, ne := resolveEndpoint(cfg, "video")
		if e != nil || ne != nil || oldEP.VendorID != newEP.VendorID || oldEP.Model != newEP.Model {
			return p, nil, errors.New("续接任务不能跨模型或厂商复用原供应商任务，请先选择独立首帧方案")
		}
	}
	if err = validateDramaVideoOptions(cfg, copySpec, *p.VideoOptions); err != nil {
		return p, nil, err
	}
	return copySpec, inputs, nil
}

func (s *Studio) dramaVideoInfo(cfg Settings, p *DramaProduction) *DramaVideoInfo {
	ep, _ := resolveEndpoint(cfg, "video")
	i := &DramaVideoInfo{DramaVideoOptions: DramaVideoOptions{VendorID: ep.VendorID, Model: ep.Model, Resolution: "480p"}, Created: now()}
	if p.VideoOptions != nil {
		i.DramaVideoOptions = *p.VideoOptions
	}
	for _, v := range cfg.Vendors {
		if v.ID == ep.VendorID {
			i.Vendor = v.Name
		}
	}
	return i
}

func (s *Studio) decorateDramaVideos(all []DramaMedia) error {
	choices := map[string]string{}
	if len(all) == 0 {
		return nil
	}
	rows, err := s.store.db.Query("SELECT entry,media_id FROM drama_video_choices WHERE work_id=?", all[0].WorkID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var entry, id string
		if err = rows.Scan(&entry, &id); err != nil {
			rows.Close()
			return err
		}
		choices[entry] = id
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for n := range all {
		m := &all[n]
		if m.Kind != "video" {
			continue
		}
		if choices[m.Entry] == "" && m.Current {
			choices[m.Entry] = m.ID
		}
		m.Selected = choices[m.Entry] == m.ID
		if m.VideoInfo == nil && m.TaskID != "" {
			if t, e := s.store.task(m.TaskID); e == nil {
				m.VideoInfo = s.dramaVideoInfo(t.Config, &DramaProduction{})
				m.VideoInfo.Created = t.Created
				m.VideoInfo.Resolution = videoResolution(t.Input.VideoResolution)
			}
		}
	}
	return nil
}

// Freeze the previous display choice before appending a candidate. Workers only
// insert missing choices; they never overwrite a user's concurrent selection.
func (s *Studio) persistDramaMedia(m DramaMedia) error {
	choice := ""
	if m.Kind == "video" {
		all, err := s.dramaMedias(m.WorkID)
		if err != nil {
			return err
		}
		for _, v := range all {
			if v.Entry == m.Entry && v.Kind == "video" && v.Selected {
				choice = v.ID
				break
			}
		}
		if choice == "" {
			choice = m.ID
		}
	}
	return s.store.transaction(func(tx *sql.Tx) error {
		if choice != "" {
			if _, err := tx.Exec("INSERT INTO drama_video_choices(work_id,entry,media_id) VALUES(?,?,?) ON CONFLICT(work_id,entry) DO NOTHING", m.WorkID, m.Entry, choice); err != nil {
				return err
			}
		}
		if m.Kind == "video" {
			var raw []byte
			err := tx.QueryRow("SELECT body FROM drama_media WHERE id=? AND work_id=?", m.ID, m.WorkID).Scan(&raw)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if err == nil {
				var prior DramaMedia
				if err = json.Unmarshal(raw, &prior); err != nil {
					return err
				}
				if prior.Hash != m.Hash || prior.File != m.File {
					return errors.New("已发布视频与恢复结果不同，保留原版本并停止")
				}
				m.Approved = m.Approved || prior.Approved
				if m.VideoInfo == nil {
					m.VideoInfo = prior.VideoInfo
				}
			}
		}
		_, err := tx.Exec("INSERT INTO drama_media VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body", m.ID, m.WorkID, jsonBytes(m))
		return err
	})
}

func (s *Studio) dramaVideoVariantRoutes(g *gin.RouterGroup) {
	g.GET("/video-options", func(c *gin.Context) {
		cfg, err := s.store.settings()
		if err != nil {
			result(c, nil, err)
			return
		}
		type option struct {
			DramaVideoOptions
			Vendor      string        `json:"vendor"`
			Resolutions []string      `json:"resolutions"`
			Pricing     *ModelPricing `json:"pricing,omitempty"`
		}
		out := []option{}
		for _, v := range cfg.Vendors {
			if !supports(v, "video") || v.Type != "api" {
				continue
			}
			ids := []string{cfg.Video.Model, "doubao-seedance-2-0-260128", "doubao-seedance-2-0-fast-260128", "doubao-seedance-2-0-mini-260615", "doubao-seedance-2-5-260628"}
			secret, e := s.credential(Endpoint{VendorID: v.ID, KeyMode: v.KeyMode, KeyEnv: v.KeyEnv, CredentialID: v.CredentialID})
			if e == nil {
				s.catalogMu.Lock()
				cat := s.catalog[catalogKey(v, secret)]
				s.catalogMu.Unlock()
				for _, m := range cat.Models {
					if contains(m.Services, "video") {
						ids = append(ids, m.ID)
					}
				}
			}
			seen := map[string]bool{}
			for _, id := range ids {
				if seen[id] || !modelAllows(v, id, "video") || len(dramaVideoResolutions(id)) == 0 || strings.Contains(id, "seedance-1") {
					continue
				}
				seen[id] = true
				out = append(out, option{DramaVideoOptions: DramaVideoOptions{VendorID: v.ID, Model: id, Resolution: "480p"}, Vendor: v.Name, Resolutions: dramaVideoResolutions(id), Pricing: officialModelPricing(v, id)})
			}
		}
		result(c, gin.H{"options": out, "default": DramaVideoOptions{VendorID: cfg.Video.VendorID, Model: cfg.Video.Model, Resolution: "480p"}}, nil)
	})
	g.POST("/works/:id/media/:media/select", func(c *gin.Context) {
		var req struct {
			Hash string `json:"hash"`
			Base string `json:"base"`
		}
		if !bind(c, &req) {
			return
		}
		s.dramaMu.Lock()
		defer s.dramaMu.Unlock()
		all, err := s.dramaMedias(c.Param("id"))
		if err != nil {
			result(c, nil, err)
			return
		}
		var target *DramaMedia
		for i := range all {
			if all[i].ID == c.Param("media") {
				target = &all[i]
				break
			}
		}
		if target == nil || target.Kind != "video" || target.Hash != req.Hash || !target.Current {
			result(c, nil, errors.New("视频版本无效或来源已变化"))
			return
		}
		base := ""
		for _, m := range all {
			if m.Entry == target.Entry && m.Kind == "video" && m.Selected {
				base = m.ID
			}
		}
		if base != req.Base {
			result(c, nil, errConflict)
			return
		}
		if err = s.verifyDramaMedia(*target); err != nil {
			result(c, nil, err)
			return
		}
		target.Approved = true
		target.Selected = true
		err = s.store.transaction(func(tx *sql.Tx) error {
			var persisted string
			e := tx.QueryRow("SELECT media_id FROM drama_video_choices WHERE work_id=? AND entry=?", target.WorkID, target.Entry).Scan(&persisted)
			if e != nil && !errors.Is(e, sql.ErrNoRows) {
				return e
			}
			if e == nil && persisted != req.Base {
				return errConflict
			}
			if _, e = tx.Exec("INSERT INTO drama_video_choices VALUES(?,?,?) ON CONFLICT(work_id,entry) DO UPDATE SET media_id=excluded.media_id", target.WorkID, target.Entry, target.ID); e != nil {
				return e
			}
			_, e = tx.Exec("UPDATE drama_media SET body=? WHERE id=?", jsonBytes(target), target.ID)
			return e
		})
		result(c, target, err)
	})
}
