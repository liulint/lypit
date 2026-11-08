package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func videoVariantFixture(t *testing.T) (*Studio, DramaWork, DramaMedia, DramaProduction, Settings) {
	t.Helper()
	s := testStudio(t)
	cfg := estimateSettings("doubao-seedance-2-0-mini-260615")
	if _, e := s.store.db.Exec("UPDATE settings SET body=? WHERE id=1", jsonBytes(cfg)); e != nil {
		t.Fatal(e)
	}
	w := dramaTestWork(t, s)
	for _, kind := range []string{"script", "assets", "storyboard", "motion"} {
		dramaTestSave(t, s, w, kind, dramaMock(kind, 5))
	}
	im := guideReference(t, s, w.ID, "SHOT-EP001-001")
	im, e := s.padDramaFrame(w, im)
	if e != nil {
		t.Fatal(e)
	}
	im.Approved = true
	if e = s.saveDramaMedia(im); e != nil {
		t.Fatal(e)
	}
	p, inputs, e := s.buildDramaProduction(w, DramaProduction{Kind: "video", Document: "motion", Entry: "MOTION-EP001-001", FrameMediaID: im.ID, FrameMediaHash: im.Hash}, cfg)
	if e != nil {
		t.Fatal(e)
	}
	j := DramaJob{ID: uid(), WorkID: w.ID, Kind: "produce", Inputs: inputs, Production: &p}
	task, e := s.newDramaTask(w, cfg, j)
	if e != nil {
		t.Fatal(e)
	}
	task.Status = "completed"
	if e = s.store.saveTask(task); e != nil {
		t.Fatal(e)
	}
	file := w.ID + "/original.mp4"
	if e = os.MkdirAll(filepath.Dir(s.media(file)), 0700); e != nil {
		t.Fatal(e)
	}
	b := []byte("immutable original fixture")
	if e = os.WriteFile(s.media(file), b, 0600); e != nil {
		t.Fatal(e)
	}
	m := DramaMedia{ID: uid(), WorkID: w.ID, Entry: p.Entry, Kind: "video", File: file, Path: s.dramaMediaPath(w.ID, "original.mp4"), Hash: digest(b), TaskID: task.ID, Approved: true, Duration: 5}
	if e = s.saveDramaMedia(m); e != nil {
		t.Fatal(e)
	}
	return s, w, m, p, cfg
}

func TestDramaVideoVariantQuoteFreezesInputsAndResolution(t *testing.T) {
	s, w, m, original, cfg := videoVariantFixture(t)
	o := DramaVideoOptions{VendorID: cfg.Video.VendorID, Model: "doubao-seedance-2-0-260128", Resolution: "720p"}
	p := DramaProduction{Kind: "video", Document: "motion", Entry: m.Entry, Operation: "compare", CompareSource: &m, VideoOptions: &o, Prompt: "untrusted"}
	url := "/api/drama/works/" + w.ID + "/quotes"
	var q DramaQuote
	json.Unmarshal(request(t, s, "POST", url, p, 200), &q)
	if q.Model != o.Model || q.Spec.Prompt != original.Prompt || q.Spec.Mode != original.Mode || q.Spec.Bindings[0].Media.Hash != original.Bindings[0].Media.Hash || q.Spec.Duration != 5 || q.Spec.VideoOptions.Resolution != "720p" || q.Estimate == nil || q.Estimate.Amount != "4.97" || !strings.Contains(q.Estimate.Basis, "720p") {
		t.Fatalf("incorrect comparison snapshot: %+v", q)
	}
	if q.Inputs["@video-edit:"+m.ID] != dramaEditSourceHash(m) {
		t.Fatal("source not bound")
	}
	settings, _ := s.store.settings()
	if settings.Video.Model != cfg.Video.Model {
		t.Fatal("global settings changed")
	}
	work, _ := s.dramaWork(w.ID)
	if work.ProfileHash != w.ProfileHash {
		t.Fatal("creative profile changed")
	}
	var raw []byte
	s.store.db.QueryRow("SELECT body FROM drama_quotes WHERE id=?", q.ID).Scan(&raw)
	var saved struct {
		Config Settings `json:"private_config"`
	}
	json.Unmarshal(raw, &saved)
	if saved.Config.Video.Model != o.Model {
		t.Fatal("private config not frozen")
	}
	body := videoTaskOptions(Project{AspectRatio: w.Aspect, VideoResolution: q.Spec.VideoOptions.Resolution}, Shot{Duration: 5}, o.Model, q.Spec.Mode)
	if body["resolution"] != "720p" || body["duration"] != 5 {
		t.Fatal(body)
	}
	if strings.Contains(string(request(t, s, "POST", url, p, 200)), "private_config") {
		t.Fatal("private settings exposed")
	}
	o.Model = "doubao-seedance-2-0-mini-260615"
	o.Resolution = "1080p"
	request(t, s, "POST", url, p, 400)
	o.Resolution = "720p"
	p.Entry = "*"
	p.CompareSource = nil
	p.Entries = []string{m.Entry}
	json.Unmarshal(request(t, s, "POST", url, p, 200), &q)
	if len(q.Spec.Items) != 1 || q.Spec.Items[0].CompareSource.ID != m.ID || q.Spec.Items[0].Prompt != original.Prompt || len(q.ItemEstimates) != 1 {
		t.Fatal("batch trial did not include exact existing shot", q)
	}
	p.Entries = append(p.Entries, "MOTION-NOT-FOUND")
	request(t, s, "POST", url, p, 400)
	var n int
	s.store.db.QueryRow("SELECT count(*) FROM attempts").Scan(&n)
	if n != 0 {
		t.Fatal("quote submitted paid request")
	}
	// Revoking the bound frame invalidates a previously reviewed quote.
	im := original.Bindings[0].Media
	im.Approved = false
	s.saveDramaMedia(im)
	request(t, s, "POST", "/api/drama/quotes/"+q.ID+"/confirm", map[string]bool{"allow_unknown": true}, 409)
}

