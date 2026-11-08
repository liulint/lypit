package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func dramaVideoEditFixture(t *testing.T, s *Studio) (DramaWork, DramaMedia, Settings) {
	t.Helper()
	cfg := estimateSettings("doubao-seedance-2-0-mini-260615")
	if _, err := s.store.db.Exec("UPDATE settings SET body=? WHERE id=1", jsonBytes(cfg)); err != nil {
		t.Fatal(err)
	}
	w := dramaTestWork(t, s)
	for _, kind := range []string{"script", "assets", "storyboard", "motion"} {
		dramaTestSave(t, s, w, kind, dramaMock(kind, 5))
	}
	p := DramaProduction{Kind: "video", Document: "motion", Entry: "MOTION-EP001-001", Duration: 5, Mode: "first_frame"}
	j := DramaJob{ID: uid(), WorkID: w.ID, Kind: "produce", Inputs: map[string]string{}, Production: &p}
	old, err := s.newDramaTask(w, cfg, j)
	if err != nil {
		t.Fatal(err)
	}
	old.Status = "completed"
	if err = s.store.saveTask(old); err != nil {
		t.Fatal(err)
	}
	file := w.ID + "/" + old.ID + "-clip-EP001-001.mp4"
	if err = os.MkdirAll(filepath.Dir(s.media(file)), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = runCommand(context.Background(), s.root, "ffmpeg", "-v", "error", "-y", "-f", "lavfi", "-i", "color=c=green:s=480x864:r=24", "-t", "5", "-c:v", "libx264", "-pix_fmt", "yuv420p", s.media(file)); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(s.media(file))
	if err != nil {
		t.Fatal(err)
	}
	m := DramaMedia{ID: uid(), WorkID: w.ID, Entry: p.Entry, Kind: "video", File: file, Hash: digest(b), TaskID: old.ID, Duration: 5}
	if err = s.saveDramaMedia(m); err != nil {
		t.Fatal(err)
	}
	if _, err = s.store.db.Exec("INSERT INTO attempts(id,task_id,status,response,remote_id) VALUES(?,?,'received',?,?)", old.ID+":video-EP001-001", old.ID, []byte(`{"id":"original-remote"}`), "original-remote"); err != nil {
		t.Fatal(err)
	}
	return w, m, cfg
}

func TestDramaVideoEditQuoteBindsOnlyCurrentVideoWithoutSubmission(t *testing.T) {
	s := testStudio(t)
	w, m, _ := dramaVideoEditFixture(t, s)
	p := DramaProduction{Kind: "video", Document: "motion", Entry: m.Entry, Operation: "regenerate", Mode: "edit_video", Changes: "背景暗一点", Source: &DramaMedia{ID: m.ID, Hash: m.Hash}, VideoEditInput: &VideoInput{SourceRemoteID: "untrusted"}}
	var q DramaQuote
	if err := json.Unmarshal(request(t, s, "POST", "/api/drama/works/"+w.ID+"/quotes", p, 200), &q); err != nil {
		t.Fatal(err)
	}
	v := q.Spec.VideoEditInput
	if q.Spec.Mode != "edit_video" || len(q.Spec.Bindings) != 0 || v == nil || v.SourceRemoteID != "original-remote" || v.SourceHash != m.Hash || v.SourceVideo != m.File || len(v.Images) != 0 || q.Estimate != nil || !strings.Contains(q.EstimateReason, "输入") || !q.Unknown || strings.Contains(q.Spec.Prompt, "@图片") {
		t.Fatalf("incorrect video edit quote: %+v", q)
	}
	if q.Inputs["@video-edit:"+m.ID] != dramaEditSourceHash(m) {
		t.Fatal("missing source dependency")
	}
	var n int
	s.store.db.QueryRow("SELECT count(*) FROM tasks").Scan(&n)
	if n != 1 {
		t.Fatal("quote generated a task")
	}
	s.store.db.QueryRow("SELECT count(*) FROM attempts").Scan(&n)
	if n != 1 {
		t.Fatal("quote contacted supplier")
	}
	request(t, s, "POST", "/api/drama/quotes/"+q.ID+"/confirm", map[string]any{"allow_unknown": false}, 400)
	b, _ := os.ReadFile(s.media(m.File))
	if err := os.WriteFile(s.media(m.File), append(b, 0), 0600); err != nil {
		t.Fatal(err)
	}
	request(t, s, "POST", "/api/drama/quotes/"+q.ID+"/confirm", map[string]any{"allow_unknown": true}, 409)
}

func TestDramaVideoEditRejectsWrongSourcesAndUnsupportedModels(t *testing.T) {
	s := testStudio(t)
	w, source, cfg := dramaVideoEditFixture(t, s)
	p := DramaProduction{Entry: source.Entry, Duration: 5, Source: &source}
	for _, scenario := range []string{"foreign-shot", "changed-hash", "frame", "unsupported", "unknown-receipt"} {
		t.Run(scenario, func(t *testing.T) {
			m := source
			candidate := p
			candidate.Source = &m
			config := cfg
			switch scenario {
			case "foreign-shot":
				candidate.Entry = "MOTION-EP001-002"
			case "changed-hash":
				m.Hash = "wrong"
			case "frame":
				candidate.FrameMediaID = "image"
			case "unsupported":
				config.Video.Model = "ep-unverified"
			case "unknown-receipt":
				s.store.db.Exec("UPDATE attempts SET status='submitting' WHERE task_id=?", source.TaskID)
			}
			if _, _, err := s.dramaVideoEditInput(w, candidate, config); err == nil {
				t.Fatal("unsafe edit source accepted", scenario)
			}
		})
	}
}

func TestDramaVideoEditUnknownSubmissionRecoversWithoutSource(t *testing.T) {
	s := testStudio(t)
	w := dramaTestWork(t, s)
	p := DramaProduction{Kind: "video", Document: "motion", Entry: "MOTION-EP001-001", Mode: "edit_video", Duration: 5, Source: &DramaMedia{File: "missing.mp4"}, VideoEditInput: &VideoInput{Mode: "edit_video", ShotID: "EP001-001", SourceVideo: "missing.mp4"}}
	j := DramaJob{ID: uid(), WorkID: w.ID, Kind: "produce", Inputs: map[string]string{}, Production: &p}
	task, err := s.newDramaTask(w, estimateSettings("doubao-seedance-2-0-mini-260615"), j)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.db.Exec("INSERT INTO attempts(id,task_id,status) VALUES(?,?,'submitting')", task.ID+":video-EP001-001", task.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.executeDramaProduction(context.Background(), &task, &j); err == nil || !strings.Contains(err.Error(), "提交结果未知") {
		t.Fatal("unknown edit tried to rebuild or resubmit", err)
	}
}

func TestDramaVideoRecreateUsesReviewedPromptWithoutOldVideo(t *testing.T) {
	s := testStudio(t)
	w, old, cfg := dramaVideoEditFixture(t, s)
	frame := guideReference(t, s, w.ID, "SHOT-EP001-001")
	p := DramaProduction{Kind: "video", Document: "motion", Entry: old.Entry, Operation: "regenerate", Changes: "按修改后的本镜说明重做", PromptOverride: "小白先抬爪，阿灰停在原位观察。", FrameMediaID: frame.ID, FrameMediaHash: frame.Hash, Source: &old}
	before, _ := s.dramaDoc(w.ID, "motion")
	out, _, err := s.buildDramaProduction(w, p, cfg)
	if err != nil || out.Mode != "first_frame" || out.Source != nil || out.VideoEditInput != nil || len(out.Bindings) != 1 || !strings.Contains(out.Prompt, p.PromptOverride) {
		t.Fatal("recreate transmitted old video or lost reviewed text", out, err)
	}
	after, _ := s.dramaDoc(w.ID, "motion")
	if before.Hash != after.Hash || before.Text != after.Text {
		t.Fatal("quote rewrote the original document")
	}
	p.Mode = "edit_video"
	if _, _, err := s.buildDramaProduction(w, p, cfg); err == nil {
		t.Fatal("edit and recreate inputs were mixed")
	}
}

func TestMockFilmIntegrationDramaVideoEditPreservesSource(t *testing.T) {
	if os.Getenv("LYPIT_INTEGRATION") != "1" {
		t.Skip("set LYPIT_INTEGRATION=1")
	}
	s := testStudio(t)
	w, source, cfg := dramaVideoEditFixture(t, s)
	cfg.Mode = "mock"
	p, inputs, err := s.buildDramaProduction(w, DramaProduction{Kind: "video", Document: "motion", Entry: source.Entry, Operation: "regenerate", Mode: "edit_video", Changes: "仅背景变暗", Source: &source}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	j := DramaJob{ID: uid(), WorkID: w.ID, Kind: "produce", Inputs: inputs, Production: &p}
	task, err := s.newDramaTask(w, cfg, j)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.executeDramaProduction(context.Background(), &task, &j); err != nil {
		t.Fatal(err)
	}
	all, err := s.dramaMedias(w.ID)
	if err != nil || len(all) != 2 {
		t.Fatal("edit replaced original version", len(all), err)
	}
	var result DramaMedia
	for _, m := range all {
		if m.TaskID == task.ID {
			result = m
		}
	}
	if result.Parent != source.ID || result.File == source.File || result.Approved || !result.Mock {
		t.Fatal("edit result identity changed", result)
	}
	if err := s.verifyDramaMedia(source); err != nil {
		t.Fatal("source was overwritten", err)
	}
	if err := s.checkClipDuration(context.Background(), result.File, 5); err != nil {
		t.Fatal(err)
	}
	if err := s.requireAudio(context.Background(), result.File); err != nil {
		t.Fatal(err)
	}
}
