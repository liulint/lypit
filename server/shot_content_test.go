package main

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func contentProject(t *testing.T, s *Studio) Project {
	t.Helper()
	p := readyRetryProject(t, s)
	p, e := s.store.update(p.ID, p.Revision, func(p *Project) error {
		p.PlanVersion = 2
		p.PlanApproved = true
		p.AudioMode = "narration"
		p.Script = "故事原文"
		p.BoardsLocked = true
		p.Output = "old-final.mp4"
		p.Voice = "old.wav"
		for i := range p.Shots {
			p.Shots[i].SourceText = p.Script
			p.Shots[i].FirstFrame = "原始首帧"
			p.Shots[i].ReviewStatus = "reviewed"
			p.Shots[i].Approved = true
		}
		p.Shots[0].Video = "old-video.mp4"
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func makeRewrite(t *testing.T, s *Studio, p Project, trial bool) Task {
	t.Helper()
	shots := p.Shots
	if trial {
		shots = p.PlannedShots
	}
	var q Quote
	json.Unmarshal(request(t, s, "POST", "/api/projects/"+p.ID+"/quotes", map[string]any{"revision": p.Revision, "stage": "shot_rewrite", "shot_id": shots[0].ID, "target_duration": 6, "plan_trial": trial}, 200), &q)
	if q.TargetDuration != 6 || q.ShotID != shots[0].ID {
		t.Fatal("wrong scope", q)
	}
	var task Task
	json.Unmarshal(request(t, s, "POST", "/api/quotes/"+q.ID+"/confirm", map[string]any{}, 200), &task)
	waitTask(t, s, task.ID)
	task, e := s.store.task(task.ID)
	if e != nil {
		t.Fatal(e)
	}
	return task
}
func TestShotRewriteWaitsForApplicationAndIsAtomic(t *testing.T) {
	s := testStudio(t)
	p := contentProject(t, s)
	cfg, _ := s.store.settings()
	task := makeRewrite(t, s, p, false)
	if !reflect.DeepEqual(getProject(t, s, p.ID), p) {
		t.Fatal("generation changed current project")
	}
	var out ShotRewriteResult
	json.Unmarshal(request(t, s, "GET", "/api/tasks/"+task.ID+"/rewrite", nil, 200), &out)
	if out.OldDuration != 5 || out.Duration != 6 || out.Before.FirstFrame != "原始首帧" {
		t.Fatal(out)
	}
	body := map[string]any{"revision": p.Revision, "settings_version": cfg.Version, "first_frame": "新首帧", "description": "新动作", "narration": "简短旁白"}
	request(t, s, "POST", "/api/tasks/"+task.ID+"/rewrite/apply", body, 200)
	next := getProject(t, s, p.ID)
	if next.Duration != p.Duration+1 || next.Shots[0].Duration != 6 || next.Shots[0].FirstFrame != "新首帧" || next.Shots[0].Video != "" || next.Shots[0].Approved || next.BoardsLocked || next.Output != "" || next.Voice != "" {
		t.Fatal("bad application", next)
	}
	if !reflect.DeepEqual(next.Shots[0].Candidates, p.Shots[0].Candidates) || !reflect.DeepEqual(next.Shots[1], p.Shots[1]) || len(next.PreviousShots) != 1 || next.PreviousShots[0].Video != "old-video.mp4" {
		t.Fatal("lost media or touched other shot")
	}
	body["revision"] = next.Revision
	request(t, s, "POST", "/api/tasks/"+task.ID+"/rewrite/apply", body, 409)
	if getProject(t, s, p.ID).Revision != next.Revision {
		t.Fatal("double apply")
	}
}
func TestManualContentAndDialogueOverridePreserveFidelity(t *testing.T) {
	p, shots := timedPlanFixture()
	p.Shots = shots
	p.PlanVersion = 2
	p.PlanApproved = true
	old := string(jsonBytes(p))
	v := ShotContent{FirstFrame: "猫在路灯下", Description: "猫招手后安静等待", Narration: "橘猫：来吧。"}
	next, e := changeShotContent(p, shots[2].ID, false, 6, v, defaultSettings())
	if e != nil {
		t.Fatal(e)
	}
	if string(jsonBytes(p)) != old {
		t.Fatal("mutated input")
	}
	if !contentOverridden(next.Shots[2]) || validateFaithfulPlan(next, next.Shots) != nil {
		t.Fatal("override not honored")
	}
	if !strings.Contains(next.Script, "橘猫：小朋友，冷了怎么办？") || !strings.Contains(next.Script, "【16—21秒｜成绩】") {
		t.Fatal("script body changed or markers not retimed")
	}
	next.Shots[0].Narration = "橘猫：未经确认的改写。"
	if validateFaithfulPlan(next, next.Shots) == nil {
		t.Fatal("unrelated dialogue drift accepted")
	}
	next.Shots[0] = shots[0]
	next.Shots[2].Narration = "橘猫：篡改覆盖。"
	if validateFaithfulPlan(next, next.Shots) == nil {
		t.Fatal("tampered authorization accepted")
	}
	v.Narration = "橘猫：" + strings.Repeat("长", 40)
	if _, e := changeShotContent(p, shots[2].ID, false, 4, v, defaultSettings()); e == nil {
		t.Fatal("overlong speech")
	}
	v.Narration = "陌生人：你好。"
	if _, e := changeShotContent(p, shots[2].ID, false, 6, v, defaultSettings()); e == nil {
		t.Fatal("unknown speaker")
	}
}
func TestShotRewriteStaleAndUnknownGuards(t *testing.T) {
	s := testStudio(t)
	p := contentProject(t, s)
	cfg, _ := s.store.settings()
	task := makeRewrite(t, s, p, false)
	body := map[string]any{"revision": p.Revision, "settings_version": cfg.Version + 1, "first_frame": "新首帧", "description": "新动作", "narration": ""}
	path := "/api/tasks/" + task.ID + "/rewrite/apply"
	request(t, s, "POST", path, body, 400)
	p, e := s.store.update(p.ID, p.Revision, func(p *Project) error { p.Title = "新标题"; return nil })
	if e != nil {
		t.Fatal(e)
	}
	body["revision"] = p.Revision
	body["settings_version"] = cfg.Version
	request(t, s, "POST", path, body, 409)
	unknown := Task{ID: uid(), ProjectID: p.ID, Stage: "shot_rewrite", ShotID: p.Shots[0].ID, TargetDuration: 6, Status: "failed", Input: p, Config: cfg, Mode: "remote"}
	s.store.db.Exec("INSERT INTO tasks VALUES(?,?,?,?,?,?)", unknown.ID, p.ID, unknown.Status, jsonBytes(unknown), jsonBytes(p), jsonBytes(cfg))
	s.store.db.Exec("INSERT INTO attempts(id,task_id,status) VALUES(?,?,'unknown')", unknown.ID+":shot-rewrite", unknown.ID)
	if e := s.rewriteShotTask(context.Background(), &unknown); e == nil {
		t.Fatal("unknown request retried")
	}
	body["duration"] = 6
	request(t, s, "PUT", "/api/projects/"+p.ID+"/shots/"+p.Shots[0].ID+"/content", body, 400)
	request(t, s, "POST", "/api/projects/"+p.ID+"/quotes", map[string]any{"revision": p.Revision, "stage": "shot_rewrite", "shot_id": p.Shots[0].ID, "target_duration": 6}, 400)
	if !reflect.DeepEqual(getProject(t, s, p.ID), p) {
		t.Fatal("guard changed project")
	}
}
func TestDraftRewriteRequiresReviewAndSurvivesAdoption(t *testing.T) {
	s := testStudio(t)
	p := stage(t, s, contentProject(t, s), "board_plan")
	before := string(jsonBytes(p.Shots))
	task := makeRewrite(t, s, p, true)
	cfg, _ := s.store.settings()
	body := map[string]any{"revision": p.Revision, "settings_version": cfg.Version, "first_frame": task.Rewrite.After.FirstFrame, "description": task.Rewrite.After.Description, "narration": ""}
	request(t, s, "POST", "/api/tasks/"+task.ID+"/rewrite/apply", body, 200)
	p = getProject(t, s, p.ID)
	if string(jsonBytes(p.Shots)) != before || p.PlannedShots[0].ReviewStatus != "pending" {
		t.Fatal("draft affected active shots")
	}
	for i := range p.PlannedShots {
		p.PlannedShots[i].ReviewStatus = "reviewed"
	}
	// A client may not forge authorization for another draft shot.
	p.PlannedShots[1].ContentOverride = &ShotContentOverride{Version: 1, Hash: contentHash(p.PlannedShots[1])}
	request(t, s, "PUT", "/api/projects/"+p.ID+"/board-plan", map[string]any{"revision": p.Revision, "shots": p.PlannedShots, "approve": true}, 200)
	p = getProject(t, s, p.ID)
	if !contentOverridden(p.Shots[0]) || p.Shots[1].ContentOverride != nil {
		t.Fatal("lost or forged override")
	}
	if err := validateFaithfulPlan(p, p.Shots); err != nil {
		t.Fatal(err)
	}
}

func TestRewriteRecoveryUsesSavedReceiptWithoutNewInference(t *testing.T) {
	s := testStudio(t)
	p := contentProject(t, s)
	cfg := defaultSettings()
	cfg.Mode = "remote"
	t.Setenv("TEXT_API_KEY", "unit-test-only")
	task := Task{ID: uid(), ProjectID: p.ID, Stage: "shot_rewrite", ShotID: p.Shots[0].ID, TargetDuration: 4, Status: "failed", Input: p, Config: cfg, Mode: "remote"}
	if _, e := s.store.db.Exec("INSERT INTO tasks VALUES(?,?,?,?,?,?)", task.ID, p.ID, task.Status, jsonBytes(task), jsonBytes(p), jsonBytes(cfg)); e != nil {
		t.Fatal(e)
	}
	response := map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": string(jsonBytes(ShotContent{FirstFrame: "接收的首帧", Description: "接收的动作", Narration: "短句"}))}}}}
	if _, e := s.store.db.Exec("INSERT INTO attempts(id,task_id,status,response) VALUES(?,?,'received',?)", task.ID+":shot-rewrite", task.ID, jsonBytes(response)); e != nil {
		t.Fatal(e)
	}
	if e := s.rewriteShotTask(context.Background(), &task); e != nil {
		t.Fatal(e)
	}
	if task.Rewrite == nil || task.Rewrite.After.Description != "接收的动作" {
		t.Fatal("saved result not recovered")
	}
	var count int
	s.store.db.QueryRow("SELECT count(*) FROM attempts WHERE task_id=?", task.ID).Scan(&count)
	if count != 1 {
		t.Fatal("extra submission")
	}
	if !reflect.DeepEqual(getProject(t, s, p.ID), p) {
		t.Fatal("recovery applied draft")
	}
}

func TestMockFilmIntegrationShotRewrite(t *testing.T) {
	if os.Getenv("LYPIT_INTEGRATION") != "1" {
		t.Skip("set LYPIT_INTEGRATION=1 for local FFmpeg validation")
	}
	s := testStudio(t)
	p := contentProject(t, s)
	cfg, _ := s.store.settings()
	task := makeRewrite(t, s, p, false)
	request(t, s, "POST", "/api/tasks/"+task.ID+"/rewrite/apply", map[string]any{"revision": p.Revision, "settings_version": cfg.Version, "first_frame": "【本地模拟】猫在路灯下", "description": "【本地模拟】猫向朋友挥手。", "narration": "朋友来了。"}, 200)
	p = getProject(t, s, p.ID)
	for _, q := range p.Shots {
		request(t, s, "PUT", "/api/projects/"+p.ID+"/shots/"+q.ID, map[string]any{"revision": p.Revision, "selected": q.Selected, "approved": true, "narration": q.Narration}, 200)
		p = getProject(t, s, p.ID)
	}
	p = lock(t, s, p, "boards")
	p = stage(t, s, p, "videos")
	p = stage(t, s, p, "render")
	if p.Duration != 11 || p.Output == "" || p.Shots[0].Duration != 6 || p.Shots[0].Narration != "朋友来了。" {
		t.Fatal("rewritten content not rendered", p)
	}
}
