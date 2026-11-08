package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func guideReference(t *testing.T, s *Studio, work string, entry string) DramaMedia {
	t.Helper()
	var m DramaMedia
	raw := mockPNG(entry, false, false)
	json.Unmarshal(request(t, s, "POST", "/api/drama/works/"+work+"/media/import", map[string]any{"entry": entry, "name": "参考.png", "data": base64.StdEncoding.EncodeToString(raw)}, 200), &m)
	json.Unmarshal(request(t, s, "POST", "/api/drama/works/"+work+"/media/"+m.ID+"/approve", map[string]any{"category": "character"}, 200), &m)
	return m
}
func TestDramaTextReferenceSnapshot(t *testing.T) {
	s := testStudio(t)
	w := dramaTestWork(t, s)
	other := dramaTestWork(t, s)
	m := guideReference(t, s, w.ID, "固定角色")
	foreign := guideReference(t, s, other.ID, "别的角色")
	id := uid()
	req := map[string]any{"kind": "script", "base": "", "request": "少对白", "operation_id": id, "reference_media_ids": []string{m.ID}}
	request(t, s, "POST", "/api/drama/works/"+w.ID+"/create", req, 200)
	waitTask(t, s, id)
	job, e := s.readDramaJob(id)
	if e != nil || len(job.MediaReferences) != 1 || !strings.Contains(job.Prompt, m.Path) || job.Inputs["@media:"+m.ID] != m.Hash {
		t.Fatal("reference snapshot missing", e)
	}
	request(t, s, "POST", "/api/drama/works/"+w.ID+"/create", req, 200)
	req["reference_media_ids"] = []string{}
	request(t, s, "POST", "/api/drama/works/"+w.ID+"/create", req, 409)
	req["operation_id"] = uid()
	req["reference_media_ids"] = []string{foreign.ID}
	request(t, s, "POST", "/api/drama/works/"+w.ID+"/create", req, 400)
	os.WriteFile(s.media(m.File), []byte("changed"), 0600)
	request(t, s, "POST", "/api/drama/jobs/"+id+"/adopt", map[string]any{}, 409)
	// References to a generated image can be used to revise its own source without
	// recursively following creative dependencies back into that document.
	raw := mockPNG("cycle", false, false)
	file, e := s.writeMedia(w.ID, "cycle.png", raw)
	if e != nil {
		t.Fatal(e)
	}
	source := dramaTestSave(t, s, w, "script", dramaMock("script", 5))
	cfg, _ := s.store.settings()
	j := DramaJob{ID: uid(), WorkID: w.ID, Kind: "produce", Inputs: map[string]string{"script": source.Hash}}
	if _, e = s.newDramaTask(w, cfg, j); e != nil {
		t.Fatal(e)
	}
	image := DramaMedia{ID: uid(), WorkID: w.ID, Entry: "循环检查", Kind: "image", File: file, Path: "剧集/EP001/制作成果/cycle.png", Hash: digest(raw), Approved: true, TaskID: j.ID}
	s.saveDramaMedia(image)
	if !s.dramaReferenceCurrent(w.ID, image.ID, image.Hash) {
		t.Fatal("image not available")
	}
	updated, e := s.saveDramaDoc(w.ID, "script", source.Hash, source.Text+"\n修订", true, map[string]string{"@media:" + image.ID: image.Hash})
	if e != nil || updated.State != "accepted" {
		t.Fatal(updated, e)
	}
}
func TestDramaSharedReuseReceipts(t *testing.T) {
	s := testStudio(t)
	v := seriesTestCreate(t, s)
	m := guideReference(t, s, v.ID, "主角")
	v.Media = []DramaMedia{m}
	var published DramaSeries
	json.Unmarshal(request(t, s, "PUT", "/api/drama/series/"+v.ID, map[string]any{"base": v.Hash, "series": v}, 200), &published)
	first := seriesTestEpisode(t, s, v, 1, nil)
	second := seriesTestEpisode(t, s, v, 2, nil)
	foreignSeries := seriesTestCreate(t, s)
	foreign := seriesTestEpisode(t, s, foreignSeries, 1, nil)
	before := dramaTestSave(t, s, first, "script", dramaMock("script", 5))
	req := map[string]any{"operation_id": uid(), "version": published.Version, "series_hash": published.Hash, "media_ids": []string{m.ID}, "work_ids": []string{first.ID, second.ID}}
	// Force only the second target to fail, then resume the same durable request.
	cloneID := digest([]byte("shared:" + v.ID + ":" + second.ID + ":" + m.ID + ":" + m.Hash))[:24]
	file, e := s.writeMedia(second.ID, cloneID+filepath.Ext(m.File), []byte("do not overwrite"))
	if e != nil {
		t.Fatal(e)
	}
	var result DramaReuseReceipt
	json.Unmarshal(request(t, s, "POST", "/api/drama/series/"+v.ID+"/reuse-media", req, 200), &result)
	if result.Results[0].State != "completed" || result.Results[1].State != "failed" {
		t.Fatal(result.Results)
	}
	actual, _ := s.dramaDoc(first.ID, "script")
	unchanged, _ := s.dramaWork(first.ID)
	if actual.Hash != before.Hash || actual.State != "accepted" || unchanged.Context.Series.Version != 1 {
		t.Fatal("reuse changed episode context")
	}
	os.Remove(s.media(file))
	json.Unmarshal(request(t, s, "POST", "/api/drama/series/"+v.ID+"/reuse-media", req, 200), &result)
	if result.Results[1].State != "completed" {
		t.Fatal(result.Results)
	}
	for _, w := range []DramaWork{first, second} {
		all, _ := s.dramaMedias(w.ID)
		if len(all) != 1 || all[0].SharedOrigin == nil || all[0].SharedOrigin.Version != 2 || all[0].TaskID != "" || s.verifyDramaMedia(all[0]) != nil {
			t.Fatal(all)
		}
	}
	req["operation_id"] = uid()
	request(t, s, "POST", "/api/drama/series/"+v.ID+"/reuse-media", req, 200)
	all, _ := s.dramaMedias(first.ID)
	if len(all) != 1 {
		t.Fatal("duplicate copies")
	}
	req["work_ids"] = []string{foreign.ID}
	request(t, s, "POST", "/api/drama/series/"+v.ID+"/reuse-media", req, 409)
	req["operation_id"] = uid()
	request(t, s, "POST", "/api/drama/series/"+v.ID+"/reuse-media", req, 400)
	third := seriesTestEpisode(t, s, v, 3, nil)
	req["work_ids"] = []string{third.ID}
	req["operation_id"] = uid()
	os.WriteFile(s.media(m.File), []byte("changed"), 0600)
	json.Unmarshal(request(t, s, "POST", "/api/drama/series/"+v.ID+"/reuse-media", req, 200), &result)
	if result.Results[0].State != "failed" {
		t.Fatal("changed source accepted")
	}
	tasks, _ := s.store.tasks(first.ID)
	if len(tasks) != 0 {
		t.Fatal("reuse created model tasks")
	}
}
func TestDramaGuidedFirstFrameBinding(t *testing.T) {
	s := testStudio(t)
	w := dramaTestWork(t, s)
	dramaTestSave(t, s, w, "script", dramaMock("script", 5))
	dramaTestSave(t, s, w, "assets", dramaMock("assets", 5))
	board := dramaTestSave(t, s, w, "storyboard", dramaMock("storyboard", 5))
	source := guideReference(t, s, w.ID, "SHOT-EP001-001")
	cfg, _ := s.store.settings()
	j := DramaJob{ID: uid(), WorkID: w.ID, Kind: "produce", Inputs: map[string]string{"storyboard": board.Hash}}
	s.newDramaTask(w, cfg, j)
	source.TaskID = j.ID
	s.saveDramaMedia(source)
	req := map[string]any{"base": board.Hash, "entry": source.Entry, "media_id": source.ID}
	var draft DramaDoc
	json.Unmarshal(request(t, s, "POST", "/api/drama/works/"+w.ID+"/bind-frame", req, 200), &draft)
	request(t, s, "POST", "/api/drama/works/"+w.ID+"/bind-frame", req, 200)
	if draft.State != "draft" || draft.Hash == board.Hash {
		t.Fatal("frame not saved as draft")
	}
	if s.dramaMediaCurrent(source) {
		t.Fatal("source ancestry unexpectedly stayed current")
	}
	dramaTestSave(t, s, w, "storyboard", draft.Text)
	board, _ = s.dramaDoc(w.ID, "storyboard")
	text := strings.Replace(dramaMock("motion", 5), "无（待补参考图：本镜起始帧）", board.Entries[1].Fields["输入参考图"], 1)
	dramaTestSave(t, s, w, "motion", text)
	p, _, e := s.buildDramaProduction(w, DramaProduction{Kind: "video", Document: "motion", Entry: "MOTION-EP001-001"}, cfg)
	if e != nil || len(p.Bindings) != 1 || !p.Bindings[0].Media.ReferenceOnly {
		t.Fatal("video not using pinned frame", e)
	}
}

