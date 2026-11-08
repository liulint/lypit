package main

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/gin-gonic/gin"
)

type DramaProfile struct {
	Model      string `json:"model"`
	Vendor     string `json:"vendor"`
	Min        int    `json:"min"`
	Max        int    `json:"max"`
	Known      bool   `json:"known"`
	Mode       string `json:"mode"`
	Language   string `json:"language"`
	Resolution string `json:"resolution"`
	Source     string `json:"source"`
	Checked    string `json:"checked"`
}

// Native ranges are independent of the retired editorial 12-second limit.
// Official API checked 2026-11-08: https://docs.volcengine.com/docs/82379/1520757
func dramaProfile(cfg Settings, mode, language string) DramaProfile {
	if mode == "" {
		mode = "first_frame"
	}
	if language == "" {
		language = "zh-CN"
	}
	p := DramaProfile{Mode: mode, Language: language, Resolution: "480p", Source: "https://docs.volcengine.com/docs/82379/1520757", Checked: "2026-11-08"}
	ep, e := resolveEndpoint(cfg, "video")
	if e == nil {
		p.Model = ep.Model
		p.Vendor = ep.VendorID
	}
	model := strings.ToLower(p.Model)
	switch {
	case cfg.Mode == "mock":
		p.Min = 1
		p.Max = 600
		p.Known = true
	case strings.Contains(model, "seedance-2-5") || strings.Contains(model, "seedance-2.5"):
		p.Min = 4
		p.Max = 30
		p.Known = true
	case strings.Contains(model, "seedance-2-0") || strings.Contains(model, "seedance-2.0"):
		p.Min = 4
		p.Max = 15
		p.Known = true
	case strings.Contains(model, "seedance-1"):
		p.Min = 2
		p.Max = 12
		p.Known = true
	}
	return p
}
func profileHash(p DramaProfile) string { return digest(jsonBytes(p)) }
func (s *Studio) dramaDependencyCurrent(id, kind, hash string, accepted bool) bool {
	if strings.HasPrefix(kind, "@tail-video:") {
		m, err := s.dramaMediaByID(id, strings.TrimPrefix(kind, "@tail-video:"))
		if err != nil || m.Kind != "video" || m.Hash != hash || !m.Approved || !s.dramaMediaCurrent(m) || s.verifyDramaMedia(m) != nil {
			return false
		}
		var selected string
		return s.store.db.QueryRow("SELECT media_id FROM drama_video_choices WHERE work_id=? AND entry=?", id, m.Entry).Scan(&selected) == nil && selected == m.ID
	}
	if strings.HasPrefix(kind, "@video-choice:") {
		var selected string
		err := s.store.db.QueryRow("SELECT media_id FROM drama_video_choices WHERE work_id=? AND entry=?", id, strings.TrimPrefix(kind, "@video-choice:")).Scan(&selected)
		return errors.Is(err, sql.ErrNoRows) || err == nil && selected == hash
	}
	if strings.HasPrefix(kind, "@video-edit:") {
		m, err := s.dramaMediaByID(id, strings.TrimPrefix(kind, "@video-edit:"))
		return err == nil && m.Kind == "video" && dramaEditSourceHash(m) == hash && s.dramaMediaCurrent(m) && s.verifyDramaMedia(m) == nil
	}
	if kind == "@prompt-language" {
		w, e := s.dramaWork(id)
		return e == nil && w.PromptLanguage == hash
	}
	if strings.HasPrefix(kind, "@media:") {
		return s.dramaReferenceCurrent(id, strings.TrimPrefix(kind, "@media:"), hash)
	}
	if kind == "@episode" {
		w, e := s.dramaWork(id)
		return e == nil && dramaEpisodeHash(w) == hash
	}
	if kind == "@profile" {
		w, e := s.dramaWork(id)
		return e == nil && profileHash(w.Profile) == hash
	}
	d, e := s.dramaDoc(id, kind)
	return e == nil && d.Hash == hash && (!accepted || d.State == "accepted")
}
func validDramaPromptLanguage(language string) bool { return language == "zh-CN" || language == "en" }

