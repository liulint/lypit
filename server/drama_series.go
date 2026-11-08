package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

// Revisions are append-only. An episode owns copies of its chosen references and
// never reads a mutable series revision or another episode's current state.
type DramaSeries struct {
	ID       string       `json:"id"`
	Version  int          `json:"version"`
	Hash     string       `json:"hash"`
	Title    string       `json:"title"`
	Idea     string       `json:"idea"`
	Settings string       `json:"settings"`
	Genre    string       `json:"genre"`
	Form     string       `json:"form"`
	Aspect   string       `json:"aspect"`
	Duration int          `json:"duration"`
	Media    []DramaMedia `json:"media"`
	Created  string       `json:"created"`
}
type DramaEpisodePlan struct {
	Theme        string `json:"theme"`
	Requirements string `json:"requirements"`
}
type DramaContinuity struct {
	ScriptVersion int    `json:"script_version"`
	WorkID        string `json:"work_id"`
	Number        int    `json:"number"`
	Version       int    `json:"version"`
	Hash          string `json:"hash"`
	Text          string `json:"text"`
	Confirmed     bool   `json:"confirmed"`
	ScriptHash    string `json:"script_hash"`
	Current       bool   `json:"current"`
}
type DramaEpisodeContext struct {
	Series   DramaSeries       `json:"series"`
	Previous []DramaContinuity `json:"previous"`
	Media    []DramaMedia      `json:"media"`
}

