package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func videoEditFixture(t *testing.T, s *Studio) (Project, Settings, Task) {
	t.Helper()
	p := approveFirstVideoShot(t, s, readyRetryProject(t, s))
	cfg := estimateSettings("doubao-seedance-2-0-mini-260615")
	t.Setenv("ARK_API_KEY", "edit-test-secret")
	if _, err := s.store.db.Exec("UPDATE settings SET body=? WHERE id=1", jsonBytes(cfg)); err != nil {
		t.Fatal(err)
	}
	cfg, err := s.store.settings()
	if err != nil {
		t.Fatal(err)
	}
	old := Task{ID: uid(), ProjectID: p.ID, Stage: "shot_video", Status: "completed", Mode: "remote", Config: cfg}
	file := p.ID + "/" + old.ID + "-clip-" + p.Shots[0].ID + ".mp4"
	if _, err = runCommand(context.Background(), s.root, "ffmpeg", "-v", "error", "-y", "-f", "lavfi", "-i", "color=c=green:s=480x864:r=24", "-t", "5", "-c:v", "libx264", "-pix_fmt", "yuv420p", s.media(file)); err != nil {
		t.Fatal(err)
	}
	p, err = s.store.update(p.ID, p.Revision, func(p *Project) error {
		p.Mode = "remote"
		p.Shots[0].Video = file
		p.Shots[0].VideoTaskID = old.ID
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	old.Input = p
	if _, err = s.store.db.Exec("INSERT INTO tasks VALUES(?,?,?,?,?,?)", old.ID, p.ID, old.Status, jsonBytes(old), jsonBytes(p), jsonBytes(cfg)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.store.db.Exec("INSERT INTO attempts(id,task_id,status,response,remote_id) VALUES(?,?,'received',?,?)", old.ID+":video-"+p.Shots[0].ID, old.ID, []byte(`{"id":"old-remote"}`), "old-remote"); err != nil {
		t.Fatal(err)
	}
	return p, cfg, old
}

func TestVideoEditQuoteBindsCurrentVideoAndRejectsChangedSource(t *testing.T) {
	s := testStudio(t)
	p, cfg, old := videoEditFixture(t, s)
	before := string(jsonBytes(p))
	var q Quote
	json.Unmarshal(request(t, s, "POST", "/api/projects/"+p.ID+"/quotes", map[string]any{"revision": p.Revision, "stage": "shot_video", "shot_id": p.Shots[0].ID, "feedback": "背景暗一点", "video_mode": "edit_video"}, 200), &q)
	if len(q.VideoInputs) != 1 || q.Estimate != nil || !q.Unknown || !strings.Contains(q.EstimateReason, "输入") {
		t.Fatal("edit quote scope or billing wrong", q)
	}
	v := q.VideoInputs[0]
	b, err := readVideoFile(s.media(p.Shots[0].Video))
	if err != nil {
		t.Fatal(err)
	}
	if v.Mode != "edit_video" || v.SourceShotID != p.Shots[0].ID || v.SourceVideo != p.Shots[0].Video || v.SourceHash != digest(b) || v.SourceTaskID != old.ID || v.SourceRemoteID != "old-remote" || len(v.Images) != 0 {
		t.Fatal("edit did not bind only this video", v)
	}
	if string(jsonBytes(getProject(t, s, p.ID))) != before {
		t.Fatal("quoting replaced original video")
	}
	// Recreate is a different quote, with a first frame and no old video input.
	recreate, err := s.quoteFor(p, cfg, "shot_video", ShotRequest{ShotID: p.Shots[0].ID, Feedback: "动作自然一点", VideoMode: "first_frame"})
	if err != nil || recreate.VideoInputs[0].SourceVideo != "" || len(recreate.VideoInputs[0].Images) != 1 {
		t.Fatal("recreate unexpectedly transmitted old video", err, recreate)
	}
	if err = os.WriteFile(s.media(v.SourceVideo), append(b, 0), 0600); err != nil {
		t.Fatal(err)
	}
	request(t, s, "POST", "/api/quotes/"+q.ID+"/confirm", map[string]any{"allow_unknown": true}, 400)
	jobs, _ := s.store.tasks(p.ID)
	for _, job := range jobs {
		if job.Status == "queued" || job.Status == "running" {
			t.Fatal("changed source launched paid work")
		}
	}
}

func TestVideoEditRejectsUnknownModelsReceiptsAndOtherShotSources(t *testing.T) {
	s := testStudio(t)
	p, cfg, old := videoEditFixture(t, s)
	for _, model := range []string{"ep-custom", "doubao-seedance-1-0-pro-250528", "doubao-seedance-2-0-299999"} {
		candidate := cfg
		candidate.Video.Model = model
		if _, err := s.videoInput(p, p.Shots[0], candidate, "edit_video", ""); err == nil {
			t.Fatal("unverified edit model accepted", model)
		}
	}
	wrong := p.Shots[0]
	wrong.VideoTaskID = "missing"
	if _, err := s.videoInput(p, wrong, cfg, "edit_video", ""); err == nil {
		t.Fatal("missing supplier record accepted")
	}
	wrong = p.Shots[0]
	wrong.ID = p.Shots[1].ID
	if _, err := s.videoInput(p, wrong, cfg, "edit_video", ""); err == nil {
		t.Fatal("another shot's source accepted")
	}
	if _, err := s.store.db.Exec("UPDATE attempts SET status='unknown' WHERE task_id=?", old.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.videoInput(p, p.Shots[0], cfg, "edit_video", ""); err == nil {
		t.Fatal("unknown receipt accepted")
	}
}

func TestVideoEditUnknownSubmissionDoesNotRebuildOrResubmit(t *testing.T) {
	s := testStudio(t)
	job := cliTask(t, s)
	job.Config = defaultSettings()
	job.Stage = "shot_video"
	job.VideoInputs = []VideoInput{{ShotID: "s", Mode: "edit_video", SourceVideo: "missing.mp4", SourceHash: "missing"}}
	if _, err := s.store.db.Exec("INSERT INTO attempts(id,task_id,status) VALUES(?,?,'unknown')", job.ID+":video-s", job.ID); err != nil {
		t.Fatal(err)
	}
	_, err := s.remoteVideo(context.Background(), &job, Shot{ID: "s"})
	if err == nil || !strings.Contains(err.Error(), "提交结果未知") {
		t.Fatal("unknown edit tried to rebuild source or resubmit", err)
	}
}

func TestVideoEditRequestOptionsKeepNativeEditingSeparateFromExtension(t *testing.T) {
	p := Project{AudioMode: "dialogue", AspectRatio: "9:16"}
	q := Shot{Duration: 5}
	for _, model := range []string{"doubao-seedance-2-5-260628", "doubao-seedance-2-0-mini-260615"} {
		body := videoTaskOptions(p, q, model, "edit_video")
		if body["ratio"] != "adaptive" || body["generate_audio"] != true {
			t.Fatal("edit dimensions or audio changed", body)
		}
		if videoEditModelVersion(model) == "2.5" {
			if body["duration"] != -1 || body["omni_reference_task_type"] != "edit" {
				t.Fatal("2.5 edit must preserve original duration, not extend", body)
			}
		} else if body["duration"] != 5 || body["omni_reference_task_type"] != nil {
			t.Fatal("2.0 received 2.5-only task options", body)
		}
	}
	first := videoTaskOptions(p, q, "doubao-seedance-2-5-260628", "first_frame")
	if first["duration"] != 5 || first["omni_reference_task_type"] != nil {
		t.Fatal("recreate inherited edit settings", first)
	}
}

func TestVideoEditMockIntegration(t *testing.T) {
	if os.Getenv("LYPIT_INTEGRATION") != "1" {
		t.Skip("set LYPIT_INTEGRATION=1")
	}
	s := testStudio(t)
	p := approveFirstVideoShot(t, s, readyRetryProject(t, s))
	p = runSingleVideo(t, s, p, "")
	oldFile := p.Shots[0].Video
	oldBytes, err := os.ReadFile(s.media(oldFile))
	if err != nil {
		t.Fatal(err)
	}
	others := string(jsonBytes(p.Shots[1:]))
	var quote Quote
	json.Unmarshal(request(t, s, "POST", "/api/projects/"+p.ID+"/quotes", map[string]any{"revision": p.Revision, "stage": "shot_video", "shot_id": p.Shots[0].ID, "feedback": "仅调整当前镜头背景", "video_mode": "edit_video"}, 200), &quote)
	if quote.VideoInputs[0].SourceVideo != oldFile {
		t.Fatal("wrong source video")
	}
	var job Task
	json.Unmarshal(request(t, s, "POST", "/api/quotes/"+quote.ID+"/confirm", map[string]any{}, 200), &job)
	waitTask(t, s, job.ID)
	p = getProject(t, s, p.ID)
	if p.Shots[0].Video == oldFile || p.Shots[0].VideoMode != "edit_video" || string(jsonBytes(p.Shots[1:])) != others {
		t.Fatal("edit expanded scope or failed to produce separate version")
	}
	retained, err := os.ReadFile(s.media(oldFile))
	if err != nil || digest(retained) != digest(oldBytes) {
		t.Fatal("original video was overwritten", err)
	}
}