func (s *Studio) dramaProfileRoutes(g *gin.RouterGroup) {
	g.PUT("/works/:id/prompt-language", func(c *gin.Context) {
		var req struct {
			Base        string `json:"base"`
			ProfileBase string `json:"profile_base"`
			Language    string `json:"language"`
		}
		if !bind(c, &req) {
			return
		}
		if !validDramaPromptLanguage(req.Language) {
			result(c, nil, errors.New("提示词语言请选择中文或英文"))
			return
		}
		s.dramaMu.Lock()
		defer s.dramaMu.Unlock()
		w, e := s.dramaWork(c.Param("id"))
		if e != nil {
			result(c, nil, e)
			return
		}
		if req.Base != w.PromptLanguage || req.ProfileBase != profileHash(w.Profile) {
			result(c, nil, errConflict)
			return
		}
		// Change only future prompt configuration; never rewrite documents or task snapshots.
		w.PromptLanguage = req.Language
		w.Profile.Language = req.Language
		w.ProfileHash = profileHash(w.Profile)
		_, e = s.store.db.Exec("UPDATE drama_works SET body=? WHERE id=?", jsonBytes(w), w.ID)
		result(c, w, e)
	})
	g.PUT("/works/:id/profile", func(c *gin.Context) {
		var v struct {
			Base     string `json:"base"`
			Mode     string `json:"mode"`
			Language string `json:"language"`
		}
		if !bind(c, &v) {
			return
		}
		s.dramaMu.Lock()
		defer s.dramaMu.Unlock()
		w, e := s.dramaWork(c.Param("id"))
		if e != nil {
			result(c, nil, e)
			return
		}
		if v.Base != profileHash(w.Profile) {
			result(c, nil, errConflict)
			return
		}
		cfg, e := s.store.settings()
		if e != nil {
			result(c, nil, e)
			return
		}
		w.Profile = dramaProfile(cfg, v.Mode, v.Language)
		w.ProfileHash = profileHash(w.Profile)
		_, e = s.store.db.Exec("UPDATE drama_works SET body=? WHERE id=?", jsonBytes(w), w.ID)
		result(c, w, e)
	})
}

func dramaProjectConfig(w DramaWork) map[string]any {
	model := strings.ToLower(w.Profile.Model)
	dialect := w.Profile.Model
	switch {
	case strings.Contains(model, "seedance-2-5") || strings.Contains(model, "seedance-2.5"):
		dialect = "seedance-2.5"
	case strings.Contains(model, "seedance-2-0") || strings.Contains(model, "seedance-2.0"):
		dialect = "seedance-2.0"
	}
	status := "unset"
	choices := map[string]any{}
	if w.Profile.Model != "" && w.Profile.Known {
		status = "accepted"
		choices = map[string]any{"target_video_model": dialect, "model_id": w.Profile.Model, "video_prompt_dialect": dialect, "video_prompt_language": w.Profile.Language, "native_duration_seconds": map[string]int{"min": w.Profile.Min, "max": w.Profile.Max}, "audio_generation": "same_pass"}
	}
	return map[string]any{"schema_version": "1.0.0-draft", "project_id": w.ID, "title": w.Title, "language": w.Language, "format": map[string]any{"prompt_language": w.PromptLanguage, "aspect_ratio": w.Aspect, "episode_count": 1, "episode_id": dramaEpisodeCode(w), "target_seconds_per_episode": w.Duration}, "creator_authority": map[string]any{"production_profile": map[string]any{"status": status, "choices": choices}, "rhythm_profile": map[string]any{"status": "unset"}}, "lypit": map[string]any{"skill_version": dramaVersion, "native_continuation": "bound_provider_receipt_only"}}
}