func dramaEpisodeCode(w DramaWork) string {
	n := w.EpisodeNumber
	if n == 0 {
		n = 1
	}
	return fmt.Sprintf("EP%03d", n)
}
func dramaWorkPath(w DramaWork, rel string) string {
	return strings.ReplaceAll(rel, "EP001", dramaEpisodeCode(w))
}
func (s *Studio) dramaMediaPath(id, tail string) string {
	w, e := s.dramaWork(id)
	if e != nil {
		return "剧集/EP001/制作成果/" + tail
	}
	return "剧集/" + dramaEpisodeCode(w) + "/制作成果/" + tail
}
func dramaEpisodeHash(w DramaWork) string {
	return digest(jsonBytes(struct {
		Title, Idea string
		Plan        DramaEpisodePlan
		Context     *DramaEpisodeContext
	}{w.Title, w.Idea, w.Plan, w.Context}))
}
func seriesHash(v DramaSeries) string { v.Hash = ""; return digest(jsonBytes(v)) }
func (s *Studio) dramaSeries(id string, version int) (DramaSeries, error) {
	var v DramaSeries
	var b []byte
	if !dramaID.MatchString(id) {
		return v, errors.New("系列标识无效")
	}
	var e error
	if version == 0 {
		e = s.store.db.QueryRow("SELECT body FROM drama_series_versions WHERE series_id=? ORDER BY version DESC LIMIT 1", id).Scan(&b)
	} else {
		e = s.store.db.QueryRow("SELECT body FROM drama_series_versions WHERE series_id=? AND version=?", id, version).Scan(&b)
	}
	if e == nil {
		e = json.Unmarshal(b, &v)
	}
	return v, e
}
func validateSeries(v *DramaSeries) error {
	if strings.TrimSpace(v.Title) == "" || len(v.Title) > 320 || strings.TrimSpace(v.Idea) == "" || len(v.Idea) > 20000 || len(v.Settings) > 60000 || len(v.Form) > 2000 || len(v.Genre) > 1000 {
		return errors.New("请填写系列名称与创作意图，并检查内容长度")
	}
	if v.Aspect == "" {
		v.Aspect = "9:16"
	}
	if v.Duration == 0 {
		v.Duration = 33
	}
	if (v.Aspect != "9:16" && v.Aspect != "16:9") || v.Duration < 1 || v.Duration > 600 {
		return errors.New("画幅须为9:16或16:9，每集时长须为1～600秒")
	}
	if len(v.Media) > 40 {
		return errors.New("每版共用素材最多40份，请选择本系列实际需要的素材")
	}
	return nil
}
func (s *Studio) resolveSeriesMedia(id string, refs []DramaMedia) ([]DramaMedia, error) {
	out := []DramaMedia{}
	seen := map[string]bool{}
	for _, ref := range refs {
		if seen[ref.ID] {
			return nil, errors.New("共用素材重复")
		}
		seen[ref.ID] = true
		w, e := s.dramaWork(ref.WorkID)
		if e != nil {
			return nil, e
		}
		if w.ID != id && w.SeriesID != id {
			return nil, errors.New("共用素材须来自本系列素材库或分集")
		}
		all, e := s.dramaMedias(ref.WorkID)
		if e != nil {
			return nil, e
		}
		found := false
		for _, m := range all {
			if m.ID == ref.ID && m.Hash == ref.Hash && m.Approved && m.Current && (m.Kind == "image" || m.Kind == "audio" || m.Kind == "voice" || m.Kind == "music") {
				if e = s.verifyDramaMedia(m); e != nil {
					return nil, e
				}
				out = append(out, m)
				found = true
				break
			}
		}
		if !found {
			return nil, errors.New("素材未确认、来源已变化或版本不匹配，请重新选择")
		}
	}
	return out, nil
}
func (s *Studio) dramaContinuity(id string) (DramaContinuity, error) {
	v := DramaContinuity{WorkID: id}
	var b []byte
	e := s.store.db.QueryRow("SELECT body FROM drama_continuity WHERE work_id=? ORDER BY version DESC LIMIT 1", id).Scan(&b)
	if errors.Is(e, sql.ErrNoRows) {
		return v, nil
	}
	if e != nil {
		return v, e
	}
	if e = json.Unmarshal(b, &v); e != nil {
		return v, e
	}
	d, e := s.dramaDoc(id, "script")
	if e != nil {
		return v, e
	}
	v.Current = v.Confirmed && d.State == "accepted" && d.Hash == v.ScriptHash && d.Version == v.ScriptVersion
	return v, nil
}
func (s *Studio) insertDramaWork(tx *sql.Tx, w DramaWork) error {
	shell := Project{ID: w.ID, Title: w.Title, AspectRatio: w.Aspect, Duration: w.Duration, Created: w.Created}
	if _, e := tx.Exec("INSERT INTO projects(id,body) VALUES(?,?)", w.ID, jsonBytes(shell)); e != nil {
		return e
	}
	_, e := tx.Exec("INSERT INTO drama_works VALUES(?,?)", w.ID, jsonBytes(w))
	return e
}
func (s *Studio) dramaSeriesRoutes(g *gin.RouterGroup) {
	g.GET("/series", func(c *gin.Context) {
		rows, e := s.store.db.Query("SELECT body FROM drama_series_versions v WHERE version=(SELECT max(version) FROM drama_series_versions WHERE series_id=v.series_id) ORDER BY rowid DESC")
		if e != nil {
			result(c, nil, e)
			return
		}
		defer rows.Close()
		out := []DramaSeries{}
		for rows.Next() {
			var b []byte
			var v DramaSeries
			if e = rows.Scan(&b); e != nil {
				break
			}
			if e = json.Unmarshal(b, &v); e != nil {
				break
			}
			out = append(out, v)
		}
		if e == nil {
			e = rows.Err()
		}
		result(c, out, e)
	})
	g.POST("/series", func(c *gin.Context) {
		var v DramaSeries
		if !bind(c, &v) {
			return
		}
		if e := validateSeries(&v); e != nil {
			result(c, nil, e)
			return
		}
		s.dramaMu.Lock()
		defer s.dramaMu.Unlock()
		v.ID = uid()
		v.Version = 1
		v.Created = now()
		v.Media = []DramaMedia{}
		v.Hash = seriesHash(v)
		cfg, e := s.store.settings()
		if e != nil {
			result(c, nil, e)
			return
		}
		library := DramaWork{ID: v.ID, Library: true, Title: v.Title + " · 共用素材库", Idea: v.Idea, Aspect: v.Aspect, Duration: v.Duration, Language: "zh-CN", PromptLanguage: "zh-CN", Created: v.Created, Profile: dramaProfile(cfg, "", "")}
		e = s.store.transaction(func(tx *sql.Tx) error {
			if e := s.insertDramaWork(tx, library); e != nil {
				return e
			}
			_, e := tx.Exec("INSERT INTO drama_series_versions VALUES(?,?,?)", v.ID, v.Version, jsonBytes(v))
			return e
		})
		result(c, v, e)
	})
	g.GET("/series/:series", func(c *gin.Context) {
		s.dramaMu.Lock()
		defer s.dramaMu.Unlock()
		id := c.Param("series")
		v, e := s.dramaSeries(id, 0)
		if e != nil {
			result(c, nil, e)
			return
		}
		rows, e := s.store.db.Query("SELECT work_id FROM drama_episodes WHERE series_id=? ORDER BY number", id)
		if e != nil {
			result(c, nil, e)
			return
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				break
			}
			ids = append(ids, id)
		}
		if e == nil {
			e = rows.Err()
		}
		rows.Close()
		if e != nil {
			result(c, nil, e)
			return
		}
		works := []DramaWork{}
		states := []DramaContinuity{}
		media := []DramaMedia{}
		progress := map[string]string{}
		for _, wid := range append([]string{id}, ids...) {
			w, err := s.dramaWork(wid)
			if err != nil {
				result(c, nil, err)
				return
			}
			if !w.Library {
				works = append(works, w)
				state, err := s.dramaContinuity(wid)
				if err != nil {
					result(c, nil, err)
					return
				}
				states = append(states, state)
			}
			all, err := s.dramaMedias(wid)
			if err != nil {
				result(c, nil, err)
				return
			}
			media = append(media, all...)
			if !w.Library {
				status := "待写剧本"
				script, err := s.dramaDoc(wid, "script")
				if err != nil {
					result(c, nil, err)
					return
				}
				if script.Text != "" {
					status = "剧本待采用"
				}
				if script.State == "accepted" {
					status = "剧本已采用"
				}
				videos := 0
				for _, m := range all {
					if m.Current && m.Kind == "video" {
						videos++
					}
				}
				if videos > 0 {
					status = fmt.Sprintf("已生成%d段视频", videos)
				}
				for _, m := range all {
					if m.Current && m.Kind == "film" {
						status = "已合成成片"
						break
					}
				}
				tasks, err := s.store.tasks(wid)
				if err != nil {
					result(c, nil, err)
					return
				}
				for _, t := range tasks {
					if t.Status == "queued" || t.Status == "running" {
						status = "有任务执行中"
						break
					}
				}
				progress[wid] = status
			}
		}
		rows, e = s.store.db.Query("SELECT body FROM drama_series_versions WHERE series_id=? ORDER BY version DESC", id)
		if e != nil {
			result(c, nil, e)
			return
		}
		versions := []DramaSeries{}
		for rows.Next() {
			var raw []byte
			var r DramaSeries
			if e = rows.Scan(&raw); e != nil {
				break
			}
			if e = json.Unmarshal(raw, &r); e != nil {
				break
			}
			versions = append(versions, r)
		}
		if e == nil {
			e = rows.Err()
		}
		rows.Close()
		result(c, gin.H{"series": v, "versions": versions, "episodes": works, "continuity": states, "media": media, "progress": progress}, e)
	})
	g.PUT("/series/:series", func(c *gin.Context) {
		var req struct {
			Base   string      `json:"base"`
			Series DramaSeries `json:"series"`
		}
		if !bind(c, &req) {
			return
		}
		s.dramaMu.Lock()
		defer s.dramaMu.Unlock()
		old, e := s.dramaSeries(c.Param("series"), 0)
		if e != nil {
			result(c, nil, e)
			return
		}
		if req.Base != old.Hash {
			result(c, nil, errConflict)
			return
		}
		v := req.Series
		v.ID = old.ID
		v.Version = old.Version + 1
		v.Created = old.Created
		if e = validateSeries(&v); e == nil {
			v.Media, e = s.resolveSeriesMedia(v.ID, v.Media)
		}
		if e != nil {
			result(c, nil, e)
			return
		}
		v.Hash = seriesHash(v)
		_, e = s.store.db.Exec("INSERT INTO drama_series_versions VALUES(?,?,?)", v.ID, v.Version, jsonBytes(v))
		result(c, v, e)
	})
	g.POST("/series/:series/episodes", func(c *gin.Context) {
		var req struct {
			OperationID    string            `json:"operation_id"`
			PromptLanguage string            `json:"prompt_language,omitempty"`
			Version        int               `json:"version"`
			Number         int               `json:"number"`
			Title          string            `json:"title"`
			Idea           string            `json:"idea"`
			Duration       int               `json:"duration"`
			Plan           DramaEpisodePlan  `json:"plan"`
			Previous       []DramaContinuity `json:"previous"`
		}
		if !bind(c, &req) {
			return
		}
		if !dramaID.MatchString(req.OperationID) || req.Version < 1 || req.Number < 1 || req.Number > 999 || strings.TrimSpace(req.Title) == "" || len(req.Title) > 320 || strings.TrimSpace(req.Idea) == "" || len(req.Idea) > 20000 || len(req.Plan.Theme) > 2000 || len(req.Plan.Requirements) > 20000 || len(req.Previous) > 999 {
			result(c, nil, errors.New("请填写有效集号（1～999）、标题、梗概和共用版本"))
			return
		}
		s.dramaMu.Lock()
		defer s.dramaMu.Unlock()
		id := c.Param("series")
		inputHash := digest(jsonBytes(req))
		var existing, hash string
		e := s.store.db.QueryRow("SELECT work_id,input_hash FROM drama_episodes WHERE work_id=?", req.OperationID).Scan(&existing, &hash)
		if e == nil {
			if hash != inputHash {
				result(c, nil, errConflict)
				return
			}
			w, e := s.dramaWork(existing)
			if e == nil && w.SeriesID != id {
				e = errConflict
			}
			result(c, w, e)
			return
		}
		if !errors.Is(e, sql.ErrNoRows) {
			result(c, nil, e)
			return
		}
		series, e := s.dramaSeries(id, req.Version)
		if e != nil {
			result(c, nil, e)
			return
		}
		var count int
		if e = s.store.db.QueryRow("SELECT count(*) FROM drama_episodes WHERE series_id=? AND number=?", id, req.Number).Scan(&count); e != nil {
			result(c, nil, e)
			return
		}
		if count > 0 {
			result(c, nil, errors.New("集号已存在，请刷新分集列表"))
			return
		}
		previous := []DramaContinuity{}
		seen := map[string]bool{}
		for _, ref := range req.Previous {
			w, err := s.dramaWork(ref.WorkID)
			if err != nil {
				result(c, nil, err)
				return
			}
			v, err := s.dramaContinuity(w.ID)
			if err != nil {
				result(c, nil, err)
				return
			}
			if w.SeriesID != id || w.EpisodeNumber >= req.Number || !v.Current || ref.Hash != v.Hash || seen[w.ID] {
				result(c, nil, errors.New("前情须来自本系列更早分集的当前已确认版本"))
				return
			}
			seen[w.ID] = true
			previous = append(previous, v)
		}
		duration := req.Duration
		if duration == 0 {
			duration = series.Duration
		}
		if duration < 1 || duration > 600 {
			result(c, nil, errors.New("时长须为1～600秒"))
			return
		}
		cfg, e := s.store.settings()
		if e != nil {
			result(c, nil, e)
			return
		}
		language := req.PromptLanguage
		if language == "" {
			language = "zh-CN"
		}
		if !validDramaPromptLanguage(language) {
			result(c, nil, errors.New("提示词语言请选择中文或英文"))
			return
		}
		w := DramaWork{ID: req.OperationID, SeriesID: id, EpisodeNumber: req.Number, Title: req.Title, Idea: req.Idea, Plan: req.Plan, Genre: series.Genre, Form: series.Form, Aspect: series.Aspect, Duration: duration, Language: "zh-CN", PromptLanguage: language, Created: now(), Profile: dramaProfile(cfg, "", language)}
		refs := []DramaMedia{}
		for _, m := range series.Media {
			if e = s.verifyDramaMedia(m); e != nil {
				result(c, nil, e)
				return
			}
			raw, err := os.ReadFile(s.media(m.File))
			if err != nil {
				result(c, nil, err)
				return
			}
			if digest(raw) != m.Hash {
				result(c, nil, errors.New("复制共用素材期间源文件发生变化，请重新核对"))
				return
			}
			file, err := s.writeMedia(w.ID, uid()+filepath.Ext(m.File), raw)
			if err != nil {
				result(c, nil, err)
				return
			}
			clone := m
			clone.ID = uid()
			clone.WorkID = w.ID
			clone.Parent = m.ID
			clone.TaskID = ""
			clone.File = file
			clone.Path = "剧集/" + dramaEpisodeCode(w) + "/共用素材/" + filepath.Base(file)
			clone.Current = true
			refs = append(refs, clone)
		}
		w.Context = &DramaEpisodeContext{Series: series, Previous: previous, Media: refs}
		w.ProfileHash = profileHash(w.Profile)
		w.PlanHash = dramaEpisodeHash(w)
		e = s.store.transaction(func(tx *sql.Tx) error {
			if e := s.insertDramaWork(tx, w); e != nil {
				return e
			}
			if _, e := tx.Exec("INSERT INTO drama_episodes VALUES(?,?,?,?)", id, req.Number, w.ID, inputHash); e != nil {
				return e
			}
			for _, m := range refs {
				if _, e := tx.Exec("INSERT INTO drama_media VALUES(?,?,?)", m.ID, w.ID, jsonBytes(m)); e != nil {
					return e
				}
			}
			return nil
		})
		result(c, w, e)
	})
	g.PUT("/works/:id/plan", func(c *gin.Context) {
		var req struct {
			Base  string           `json:"base"`
			Title string           `json:"title"`
			Idea  string           `json:"idea"`
			Plan  DramaEpisodePlan `json:"plan"`
		}
		if !bind(c, &req) {
			return
		}
		s.dramaMu.Lock()
		defer s.dramaMu.Unlock()
		w, e := s.dramaWork(c.Param("id"))
		if e != nil {
			result(c, nil, e)
			return
		}
		if w.SeriesID == "" || req.Base != dramaEpisodeHash(w) {
			result(c, nil, errConflict)
			return
		}
		if strings.TrimSpace(req.Title) == "" || len(req.Title) > 320 || strings.TrimSpace(req.Idea) == "" || len(req.Idea) > 20000 || len(req.Plan.Theme) > 2000 || len(req.Plan.Requirements) > 20000 {
			result(c, nil, errors.New("请填写标题、梗概并检查长度"))
			return
		}
		w.Title = req.Title
		w.Idea = req.Idea
		w.Plan = req.Plan
		shell, e := s.store.project(w.ID)
		if e != nil {
			result(c, nil, e)
			return
		}
		shell.Title = w.Title
		e = s.store.transaction(func(tx *sql.Tx) error {
			if _, e := tx.Exec("UPDATE drama_works SET body=? WHERE id=?", jsonBytes(w), w.ID); e != nil {
				return e
			}
			_, e := tx.Exec("UPDATE projects SET body=? WHERE id=?", jsonBytes(shell), w.ID)
			return e
		})
		w.PlanHash = dramaEpisodeHash(w)
		result(c, w, e)
	})
	g.GET("/works/:id/continuity", func(c *gin.Context) { v, e := s.dramaContinuity(c.Param("id")); result(c, v, e) })
	g.PUT("/works/:id/continuity", func(c *gin.Context) {
		var req struct {
			ScriptVersion int    `json:"script_version"`
			Base          string `json:"base"`
			Text          string `json:"text"`
			Confirm       bool   `json:"confirm"`
			ScriptHash    string `json:"script_hash"`
		}
		if !bind(c, &req) {
			return
		}
		s.dramaMu.Lock()
		defer s.dramaMu.Unlock()
		w, e := s.dramaWork(c.Param("id"))
		if e != nil {
			result(c, nil, e)
			return
		}
		if w.SeriesID == "" {
			result(c, nil, errors.New("前情仅适用于系列分集"))
			return
		}
		old, e := s.dramaContinuity(w.ID)
		if e != nil {
			result(c, nil, e)
			return
		}
		if old.Hash != req.Base {
			result(c, nil, errConflict)
			return
		}
		if strings.TrimSpace(req.Text) == "" || len(req.Text) > 20000 {
			result(c, nil, errors.New("请填写本集结束状态（最多20000字节）"))
			return
		}
		d, e := s.dramaDoc(w.ID, "script")
		if e != nil {
			result(c, nil, e)
			return
		}
		if req.Confirm && (d.State != "accepted" || d.Hash != req.ScriptHash || d.Version != req.ScriptVersion) {
			result(c, nil, errors.New("请先采用当前剧本并重新核对前情"))
			return
		}
		v := DramaContinuity{WorkID: w.ID, Number: w.EpisodeNumber, Version: old.Version + 1, Text: req.Text, Confirmed: req.Confirm, ScriptHash: d.Hash, ScriptVersion: d.Version}
		v.Hash = digest(jsonBytes(v))
		_, e = s.store.db.Exec("INSERT INTO drama_continuity VALUES(?,?,?)", w.ID, v.Version, jsonBytes(v))
		v.Current = v.Confirmed
		result(c, v, e)
	})
}