func TestDramaGuidedRegenerationAdvice(t *testing.T) {
	s := testStudio(t)
	w := dramaTestWork(t, s)
	dramaTestSave(t, s, w, "script", dramaMock("script", 5))
	dramaTestSave(t, s, w, "assets", dramaMock("assets", 5))
	dramaTestSave(t, s, w, "images", dramaMock("images", 5))
	cfg, _ := s.store.settings()
	_, _, e := s.buildDramaProduction(w, DramaProduction{Kind: "image", Document: "images", Entry: "IMG-AHUI", Operation: "regenerate"}, cfg)
	if e == nil {
		t.Fatal("missing advice accepted")
	}
	p, _, e := s.buildDramaProduction(w, DramaProduction{Kind: "image", Document: "images", Entry: "IMG-AHUI", Operation: "regenerate", Changes: "保持毛色，放松耳朵"}, cfg)
	if e != nil || !strings.Contains(p.Prompt, "保持毛色，放松耳朵") || p.Entry != "IMG-AHUI" || len(p.Items) != 0 {
		t.Fatal(p, e)
	}
	_, _, e = s.buildDramaProduction(w, DramaProduction{Kind: "image", Document: "images", Entry: "*", Operation: "regenerate", Changes: "修改"}, cfg)
	if e == nil {
		t.Fatal("regeneration broadened to batch")
	}
}

