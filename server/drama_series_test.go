package main

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func seriesTestCreate(t *testing.T, s *Studio) DramaSeries {
	t.Helper()
	var v DramaSeries
	if e := json.Unmarshal(request(t, s, "POST", "/api/drama/series", map[string]any{"title": "测试系列", "idea": "人物在日常生活中成长", "settings": "角色与世界观保持一致", "form": "剧情短片", "duration": 5}, 200), &v); e != nil {
		t.Fatal(e)
	}
	return v
}
func seriesTestEpisode(t *testing.T, s *Studio, v DramaSeries, number int, previous []DramaContinuity) DramaWork {
	t.Helper()
	var w DramaWork
	if e := json.Unmarshal(request(t, s, "POST", "/api/drama/series/"+v.ID+"/episodes", map[string]any{"operation_id": uid(), "version": v.Version, "number": number, "title": "本集", "idea": "本集独立事件", "previous": previous, "plan": DramaEpisodePlan{Theme: "成长", Requirements: "少对白"}}, 200), &w); e != nil {
		t.Fatal(e)
	}
	return w
}
func TestDramaSeriesSnapshotsAndCarryover(t *testing.T) {
	s := testStudio(t)
	v := seriesTestCreate(t, s)
	// Import and confirm a real reference without a model call.
	var m DramaMedia
	json.Unmarshal(request(t, s, "POST", "/api/drama/works/"+v.ID+"/media/import", map[string]any{"entry": "固定角色", "name": "角色.png", "data": base64.StdEncoding.EncodeToString(mockPNG("series", false, false))}, 200), &m)
	request(t, s, "PUT", "/api/drama/series/"+v.ID, map[string]any{"base": v.Hash, "series": DramaSeries{Title: v.Title, Idea: v.Idea, Media: []DramaMedia{m}}}, 400)
	request(t, s, "POST", "/api/drama/works/"+v.ID+"/media/"+m.ID+"/approve", map[string]any{}, 200)
	m.Approved = true
	v.Media = []DramaMedia{m}
	var revision DramaSeries
	json.Unmarshal(request(t, s, "PUT", "/api/drama/series/"+v.ID, map[string]any{"base": v.Hash, "series": v}, 200), &revision)
	first := seriesTestEpisode(t, s, revision, 1, nil)
	script := dramaTestSave(t, s, first, "script", dramaMock("script", 5))
	var state DramaContinuity
	json.Unmarshal(request(t, s, "PUT", "/api/drama/works/"+first.ID+"/continuity", map[string]any{"text": "人物学会等待；左爪受伤尚未恢复。", "confirm": true, "script_hash": script.Hash, "script_version": script.Version}, 200), &state)
	if !state.Current {
		t.Fatal("confirmed continuity unavailable")
	}
	request(t, s, "PUT", "/api/drama/works/"+first.ID+"/continuity", map[string]any{"base": "old", "text": "覆写", "confirm": false}, 409)
	request(t, s, "PUT", "/api/drama/works/"+first.ID+"/continuity", map[string]any{"base": state.Hash, "text": "覆写", "confirm": true, "script_hash": script.Hash, "script_version": script.Version + 1}, 400)
	foreign := seriesTestCreate(t, s)
	request(t, s, "POST", "/api/drama/series/"+foreign.ID+"/episodes", map[string]any{"operation_id": uid(), "version": 1, "number": 2, "title": "跨系列", "idea": "无关", "previous": []DramaContinuity{state}}, 400)

	second := seriesTestEpisode(t, s, revision, 2, []DramaContinuity{state})
	if second.Context == nil || second.Context.Series.Version != 2 || len(second.Context.Media) != 1 || len(second.Context.Previous) != 1 {
		t.Fatalf("missing context: %+v", second)
	}
	clone := second.Context.Media[0]
	if clone.WorkID != second.ID || clone.File == m.File || clone.Parent != m.ID || !clone.Approved {
		t.Fatal("shared reference not isolated")
	}
	if e := s.verifyDramaMedia(clone); e != nil {
		t.Fatal(e)
	}
	bindings, e := s.dramaBindings(second.ID, "REF-A（顺序：1）· "+clone.Path+"《固定角色》（用途：身份；控制：外貌；不得控制：构图）")
	if e != nil || len(bindings) != 1 || bindings[0].Media.ID != clone.ID {
		t.Fatal("copied reference cannot be bound", e)
	}

	d := dramaTestSave(t, s, second, "script", strings.ReplaceAll(dramaMock("script", 5), "EP001", "EP002"))
	if d.Path != "剧集/EP002/剧本.md" || d.State != "accepted" {
		t.Fatal(d)
	}
	prompt, _, e := s.dramaPrompt(second, "script", "", d.Inputs)
	if e != nil || !strings.Contains(prompt, state.Text) || !strings.Contains(prompt, "少对白") || !strings.Contains(prompt, clone.Path) || !strings.Contains(prompt, "仅制作 EP002 一集") {
		t.Fatalf("context absent from prompt: %v", e)
	}
	revision.Settings = "新版世界观"
	revision.Media = nil
	request(t, s, "PUT", "/api/drama/series/"+v.ID, map[string]any{"base": revision.Hash, "series": revision}, 200)
	unchanged, _ := s.dramaWork(second.ID)
	if unchanged.Context.Series.Settings == revision.Settings {
		t.Fatal("series edit rewrote old episode")
	}
	// Source content may change; bound copies and selected carryover remain stable.
	os.WriteFile(s.media(m.File), []byte("changed"), 0600)
	if e = s.verifyDramaMedia(clone); e != nil {
		t.Fatal("source changed bound copy", e)
	}
	dramaTestSave(t, s, first, "script", script.Text+"\n人物走开。")
	dramaTestSave(t, s, first, "script", script.Text)
	current, _ := s.dramaContinuity(first.ID)
	if current.Current {
		t.Fatal("changed script left continuity current")
	}
	old, _ := s.dramaWork(second.ID)
	if old.Context.Previous[0].Text != state.Text {
		t.Fatal("old carryover rewritten")
	}
	request(t, s, "POST", "/api/drama/series/"+v.ID+"/episodes", map[string]any{"operation_id": uid(), "version": 2, "number": 3, "title": "第三集", "idea": "继续", "previous": []DramaContinuity{state}}, 400)
	// Plan changes invalidate documents and outstanding quotes through @episode.
	cfg, _ := s.store.settings()
	images := dramaTestSave(t, s, second, "assets", dramaMock("assets", 5))
	_ = images
	dramaTestSave(t, s, second, "images", dramaMock("images", 5))
	spec, inputs, e := s.buildDramaProduction(second, DramaProduction{Kind: "image", Document: "images", Entry: "IMG-AHUI"}, cfg)
	if e != nil {
		t.Fatal(e)
	}
	_ = spec
	loaded, _ := s.dramaWork(second.ID)
	request(t, s, "PUT", "/api/drama/works/"+second.ID+"/plan", map[string]any{"base": loaded.PlanHash, "title": "新标题", "idea": "新梗概", "plan": DramaEpisodePlan{}}, 200)
	after, _ := s.dramaDoc(second.ID, "script")
	if after.State != "stale" {
		t.Fatal("plan edit did not invalidate script")
	}
	for kind, hash := range inputs {
		if kind == "@episode" && s.dramaDependencyCurrent(second.ID, kind, hash, true) {
			t.Fatal("old quote context still current")
		}
	}
	// List excludes hidden library; standalone work remains unchanged.
	solo := dramaTestWork(t, s)
	var works []DramaWork
	json.Unmarshal(request(t, s, "GET", "/api/drama/works", nil, 200), &works)
	if len(works) != 3 {
		t.Fatal("library leaked or work disappeared", len(works))
	}
	if solo.SeriesID != "" || dramaEpisodeCode(solo) != "EP001" {
		t.Fatal(solo)
	}
	request(t, s, "GET", "/api/drama/series", nil, 200)
	request(t, s, "GET", "/api/drama/series/"+v.ID, nil, 200)
}
func TestDramaSeriesEpisodeIdempotencyAndIsolation(t *testing.T) {
	s := testStudio(t)
	v := seriesTestCreate(t, s)
	other := seriesTestCreate(t, s)
	req := map[string]any{"operation_id": uid(), "version": 1, "number": 2, "title": "第2集", "idea": "独立剧情"}
	var w, again DramaWork
	json.Unmarshal(request(t, s, "POST", "/api/drama/series/"+v.ID+"/episodes", req, 200), &w)
	json.Unmarshal(request(t, s, "POST", "/api/drama/series/"+v.ID+"/episodes", req, 200), &again)
	if again.ID != w.ID {
		t.Fatal("duplicate episode")
	}
	request(t, s, "POST", "/api/drama/series/"+other.ID+"/episodes", req, 409)
	req["idea"] = "changed"
	request(t, s, "POST", "/api/drama/series/"+v.ID+"/episodes", req, 409)
	req["operation_id"] = uid()
	request(t, s, "POST", "/api/drama/series/"+v.ID+"/episodes", req, 400)
	// Candidate uses EP002 and cannot be adopted after a planning edit.
	id := uid()
	request(t, s, "POST", "/api/drama/works/"+w.ID+"/create", map[string]any{"kind": "script", "base": "", "operation_id": id}, 200)
	waitTask(t, s, id)
	job, e := s.readDramaJob(id)
	if e != nil || !strings.Contains(job.Candidate, "EP002-SC001") {
		t.Fatal("wrong episode candidate", e)
	}
	request(t, s, "PUT", "/api/drama/works/"+w.ID+"/plan", map[string]any{"base": again.PlanHash, "title": "更改", "idea": "新规划"}, 200)
	request(t, s, "POST", "/api/drama/jobs/"+id+"/adopt", map[string]any{}, 409)
	// Export contains the right episode paths and context, without private settings.
	dramaTestSave(t, s, w, "script", strings.ReplaceAll(dramaMock("script", 5), "EP001", "EP002"))
	raw := request(t, s, "GET", "/api/drama/works/"+w.ID+"/export", nil, 200)
	z, e := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if e != nil {
		t.Fatal(e)
	}
	names := map[string]bool{}
	for _, f := range z.File {
		names[f.Name] = true
	}
	if !names["剧集/EP002/剧本.md"] || !names["系列与前情快照.json"] || names["剧集/EP001/剧本.md"] {
		t.Fatal(names)
	}
	request(t, s, "PUT", "/api/drama/series/"+v.ID, map[string]any{"base": "stale", "series": v}, 409)
	request(t, s, "POST", "/api/drama/works", map[string]any{"title": "forged", "idea": "x", "series_id": v.ID}, 400)
}
