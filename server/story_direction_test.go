package main

import (
	"context"
	"strings"
	"testing"
)

func executionFixture() shotExecution {
	return shotExecution{StartState: "阿灰在纸箱前，面向路灯", MainAction: "把纸箱拖到灯下并放稳", EndState: "纸箱停在灯下，阿灰松口", Bridge: "承接前镜准备课堂的决定", SetupSeconds: 1, ActionSeconds: 3, SettleSeconds: 1}
}

func TestShotExecutionBudgetsReserveCompletionTime(t *testing.T) {
	p := Project{AudioMode: "dialogue", Assets: []Asset{{ID: "cat", Kind: "character", Name: "阿灰"}}}
	q := Shot{Duration: 5, FirstFrame: "阿灰站在纸箱前", Description: "低机位跟随拖箱，抵达灯下放稳", References: []string{"cat"}}
	e := executionFixture()
	got, err := compileShotExecution(p, q, e)
	if err != nil || !strings.Contains(got.Description, "最后1秒收束") || !strings.Contains(got.Description, e.EndState) {
		t.Fatal(got, err)
	}
	if !strings.Contains(videoPrompt(p, got), e.EndState) || strings.Contains(boardFrame(got), e.EndState) {
		t.Fatal("endpoint missing from video or leaked into static frame")
	}
	for name, mutate := range map[string]func(*shotExecution){
		"no ending":       func(v *shotExecution) { v.EndState = "" },
		"no bridge":       func(v *shotExecution) { v.Bridge = "" },
		"no settling":     func(v *shotExecution) { v.SettleSeconds = 0; v.ActionSeconds = 4 },
		"over budget":     func(v *shotExecution) { v.ActionSeconds = 5 },
		"negative":        func(v *shotExecution) { v.SetupSeconds = -1 },
		"invented speech": func(v *shotExecution) { v.SpeechSeconds = 1; v.SpeechOverlap = true },
	} {
		t.Run(name, func(t *testing.T) {
			bad := e
			mutate(&bad)
			if _, err := compileShotExecution(p, q, bad); err == nil {
				t.Fatal("invalid budget accepted")
			}
		})
	}
	q.Narration = "阿灰：我也想当老师。"
	e.SpeechSeconds, e.SpeechOverlap = 2, true
	if _, err := compileShotExecution(p, q, e); err != nil {
		t.Fatal(err)
	}
	e.SpeechOverlap = false
	if _, err := compileShotExecution(p, q, e); err == nil {
		t.Fatal("sequential speech overrun accepted")
	}
	e.SpeechOverlap, e.SpeechSeconds = true, 1
	if _, err := compileShotExecution(p, q, e); err == nil {
		t.Fatal("rushed speech accepted")
	}
	// Narrator mode budgets the spoken words too, without parsing a speaker prefix.
	p.AudioMode, q.Narration = "narration", "它把纸箱拖到了灯下。"
	e.SpeechSeconds = 2
	if _, err := compileShotExecution(p, q, e); err != nil {
		t.Fatal(err)
	}
}

func saveDirectionReceipt(t *testing.T, s *Studio, task Task, key string, body any) {
	t.Helper()
	if _, err := s.store.db.Exec("INSERT INTO tasks VALUES(?,?,?,?,?,?)", task.ID, task.ProjectID, "failed", jsonBytes(task), jsonBytes(task.Input), jsonBytes(task.Config)); err != nil {
		t.Fatal(err)
	}
	response := map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": string(jsonBytes(body))}}}}
	if _, err := s.store.db.Exec("INSERT INTO attempts(id,task_id,status,response) VALUES(?,?,'received',?)", task.ID+":"+key, task.ID, jsonBytes(response)); err != nil {
		t.Fatal(err)
	}
}