func TestDramaGuidedRegenerationCannotReplaceMissingPrompt(t *testing.T) {
	s := testStudio(t)
	w := dramaTestWork(t, s)
	dramaTestSave(t, s, w, "script", dramaMock("script", 5))
	dramaTestSave(t, s, w, "assets", dramaMock("assets", 5))
	dramaTestSave(t, s, w, "images", "# 图片\n\n## IMG-AHUI 阿灰\n- 用途：角色主图\n")
	cfg, _ := s.store.settings()
	_, _, e := s.buildDramaProduction(w, DramaProduction{Kind: "image", Document: "images", Entry: "IMG-AHUI", Operation: "regenerate", Changes: "保持外貌"}, cfg)
	if e == nil {
		t.Fatal("advice bypassed missing original prompt")
	}
}

func TestDramaPromptLanguageSelection(t *testing.T) {
	s := testStudio(t)
	w := dramaTestWork(t, s)
	if w.PromptLanguage != "zh-CN" || w.Profile.Language != "zh-CN" {
		t.Fatal("new work must default to Chinese", w)
	}
	series := seriesTestCreate(t, s)
	episode := seriesTestEpisode(t, s, series, 1, nil)
	if episode.PromptLanguage != "zh-CN" || episode.Profile.Language != "zh-CN" {
		t.Fatal("episode default", episode)
	}
	id := uid()
	request(t, s, "POST", "/api/drama/works/"+w.ID+"/create", map[string]any{"kind": "script", "base": "", "operation_id": id}, 200)
	waitTask(t, s, id)
	original, _ := s.readDramaJob(id)
	update := map[string]any{"base": w.PromptLanguage, "profile_base": w.ProfileHash, "language": "en"}
	var changed DramaWork
	json.Unmarshal(request(t, s, "PUT", "/api/drama/works/"+w.ID+"/prompt-language", update, 200), &changed)
	if changed.PromptLanguage != "en" || changed.Profile.Language != "en" {
		t.Fatal(changed)
	}
	after, _ := s.readDramaJob(id)
	if string(jsonBytes(original)) != string(jsonBytes(after)) {
		t.Fatal("language change rewrote task")
	}
	if s.dramaDependencyCurrent(w.ID, "@prompt-language", "zh-CN", true) {
		t.Fatal("old language remains current")
	}
	request(t, s, "PUT", "/api/drama/works/"+w.ID+"/prompt-language", update, 409)
	update["base"] = changed.PromptLanguage
	update["profile_base"] = changed.ProfileHash
	update["language"] = "fr"
	request(t, s, "PUT", "/api/drama/works/"+w.ID+"/prompt-language", update, 400)
	request(t, s, "POST", "/api/drama/jobs/"+id+"/adopt", map[string]any{}, 200)
	assetsID := uid()
	request(t, s, "POST", "/api/drama/works/"+w.ID+"/create", map[string]any{"kind": "assets", "base": "", "operation_id": assetsID}, 200)
	waitTask(t, s, assetsID)
	assetsJob, _ := s.readDramaJob(assetsID)
	if assetsJob.Inputs["@prompt-language"] != "en" || !strings.Contains(assetsJob.Prompt, "映序语言选择") {
		t.Fatal("language snapshot missing")
	}
	update["language"] = "zh-CN"
	request(t, s, "PUT", "/api/drama/works/"+w.ID+"/prompt-language", update, 200)
	request(t, s, "POST", "/api/drama/jobs/"+assetsID+"/adopt", map[string]any{}, 409)
	var english DramaWork
	json.Unmarshal(request(t, s, "POST", "/api/drama/works", map[string]any{"title": "English", "idea": "A story", "prompt_language": "en"}, 200), &english)
	if english.PromptLanguage != "en" || english.Profile.Language != "en" {
		t.Fatal(english)
	}
}