func TestDramaVideoSelectionDoesNotFollowNewCandidates(t *testing.T) {
	s, w, old, _, _ := videoVariantFixture(t)
	candidate := old
	candidate.ID = uid()
	candidate.Approved = false
	if e := s.saveDramaMedia(candidate); e != nil {
		t.Fatal(e)
	}
	all, e := s.dramaMedias(w.ID)
	if e != nil {
		t.Fatal(e)
	}
	for _, m := range all {
		if m.Kind == "video" && m.Selected && m.ID != old.ID {
			t.Fatal("new candidate displaced original")
		}
	}
	url := "/api/drama/works/" + w.ID + "/media/" + candidate.ID + "/select"
	request(t, s, "POST", url, map[string]string{"hash": candidate.Hash, "base": "stale"}, 409)
	request(t, s, "POST", url, map[string]string{"hash": "stale", "base": old.ID}, 400)
	request(t, s, "POST", url, map[string]string{"hash": candidate.Hash, "base": old.ID}, 200)
	// A recovered worker publishing this same candidate preserves the review.
	if e = s.saveDramaMedia(candidate); e != nil {
		t.Fatal(e)
	}
	recovered, e := s.dramaMediaByID(w.ID, candidate.ID)
	if e != nil || !recovered.Approved {
		t.Fatal("recovery erased human confirmation", e)
	}
	// Recovery re-publishing another result cannot undo this human choice.
	third := old
	third.ID = uid()
	third.Approved = false
	s.saveDramaMedia(third)
	var chosen string
	s.store.db.QueryRow("SELECT media_id FROM drama_video_choices WHERE work_id=? AND entry=?", w.ID, old.Entry).Scan(&chosen)
	if chosen != candidate.ID {
		t.Fatal("worker replaced selected version")
	}
	request(t, s, "POST", url, map[string]string{"hash": candidate.Hash, "base": old.ID}, 409)
	request(t, s, "POST", "/api/drama/works/"+w.ID+"/media/"+old.ID+"/select", map[string]string{"hash": old.Hash, "base": candidate.ID}, 200)
	var tasks int
	s.store.db.QueryRow("SELECT count(*) FROM tasks").Scan(&tasks)
	if tasks != 1 {
		t.Fatal("selection created generation task")
	}
}

func TestMockFilmIntegrationDramaVideoVariants(t *testing.T) {
	if os.Getenv("LYPIT_INTEGRATION") != "1" {
		t.Skip("set LYPIT_INTEGRATION=1")
	}
	s, w, old, _, cfg := videoVariantFixture(t)
	cfg.Mode = "mock"
	if _, err := s.store.db.Exec("UPDATE settings SET body=? WHERE id=1", jsonBytes(cfg)); err != nil {
		t.Fatal(err)
	}
	p := DramaProduction{Kind: "video", Document: "motion", Entry: old.Entry, Operation: "compare", CompareSource: &old, VideoOptions: &DramaVideoOptions{VendorID: cfg.Video.VendorID, Model: "doubao-seedance-2-0-260128", Resolution: "720p"}}
	var q DramaQuote
	json.Unmarshal(request(t, s, "POST", "/api/drama/works/"+w.ID+"/quotes", p, 200), &q)
	var task Task
	json.Unmarshal(request(t, s, "POST", "/api/drama/quotes/"+q.ID+"/confirm", map[string]bool{}, 200), &task)
	waitTask(t, s, task.ID)
	var duplicate Task
	json.Unmarshal(request(t, s, "POST", "/api/drama/quotes/"+q.ID+"/confirm", map[string]bool{}, 200), &duplicate)
	if duplicate.ID != task.ID {
		t.Fatal("confirmation created duplicate task")
	}
	all, err := s.dramaMedias(w.ID)
	if err != nil {
		t.Fatal(err)
	}
	var result DramaMedia
	for _, m := range all {
		if m.TaskID == task.ID {
			result = m
		}
		if m.Kind == "video" && m.Selected && m.ID != old.ID {
			t.Fatal("candidate replaced selection")
		}
	}
	if result.ID == "" || result.File == old.File || result.Parent != old.ID || result.Approved || result.VideoInfo == nil || result.VideoInfo.Resolution != "720p" || result.VideoInfo.Width == 0 {
		t.Fatalf("incorrect candidate: %+v", result)
	}
	if err = s.verifyDramaMedia(old); err != nil {
		t.Fatal("original overwritten", err)
	}
	if err = s.checkClipDuration(context.Background(), result.File, 5); err != nil {
		t.Fatal(err)
	}
	edit := "# 剪辑单\n\n## CUT-EP001-001 · 选用版本\n- 来源：MOTION-EP001-001 · " + old.Path + "\n- 入点：0.00\n- 出点：5.00\n- 时长：5.00\n- 取舍：入点=动作前；出点=落定后\n- 声音：保留原声\n- 字幕：无\n"
	dramaTestSave(t, s, w, "edit", edit)
	if _, err = s.dramaCuts(w.ID); err != nil {
		t.Fatal(err)
	}
	request(t, s, "POST", "/api/drama/works/"+w.ID+"/media/"+result.ID+"/select", map[string]string{"hash": result.Hash, "base": old.ID}, 200)
	if _, err = s.dramaCuts(w.ID); err == nil || !strings.Contains(err.Error(), "旧视频版本") {
		t.Fatal("old cut accepted after selection", err)
	}
}
