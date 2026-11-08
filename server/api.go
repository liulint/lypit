package main

import (
	"archive/zip"
	"database/sql"
	"errors"
	"io"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func (s *Studio) editProject(c *gin.Context) {
	var v struct {
		Revision    int     `json:"revision"`
		Title       string  `json:"title"`
		Idea        string  `json:"idea"`
		Script      string  `json:"script"`
		Style       string  `json:"style"`
		Assets      []Asset `json:"assets"`
		Archived    bool    `json:"archived"`
		AspectRatio *string `json:"aspect_ratio"`
		Duration    *int    `json:"duration"`
		AudioMode   *string `json:"audio_mode"`
	}
	if !bind(c, &v) {
		return
	}
	p, e := s.store.update(c.Param("id"), v.Revision, func(p *Project) error {
		if strings.TrimSpace(v.Title) == "" || len([]rune(v.Title)) > 80 || strings.TrimSpace(v.Idea) == "" || len(v.Idea) > 20000 || len(v.Script) > 50000 || len(v.Style) > 500 {
			return errors.New("标题、故事或风格内容无效")
		}
		if len(v.Assets) != len(p.Assets) {
			return errors.New("不能通过编辑增删已规划素材，请修改故事后重新分析")
		}
		aspect, duration := projectAspect(*p), projectDuration(*p)
		if v.AspectRatio != nil {
			aspect = *v.AspectRatio
		}
		if v.Duration != nil {
			duration = *v.Duration
		}
		if e := validateSpec(aspect, duration); e != nil {
			return e
		}
		mode := audioMode(*p)
		if v.AudioMode != nil {
			mode = *v.AudioMode
		}
		if e := validateAudioMode(mode); e != nil {
			return e
		}
		audioChanged := mode != audioMode(*p)
		specChanged := aspect != projectAspect(*p) || duration != projectDuration(*p)
		changed := specChanged || p.Idea != v.Idea || p.Style != v.Style || p.Script != v.Script
		for i, a := range v.Assets {
			old := &p.Assets[i]
			if a.ID != old.ID || a.Kind != old.Kind || strings.TrimSpace(a.Name) == "" || strings.TrimSpace(a.Description) == "" || len(a.Name) > 300 || len(a.Description) > 4000 {
				return errors.New("素材名称或描述无效")
			}
			if a.Name != old.Name || a.Description != old.Description {
				old.Name = a.Name
				old.Description = a.Description
				old.Master = nil
				old.Sheet = nil
				old.Views = []Image{}
				changed = true
			}
		}
		if p.Idea != v.Idea || specChanged {
			p.Analyzed = false
			p.Assets = []Asset{}
		}
		if p.Style != v.Style {
			for i := range p.Assets {
				p.Assets[i].Master = nil
				p.Assets[i].Sheet = nil
				p.Assets[i].Views = []Image{}
			}
		}
		if changed {
			p.AssetsLocked = false
			p.BoardsLocked = false
			p.PreviousShots = append(p.PreviousShots, p.Shots...)
			p.PlannedShots = nil
			p.PlanApproved = false
			p.PlanVersion = 0
			p.Shots = []Shot{}
			p.Output = ""
			p.Voice = ""
		}
		if audioChanged && !changed {
			p.PlannedShots = nil
			p.PlanApproved = false
			// Preserve visual work, but never reinterpret old narration as dialogue.
			p.BoardsLocked = false
			p.Voice, p.Output = "", ""
			for i := range p.Shots {
				p.Shots[i].Narration = ""
				p.Shots[i].Video = ""
				p.Shots[i].Approved = false
			}
		}
		p.AudioMode = mode
		p.Title = strings.TrimSpace(v.Title)
		p.Idea = v.Idea
		p.Script = v.Script
		if specChanged {
			p.Script = ""
		}
		p.Style = v.Style
		p.AspectRatio = aspect
		p.Duration = duration
		p.Archived = v.Archived
		return nil
	})
	result(c, p, e)
}
func (s *Studio) approveAsset(c *gin.Context) {
	var v struct {
		Revision int    `json:"revision"`
		Part     string `json:"part"`
	}
	if !bind(c, &v) {
		return
	}
	p, e := s.store.update(c.Param("id"), v.Revision, func(p *Project) error {
		if p.AssetsLocked {
			return errors.New("素材已锁定")
		}
		for i := range p.Assets {
			a := &p.Assets[i]
			if a.ID != c.Param("asset") {
				continue
			}
			var im *Image
			switch v.Part {
			case "master":
				im = a.Master
			case "sheet":
				im = a.Sheet
			default:
				return errors.New("未知素材类型")
			}
			if im == nil {
				return errors.New("请先生成素材")
			}
			if e := s.verifyImage(*im); e != nil {
				return e
			}
			im.Approved = true
			if v.Part == "sheet" {
				if len(a.Views) != 4 {
					return errors.New("设定图必须有四个完整视角")
				}
				for j := range a.Views {
					if e := s.verifyImage(a.Views[j]); e != nil {
						return e
					}
					a.Views[j].Approved = true
				}
			}
			return nil
		}
		return errors.New("素材不存在")
	})
	result(c, p, e)
}
func (s *Studio) lockProject(c *gin.Context, kind string) {
	var v struct {
		Revision int `json:"revision"`
	}
	if !bind(c, &v) {
		return
	}
	p, e := s.store.update(c.Param("id"), v.Revision, func(p *Project) error {
		if kind == "assets" {
			if e := s.assetsReady(*p); e != nil {
				return e
			}
			p.AssetsLocked = true
		} else {
			if !p.AssetsLocked {
				return errors.New("请先生成完整分镜")
			}
			if e := validateShotPlan(*p); e != nil {
				return e
			}
			if e := s.assetsReady(*p); e != nil {
				return e
			}
			for _, q := range p.Shots {
				if !q.Approved || q.Selected < 0 || q.Selected >= len(q.Candidates) {
					return errors.New("请逐镜选择并人工确认画面")
				}
				if e := s.verifyImage(q.Candidates[q.Selected]); e != nil {
					return e
				}
			}
			p.BoardsLocked = true
		}
		return nil
	})
	result(c, p, e)
}
func (s *Studio) editShot(c *gin.Context) {
	var v struct {
		Revision  int    `json:"revision"`
		Selected  int    `json:"selected"`
		Approved  bool   `json:"approved"`
		Narration string `json:"narration"`
		PlanTrial bool   `json:"plan_trial"`
	}
	if !bind(c, &v) {
		return
	}
	p, e := s.store.updateScoped(c.Param("id"), v.Revision, !v.PlanTrial, func(p *Project) error {
		if p.Archived {
			return errors.New("已归档项目只读")
		}
		if p.BoardsLocked && !v.PlanTrial {
			return errors.New("分镜已锁定，请修改故事后重新制作")
		}
		shots := p.Shots
		if v.PlanTrial {
			shots = p.PlannedShots
		}
		for i := range shots {
			q := &shots[i]
			if q.ID != c.Param("shot") {
				continue
			}
			if v.Selected < 0 || v.Selected >= len(q.Candidates) || len(v.Narration) > 3000 {
				return errors.New("选版或台词无效")
			}
			candidate := *q
			if v.PlanTrial && (q.ReviewStatus != "reviewed" || v.Narration != q.Narration) {
				return errors.New("试做须先在规划中审核并保存台词，确认画面时不能修改规划")
			}
			old := *q
			candidate.Narration = v.Narration
			if contentOverridden(old) && old.Narration != candidate.Narration {
				if err := validateShotContent(*p, candidate); err != nil {
					return err
				}
				candidate.ContentOverride = &ShotContentOverride{Version: 1, OriginalNarration: old.ContentOverride.OriginalNarration, OriginalSource: old.ContentOverride.OriginalSource, Hash: contentHash(candidate)}
			}
			if e := validateSpeech(*p, candidate); e != nil {
				return e
			}
			if e := s.verifyImage(q.Candidates[v.Selected]); e != nil {
				return e
			}
			if q.Selected != v.Selected || q.Narration != v.Narration {
				q.Video = ""
				if !v.PlanTrial {
					p.Voice = ""
				}
			}
			if old.Video != "" && old.Narration != v.Narration {
				p.PreviousShots = append(p.PreviousShots, old)
			}
			q.ContentOverride = candidate.ContentOverride
			q.Selected = v.Selected
			q.Approved = v.Approved
			q.Narration = v.Narration
			if !v.PlanTrial {
				p.Output = ""
			}
			return nil
		}
		return errors.New("镜头不存在")
	})
	result(c, p, e)
}
func (s *Studio) assetsReady(p Project) error {
	if !p.Analyzed || len(p.Assets) == 0 {
		return errors.New("请先分析故事")
	}
	for _, a := range p.Assets {
		if a.Master == nil || !a.Master.Approved {
			return errors.New("请人工确认全部角色主图、场景和道具")
		}
		if e := s.verifyImage(*a.Master); e != nil {
			return e
		}
		if a.Kind == "character" {
			if a.Sheet == nil || !a.Sheet.Approved || len(a.Views) != 4 {
				return errors.New("请制作并确认角色2×2设定图")
			}
			if e := s.verifyImage(*a.Sheet); e != nil {
				return e
			}
			for _, v := range a.Views {
				if !v.Approved {
					return errors.New("角色视角尚未确认")
				}
				if e := s.verifyImage(v); e != nil {
					return e
				}
			}
		}
	}
	return nil
}
func (s *Studio) verifyImage(im Image) error {
	if !safeMedia(im.File) {
		return errors.New("素材路径无效")
	}
	b, e := os.ReadFile(s.media(im.File))
	if e != nil || digest(b) != im.Hash {
		return errors.New("素材丢失或文件已改变，请重新制作")
	}
	return nil
}
func (s *Studio) getSettings(c *gin.Context) {
	v, e := s.store.settings()
	if e != nil {
		result(c, nil, e)
		return
	}
	present := map[string]bool{}
	statuses := map[string]any{}
	for _, vendor := range v.Vendors {
		status := s.vendorStatus(vendor)
		present[vendor.ID] = status["ready"].(bool)
		statuses[vendor.ID] = status
	}
	caps := map[string][]string{}
	for _, vendor := range v.Vendors {
		caps[vendor.ID] = vendorCapabilities(vendor)
	}
	c.JSON(200, gin.H{"settings": v, "credentials": present, "vendor_status": statuses, "vendor_capabilities": caps, "video_duration_policy": videoDurationPolicy(v)})
}

var envPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func validEndpoint(ep Endpoint) error {
	u, e := url.Parse(ep.Base)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("远程接口地址必须是无凭据、无查询参数的 HTTPS 地址")
	}
	if len(ep.Model) > 300 || (ep.KeyMode != "local" && !envPattern.MatchString(ep.KeyEnv)) {
		return errors.New("模型ID或环境变量名称无效")
	}
	return nil
}
func (s *Studio) putSettings(c *gin.Context) {
	var input struct {
		Settings
		ImageLimit *int              `json:"image_concurrency"`
		VideoLimit *int              `json:"video_concurrency"`
		APIKeys    map[string]string `json:"api_keys"`
		Section    string            `json:"section"`
	}
	if !bind(c, &input) {
		return
	}
	v := input.Settings
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	old, e := s.store.settings()
	if e != nil {
		result(c, nil, e)
		return
	}
	if v.Version != old.Version {
		result(c, nil, errConflict)
		return
	}
	// Prompt changes have a separate version-checked endpoint; service and vendor saves retain them.
	v.PromptTemplates = old.PromptTemplates
	switch input.Section {
	case "vendors":
		v.Mode, v.Text, v.Image, v.Video, v.Voice = old.Mode, old.Text, old.Image, old.Video, old.Voice
		v.Music = old.Music
		v.ImageConcurrency, v.VideoConcurrency = old.ImageConcurrency, old.VideoConcurrency
	case "services":
		v.Vendors = old.Vendors
		input.APIKeys = nil
	case "": // Older clients retain their version-checked full update contract.
	default:
		result(c, nil, errors.New("配置页面类型无效"))
		return
	}
	// Older clients omit these fields; retain the saved limits. Vendor edits
	// cannot change service settings, even when their payload contains limits.
	v.ImageConcurrency, v.VideoConcurrency = old.ImageConcurrency, old.VideoConcurrency
	if input.Section != "vendors" {
		if input.ImageLimit != nil {
			v.ImageConcurrency = *input.ImageLimit
		}
		if input.VideoLimit != nil {
			v.VideoConcurrency = *input.VideoLimit
		}
	}
	if v.ImageConcurrency < 1 || v.ImageConcurrency > maxGenerationConcurrency || v.VideoConcurrency < 1 || v.VideoConcurrency > maxGenerationConcurrency {
		result(c, nil, errors.New("图片与视频并发数须为1～8的整数"))
		return
	}
	if v.Mode != "mock" && v.Mode != "remote" {
		result(c, nil, errors.New("请选择本地模拟或真实服务模式"))
		return
	}
	if e = validateRegistry(&v); e != nil {
		result(c, nil, e)
		return
	}
	if e = s.validateCatalogBindings(v); e != nil {
		result(c, nil, e)
		return
	}
	// Service edits retain the saved vendor registry and cannot change credentials.
	// Incomplete credentials must not prevent disabling an optional service.
	if input.Section != "services" {
		if e = s.prepareCredentials(&v, old, input.APIKeys); e != nil {
			result(c, nil, e)
			return
		}
	}
	v.Version++
	_, e = s.store.db.Exec("UPDATE settings SET body=? WHERE id=1", jsonBytes(v))
	result(c, v, e)
}
func (s *Studio) systemInfo(c *gin.Context) {
	c.JSON(200, s.systemSnapshot())
}
func (s *Studio) exportProject(c *gin.Context) {
	p, e := s.store.project(c.Param("id"))
	if e != nil {
		result(c, nil, e)
		return
	}
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", "attachment; filename=lypit-"+p.ID+".zip")
	w := zip.NewWriter(c.Writer)
	defer w.Close()
	write := func(name string, b []byte) {
		out, e := w.Create(name)
		if e == nil {
			_, _ = out.Write(b)
		}
	}
	write("project.json", jsonBytes(p))
	tasks, _ := s.store.tasks(p.ID)
	write("tasks.json", jsonBytes(tasks))
	write("BRIEF.md", []byte("# "+p.Title+"\n\n"+p.Idea+"\n\n模式："+p.Mode+"\n"))
	write("TREATMENT.md", []byte("# 导演方案\n\n风格："+p.Style+"\n\n"+p.Script))
	write("README.txt", []byte("项目与采用素材归档。mock 表示程序生成的示意图和静音视频，非真实AI生成。任务不含凭据。本地合成中间文件留在本机数据目录，不包含在此ZIP。"))
	seen := map[string]bool{}
	add := func(file string) {
		if !safeMedia(file) || seen[file] {
			return
		}
		seen[file] = true
		f, e := os.Open(s.media(file))
		if e != nil {
			return
		}
		defer f.Close()
		o, e := w.Create("media/" + file)
		if e == nil {
			_, _ = io.Copy(o, f)
		}
	}
	for _, a := range p.Assets {
		if a.Master != nil {
			add(a.Master.File)
		}
		if a.Sheet != nil {
			add(a.Sheet.File)
		}
		for _, im := range a.Views {
			add(im.File)
		}
	}
	for _, q := range append(append(append([]Shot{}, p.Shots...), p.PreviousShots...), p.PlannedShots...) {
		for _, im := range q.Candidates {
			add(im.File)
			if im.ReferencePlan != nil {
				for _, src := range im.ReferencePlan.Sources {
					add(src.Image.File)
				}
				for _, board := range im.ReferencePlan.Boards {
					add(board.File)
				}
			}
		}
		add(q.Video)
	}
	add(p.Output)
	add(p.Voice)
}
func expired(q Quote) bool {
	t, e := time.Parse(time.RFC3339Nano, q.Expires)
	return e != nil || time.Now().After(t)
}
func (s *Studio) saveTaskInput(t Task) error {
	t.Updated = now()
	_, e := s.store.db.Exec("UPDATE tasks SET body=?,input=? WHERE id=?", jsonBytes(t), jsonBytes(t.Input), t.ID)
	return e
}
func (s *Studio) commitTask(t Task) error {
	if t.Stage == "shot_rewrite" {
		if t.Rewrite == nil {
			return errors.New("改写结果缺失")
		}
		t.Status, t.Progress, t.Message, t.Updated = "completed", 100, "改写建议已生成，等待对比确认；当前分镜未改变", now()
		return s.store.saveTask(t)
	}
	trialInput := t.Input
	return s.store.transaction(func(tx *sql.Tx) error {
		var b []byte
		if e := tx.QueryRow("SELECT body FROM projects WHERE id=?", t.ProjectID).Scan(&b); e != nil {
			return e
		}
		p, e := decodeBody[Project](b)
		if e != nil {
			return e
		}
		if t.ShotInputHash != "" {
			merged, err := mergeSingleShotTask(p, t)
			if err != nil {
				return err
			}
			t.Input = merged
		} else if t.PlanTrial {
			if p.Revision != t.Input.Revision {
				return errConflict
			}
			found := false
			for i, current := range p.PlannedShots {
				if current.ID != t.ShotID {
					continue
				}
				for _, generated := range t.Input.Shots {
					if generated.ID == t.ShotID {
						p.PlannedShots[i] = generated
						found = true
						break
					}
				}
			}
			if !found {
				return errConflict
			}
			t.Input = p
		} else if t.Stage == "boards" || t.Stage == "shot_video" {
			merged, err := mergeProgressiveTask(p, t)
			if err != nil {
				return err
			}
			t.Input = merged
		} else if p.Revision != t.Input.Revision {
			return errConflict
		}
		t.Input.Revision = p.Revision + 1
		t.Input.Updated = now()
		if e = s.store.saveProject(tx, t.Input); e != nil {
			return e
		}
		if t.PlanTrial {
			t.Input = trialInput
		}
		t.Status = "completed"
		t.Progress = 100
		t.Message = "制作完成，等待人工审核"
		t.Updated = now()
		_, e = tx.Exec("UPDATE tasks SET status=?,body=?,input=? WHERE id=?", t.Status, jsonBytes(t), jsonBytes(t.Input), t.ID)
		return e
	})
}