func TestDramaCLIImageDefaultModelQuote(t *testing.T) {
	s := testStudio(t)
	w := dramaTestWork(t, s)
	dramaTestSave(t, s, w, "script", dramaMock("script", 5))
	dramaTestSave(t, s, w, "assets", dramaMock("assets", 5))
	dramaTestSave(t, s, w, "images", dramaMock("images", 5))
	cfg, _ := s.store.settings()
	cfg.Mode = "remote"
	cfg.RegistryVersion = 1
	cfg.Vendors = []Vendor{{ID: "local-codex", Name: "Codex CLI", Kind: "codex", Type: "cli", CLI: "codex"}}
	cfg.Image = Endpoint{VendorID: "local-codex"}
	if _, e := s.store.db.Exec("UPDATE settings SET body=? WHERE id=1", jsonBytes(cfg)); e != nil {
		t.Fatal(e)
	}
	// A quote must not execute CLI inference or create a task/attempt.
	var quote DramaQuote
	json.Unmarshal(request(t, s, "POST", "/api/drama/works/"+w.ID+"/quotes", map[string]any{"kind": "image", "document": "images", "entry": "*"}, 200), &quote)
	if quote.Spec.Kind != "batch" || len(quote.Spec.Items) != 1 || !quote.Unknown || quote.Model != "" {
		t.Fatal(quote.Spec, quote.Unknown, quote.Model)
	}
	for _, table := range []string{"tasks", "attempts"} {
		var count int
		s.store.db.QueryRow("SELECT count(*) FROM " + table).Scan(&count)
		if count != 0 {
			t.Fatal("quote triggered inference", table, count)
		}
	}
	cfg.Vendors = []Vendor{{ID: "remote-image", Name: "API", Kind: "custom", Type: "api", Protocol: "openai"}}
	cfg.Image = Endpoint{VendorID: "remote-image"}
	if _, _, e := s.buildDramaProduction(w, DramaProduction{Kind: "image", Document: "images", Entry: "*"}, cfg); e == nil || !strings.Contains(e.Error(), "请先配置模型") {
		t.Fatal("API model still required", e)
	}
}
