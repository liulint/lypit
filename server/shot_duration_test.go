package main

import (
	"strings"
	"testing"
)

func durationSettings() Settings {
	cfg := defaultSettings()
	cfg.Mode = "remote"
	cfg.Video.Model = "doubao-seedance-2-0-mini-260615"
	return cfg
}
func TestRetimeShotSynchronizesScriptAndPreservesUnchangedMedia(t *testing.T) {
	p, shots := timedPlanFixture()
	p.PlanVersion = 2
	p.PlanApproved = true
	p.BoardsLocked = true
	p.Output = "old-final.mp4"
	p.Voice = "old.wav"
	a, b := shots[2], shots[2]
	a.ID = "short"
	b.ID = "rest"
	a.Duration = 2
	b.Duration = 5
	a.Narration = ""
	b.Narration = shots[2].Narration
	a.Video = "old.mp4"
	a.Candidates = []Image{{File: "image.png", Hash: "h", Approved: true}}
	a.Approved = true
	shots = append(append([]Shot{}, shots[:2]...), append([]Shot{a, b}, shots[3:]...)...)
	shots[4].Video = "independent.mp4"
	p.Shots = shots
	before := string(jsonBytes(p))
	next, err := retimeShot(p, "short", 4, false, durationSettings())
	if err != nil {
		t.Fatal(err)
	}
	if next.Duration != 32 || !strings.Contains(next.Script, "【10—19秒｜路灯】") || !strings.Contains(next.Script, "【19—24秒｜成绩】") || next.Shots[2].Duration != 4 || next.Shots[3].Duration != 5 {
		t.Fatal("wrong timeline", next)
	}
	if next.Shots[2].Approved || next.Shots[2].Video != "" || next.Shots[2].Candidates[0].File != "image.png" || next.Shots[4].Video != "independent.mp4" || next.Output != "" || next.Voice != "" || next.BoardsLocked || len(next.PreviousShots) != 1 || next.PreviousShots[0].Video != "old.mp4" {
		t.Fatal("media invalidation wrong", next)
	}
	if string(jsonBytes(p)) != before {
		t.Fatal("mutated original")
	}
	if err := validateFaithfulPlan(next, next.Shots); err != nil {
		t.Fatal("retimed plan invalid", err)
	}
	smaller, err := retimeShot(next, shots[0].ID, 4, false, durationSettings())
	if err != nil || smaller.Duration != 31 || !strings.Contains(smaller.Script, "【0—4秒｜窗外】") {
		t.Fatal("shortening failed", err)
	}
	if _, err := retimeShot(next, "short", 2, false, durationSettings()); err == nil {
		t.Fatal("unsupported seconds accepted")
	}
}
func TestDurationRulesRejectShortPlanningBeforeQuoteAndPreserveOldTasks(t *testing.T) {
	cfg := durationSettings()
	p, shots := timedPlanFixture()
	shots[0].Duration = 2
	if validateVideoDuration(cfg, shots[0]) == nil {
		t.Fatal("2 seconds accepted")
	}
	cfg.Video.Model = "custom-unknown"
	if videoDurationPolicy(cfg).Known || validateVideoDuration(cfg, shots[0]) != nil {
		t.Fatal("guessed unknown limits")
	}
	cfg = durationSettings()
	p.Script = "【0—2秒】开场\n【2—30秒】后续"
	if validatePlanningDurations(p, cfg) == nil {
		t.Fatal("short authored scene accepted")
	}
	s := testStudio(t)
	p.AssetsLocked = true
	p.Mode = cfg.Mode
	if _, err := s.quoteFor(p, cfg, "board_plan"); err == nil || !strings.Contains(err.Error(), "0—2") {
		t.Fatal("quote not blocked before inference", err)
	}
	var count int
	s.store.db.QueryRow("SELECT count(*) FROM attempts").Scan(&count)
	if count != 0 {
		t.Fatal("quote submitted request")
	}
	old := Task{}
	if old.VideoDurationPolicyVersion != 0 {
		t.Fatal("legacy task reinterpreted")
	}
}
func TestDurationAPIRevisionRejectedReceiptAndUnknownGuards(t *testing.T) {
	s := testStudio(t)
	p := readyRetryProject(t, s)
	p, err := s.store.update(p.ID, p.Revision, func(p *Project) error {
		p.PlanVersion = 2
		p.AudioMode = "narration"
		p.PlanApproved = true
		p.Script = "自然剧本"
		for i := range p.Shots {
			p.Shots[i].SourceText = p.Script
			p.Shots[i].FirstFrame = "首帧"
			p.Shots[i].ReviewStatus = "reviewed"
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := s.store.settings()
	id := p.Shots[0].ID
	job := Task{ID: uid(), ProjectID: p.ID, Stage: "shot_video", ShotID: id, Status: "failed", Input: p, Config: cfg}
	if _, err := s.store.db.Exec("INSERT INTO tasks VALUES(?,?,?,?,?,?)", job.ID, p.ID, job.Status, jsonBytes(job), jsonBytes(job.Input), jsonBytes(job.Config)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.db.Exec("INSERT INTO attempts(id,task_id,status,response) VALUES(?,?,?,?)", job.ID+":video", job.ID, "unknown", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"revision": p.Revision, "duration": 6, "settings_version": cfg.Version, "finish_rejected": true}
	path := "/api/projects/" + p.ID + "/shots/" + id + "/duration"
	request(t, s, "PUT", path, body, 400)
	if getProject(t, s, p.ID).Revision != p.Revision {
		t.Fatal("unknown task modified project")
	}
	s.store.db.Exec("UPDATE attempts SET status='rejected' WHERE task_id=?", job.ID)
	body["finish_rejected"] = false
	request(t, s, "PUT", path, body, 400)
	body["finish_rejected"] = true
	body["settings_version"] = cfg.Version + 1
	request(t, s, "PUT", path, body, 400)
	body["settings_version"] = cfg.Version
	request(t, s, "PUT", path, body, 200)
	updated := getProject(t, s, p.ID)
	if updated.Duration != 11 || updated.Revision != p.Revision+1 || updated.Shots[1].Duration != p.Shots[1].Duration {
		t.Fatal("bad duration update")
	}
	done, _ := s.store.task(job.ID)
	if done.Status != "abandoned" || done.Input.Revision != p.Revision || done.Input.Shots[0].Duration != 5 {
		t.Fatal("task snapshot changed", done)
	}
	request(t, s, "PUT", path, body, 409)
	var count int
	s.store.db.QueryRow("SELECT count(*) FROM attempts WHERE task_id=?", job.ID).Scan(&count)
	if count != 1 {
		t.Fatal("receipt changed")
	}
}