func TestDirectedPlanRecoveryAndLegacyCompatibility(t *testing.T) {
	for _, version := range []int{0, storyDirectionVersion} {
		for _, invalid := range []bool{false, true} {
			s := testStudio(t)
			p := createProject(t, s)
			p.Duration, p.AudioMode = 10, "dialogue"
			p.Assets = []Asset{{ID: "cat", Kind: "character", Name: "阿灰"}}
			p.Script = "【0—5秒｜巷口】\n阿灰咬住纸箱。\n【5—10秒｜灯下】\n阿灰拖箱到灯下放稳。"
			beats, _ := scriptBeats(p)
			items := []directedShot{}
			for _, b := range beats {
				q := Shot{Title: "拖箱", Duration: 5, FirstFrame: "阿灰在纸箱前", Description: "完成拖箱后松口", SourceText: b.Text, References: []string{"cat"}}
				item := directedShot{Shot: q}
				if version > 0 {
					item.Execution = executionFixture()
				}
				if invalid {
					item.Execution.SettleSeconds = 0
				}
				items = append(items, item)
			}
			cfg := defaultSettings()
			cfg.Mode = "remote"
			job := Task{ID: uid(), ProjectID: p.ID, Stage: "board_plan", Mode: "remote", Config: cfg, Input: p, StoryDirectionVersion: version}
			saveDirectionReceipt(t, s, job, "boards-plan", map[string]any{"shots": items})
			for range 2 {
				copy := job
				err := s.planFaithfulShots(context.Background(), &copy)
				if version > 0 && invalid {
					if err == nil || len(copy.Input.PlannedShots) != 0 {
						t.Fatal("invalid plan applied", err)
					}
				} else {
					if err != nil || len(copy.Input.PlannedShots) != 2 {
						t.Fatal("receipt recovery failed", err)
					}
					if version > 0 && !strings.Contains(copy.Input.PlannedShots[0].Description, "切出前完成状态") {
						t.Fatal("lost execution instructions")
					}
				}
			}
			var count int
			s.store.db.QueryRow("SELECT count(*) FROM attempts WHERE task_id=?", job.ID).Scan(&count)
			if count != 1 {
				t.Fatal("recovery created another submission")
			}
		}
	}
}

func TestDirectedRewriteRecoveryPreservesActiveShot(t *testing.T) {
	s := testStudio(t)
	p := contentProject(t, s)
	cfg := defaultSettings()
	cfg.Mode = "remote"
	job := Task{ID: uid(), ProjectID: p.ID, ShotID: p.Shots[0].ID, Stage: "shot_rewrite", TargetDuration: 5, Mode: "remote", Config: cfg, Input: p, StoryDirectionVersion: storyDirectionVersion}
	result := struct {
		ShotContent
		Execution shotExecution `json:"execution"`
	}{ShotContent: ShotContent{FirstFrame: "人物在箱前", Description: "拖箱并放稳"}, Execution: executionFixture()}
	saveDirectionReceipt(t, s, job, "shot-rewrite", result)
	before := string(jsonBytes(job.Input))
	if err := s.rewriteShotTask(context.Background(), &job); err != nil {
		t.Fatal(err)
	}
	if job.Rewrite == nil || !strings.Contains(job.Rewrite.After.Description, "最后1秒收束") || string(jsonBytes(job.Input)) != before {
		t.Fatal("rewrite lost budget or changed active content")
	}
}

func TestDirectionVersionBoundToQuoteAndLegacySnapshots(t *testing.T) {
	s := testStudio(t)
	p := createProject(t, s)
	q, err := s.quoteFor(p, defaultSettings(), "analyze")
	if err != nil || q.StoryDirectionVersion != storyDirectionVersion {
		t.Fatal(q, err)
	}
	legacy, err := decodeBody[Task]([]byte(`{"stage":"analyze"}`))
	if err != nil || legacy.StoryDirectionVersion != 0 {
		t.Fatal("legacy snapshot upgraded", err)
	}
}

func TestDirectedScriptChecksTimingBeforeReplacingContent(t *testing.T) {
	for _, tc := range []struct {
		name, script, conflict string
		version                int
		valid                  bool
	}{
		{"legacy", "旧格式剧本", "", 0, true},
		{"timed", "【0—60秒｜主场景】\n主角看向窗外。", "", storyDirectionVersion, true},
		{"missing timeline", "没有时间段", "", storyDirectionVersion, false},
		{"wrong total", "【0—59秒｜主场景】\n主角看向窗外。", "", storyDirectionVersion, false},
		{"explicit conflict", "", "必须事件无法放入目标时长", storyDirectionVersion, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := testStudio(t)
			p := createProject(t, s)
			cfg := defaultSettings()
			cfg.Mode = "remote"
			job := Task{ID: uid(), ProjectID: p.ID, Stage: "analyze", Mode: "remote", Config: cfg, Input: p, StoryDirectionVersion: tc.version}
			saveDirectionReceipt(t, s, job, "analyze", map[string]any{"script": tc.script, "conflict": tc.conflict, "assets": []Asset{{Kind: "character", Name: "主角", Description: "人物"}, {Kind: "scene", Name: "主场景", Description: "场景"}}})
			before := string(jsonBytes(job.Input))
			err := s.analyze(context.Background(), &job)
			if (err == nil) != tc.valid {
				t.Fatal(err)
			}
			if !tc.valid && string(jsonBytes(job.Input)) != before {
				t.Fatal("invalid script replaced project content")
			}
		})
	}
}
