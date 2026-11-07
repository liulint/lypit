package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func defaultPromptMap() map[string]string { return snapshotPrompts(defaultSettings()).PromptTemplates }

func TestPromptCatalogValidationAndLiteralSubstitution(t *testing.T) {
	if len(promptDefinitions) != 10 {
		t.Fatal("prompt catalog incomplete")
	}
	seen := map[string]bool{}
	for _, d := range promptDefinitions {
		if seen[d.ID] {
			t.Fatal("duplicate prompt type", d.ID)
		}
		seen[d.ID] = true
		if err := validatePrompt(d, d.Default); err != nil {
			t.Fatal(d.ID, err)
		}
		vars := map[string]string{}
		for _, key := range promptKeys(d.Default) {
			if promptVariableLabels[key] == "" {
				t.Fatal("undocumented variable", key)
			}
			vars[key] = "literal-{{do_not_expand}}"
		}
		job := Task{PromptTemplatesVersion: 1, Config: snapshotPrompts(defaultSettings())}
		text, err := job.renderPrompt(d.ID, "legacy", vars)
		if err != nil || !strings.Contains(text, "literal-{{do_not_expand}}") {
			t.Fatal(d.ID, err)
		}
		for _, bad := range []string{"", d.Default + "{{unknown}}", d.Default + "{{ unclosed", strings.Repeat("x", maxPromptBytes+1), strings.ReplaceAll(d.Default, "{{"+promptKeys(d.Default)[0]+"}}", "removed")} {
			if err := validatePrompt(d, bad); err == nil {
				t.Fatal("invalid template accepted", d.ID)
			}
		}
	}
	job := Task{PromptTemplatesVersion: 1, Config: defaultSettings()}
	if _, err := job.renderPrompt("video", "legacy", nil); err == nil {
		t.Fatal("missing snapshot silently fell back")
	}
	job.PromptTemplatesVersion = 0
	if text, err := job.renderPrompt("video", "exact historical prompt", nil); err != nil || text != "exact historical prompt" {
		t.Fatal("legacy text changed")
	}
}

func TestPromptSaveIsAtomicVersionedAndSurvivesSettingsUpdates(t *testing.T) {
	s := testStudio(t)
	var first struct {
		Version int               `json:"version"`
		Items   []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(request(t, s, "GET", "/api/prompts", nil, 200), &first); err != nil || len(first.Items) != 10 {
		t.Fatal("catalog", err)
	}
	text := defaultPromptMap()
	text["analyze"] += "\n用具体行为表现人物选择。"
	request(t, s, "PUT", "/api/prompts", map[string]any{"version": first.Version, "templates": text}, 200)
	cfg, _ := s.store.settings()
	if cfg.Version != first.Version+1 || cfg.PromptTemplates["analyze"] != text["analyze"] {
		t.Fatal("not saved")
	}
	request(t, s, "PUT", "/api/prompts", map[string]any{"version": first.Version, "templates": defaultPromptMap()}, 409)
	invalid := defaultPromptMap()
	invalid["video"] = "missing variables"
	request(t, s, "PUT", "/api/prompts", map[string]any{"version": cfg.Version, "templates": invalid}, 400)
	current, _ := s.store.settings()
	if current.Version != cfg.Version || current.PromptTemplates["analyze"] != text["analyze"] {
		t.Fatal("invalid save partially applied")
	}
	// A normal service save cannot reset prompts, even if an old client omits them.
	cfg.PromptTemplates = nil
	request(t, s, "PUT", "/api/settings", cfg, 200)
	current, _ = s.store.settings()
	if current.PromptTemplates["analyze"] != text["analyze"] {
		t.Fatal("service settings reset custom prompts")
	}
	request(t, s, "PUT", "/api/prompts", map[string]any{"version": current.Version, "templates": defaultPromptMap()}, 200)
	current, _ = s.store.settings()
	if len(current.PromptTemplates) != 0 {
		t.Fatal("restore default left overrides")
	}
	version := current.Version
	request(t, s, "PUT", "/api/prompts", map[string]any{"version": version, "templates": defaultPromptMap()}, 200)
	current, _ = s.store.settings()
	if current.Version != version {
		t.Fatal("no-op invalidates quotes")
	}
}

func TestPromptChangesInvalidateQuotesAndFreezeConfirmedTasks(t *testing.T) {
	s := testStudio(t)
	p := createProject(t, s)
	var quote Quote
	json.Unmarshal(request(t, s, "POST", "/api/projects/"+p.ID+"/quotes", map[string]any{"revision": p.Revision, "stage": "analyze"}, 200), &quote)
	cfg, _ := s.store.settings()
	text := defaultPromptMap()
	text["analyze"] += "\nfirst-custom-marker"
	request(t, s, "PUT", "/api/prompts", map[string]any{"version": cfg.Version, "templates": text}, 200)
	request(t, s, "POST", "/api/quotes/"+quote.ID+"/confirm", map[string]bool{"allow_unknown": false}, 400)
	json.Unmarshal(request(t, s, "POST", "/api/projects/"+p.ID+"/quotes", map[string]any{"revision": p.Revision, "stage": "analyze"}, 200), &quote)
	var public Task
	body := request(t, s, "POST", "/api/quotes/"+quote.ID+"/confirm", map[string]bool{"allow_unknown": false}, 200)
	json.Unmarshal(body, &public)
	if strings.Contains(string(body), "first-custom-marker") {
		t.Fatal("private prompt snapshot exposed")
	}
	waitTask(t, s, public.ID)
	job, err := s.store.task(public.ID)
	if err != nil || job.PromptTemplatesVersion != 1 || len(job.Config.PromptTemplates) != 10 || !strings.Contains(job.Config.PromptTemplates["analyze"], "first-custom-marker") {
		t.Fatal("snapshot incomplete", err)
	}
	cfg, _ = s.store.settings()
	text["analyze"] += "\nsecond-custom-marker"
	request(t, s, "PUT", "/api/prompts", map[string]any{"version": cfg.Version, "templates": text}, 200)
	loaded, _ := s.store.task(public.ID)
	if strings.Contains(loaded.Config.PromptTemplates["analyze"], "second-custom-marker") {
		t.Fatal("task read current prompt")
	}
	// Snapshots own their map even before serialization.
	cfg.PromptTemplates = text
	frozen := snapshotPrompts(cfg)
	text["analyze"] = "mutated"
	if frozen.PromptTemplates["analyze"] == "mutated" {
		t.Fatal("snapshot shares mutable map")
	}
}

func TestManagedImageAndVideoUseTaskTemplates(t *testing.T) {
	s := testStudio(t)
	p := readyRetryProject(t, s)
	_, job := editFixture(t, s, p, []string{})
	job.PromptTemplatesVersion = 1
	job.Config = snapshotPrompts(job.Config)
	job.Config.PromptTemplates["board_edit"] += "\nimage-edit-marker"
	calls := 0
	err := s.editBoard(context.Background(), &job, func(_ context.Context, _ *Task, _ string, prompt string, _ []Image, _ bool) (Image, error) {
		calls++
		if !strings.Contains(prompt, "image-edit-marker") || !strings.Contains(prompt, job.ImageEdit.Changes) || !strings.Contains(prompt, editContract) {
			t.Fatal("edit did not use snapshot", prompt)
		}
		return Image{}, context.Canceled
	})
	if err == nil || calls != 1 {
		t.Fatal("unexpected edit call", err)
	}
	q := job.Input.Shots[0]
	job.Config.PromptTemplates["video"] += "\nvideo-marker"
	prompt, err := taskVideoPrompt(job, q)
	if err != nil || !strings.Contains(prompt, "video-marker") || !strings.Contains(prompt, q.Description) {
		t.Fatal("video did not use template", err)
	}
}

func TestManagedTextStagesSendEditedTemplatesAndRecoverWithoutResubmission(t *testing.T) {
	for _, key := range []string{"analyze", "board_plan", "shot_rewrite"} {
		t.Run(key, func(t *testing.T) {
			s := testStudio(t)
			p := createProject(t, s)
			p.Duration, p.AudioMode = 10, "dialogue"
			p.Script = "【0—5秒｜窗边】\n阿灰抬头。\n【5—10秒｜窗边】\n阿灰放下前爪。"
			p.Assets = []Asset{{ID: "cat", Kind: "character", Name: "阿灰", Description: "灰猫"}}
			beats, _ := scriptBeats(p)
			items := []directedShot{}
			for i, b := range beats {
				q := Shot{ID: string(rune('a' + i)), Title: "静观", Duration: 5, SourceText: b.Text, FirstFrame: "猫在窗边", Description: "猫完成一次动作", References: []string{"cat"}}
				items = append(items, directedShot{Shot: q, Execution: executionFixture()})
				p.Shots = append(p.Shots, q)
			}
			var answer any
			switch key {
			case "analyze":
				answer = map[string]any{"script": p.Script, "assets": []Asset{{Kind: "character", Name: "阿灰", Description: "灰猫"}, {Kind: "scene", Name: "窗边", Description: "窗"}}}
			case "board_plan":
				answer = map[string]any{"shots": items}
			case "shot_rewrite":
				answer = struct {
					ShotContent
					Execution shotExecution `json:"execution"`
				}{ShotContent: shotContent(p.Shots[0]), Execution: executionFixture()}
			}
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var body struct {
					Messages []struct {
						Content string `json:"content"`
					} `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if len(body.Messages) != 2 || !strings.Contains(body.Messages[1].Content, "edited-"+key) || strings.Contains(body.Messages[1].Content, "{{output_contract}}") {
					t.Error("edited template not rendered into actual request")
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": string(jsonBytes(answer))}}}})
			}))
			defer server.Close()
			t.Setenv("TEXT_API_KEY", "local-test-only")
			cfg := snapshotPrompts(defaultSettings())
			cfg.Mode = "remote"
			cfg.Text.Base = server.URL
			cfg.PromptTemplates[key] += "\nedited-" + key
			job := Task{ID: uid(), ProjectID: p.ID, Stage: key, Input: p, Config: cfg, Mode: "remote", PromptTemplatesVersion: 1, StoryDirectionVersion: 1, ShotID: p.Shots[0].ID, TargetDuration: 5}
			if _, err := s.store.db.Exec("INSERT INTO tasks VALUES(?,?,?,?,?,?)", job.ID, p.ID, "failed", jsonBytes(job), jsonBytes(p), jsonBytes(cfg)); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				copy := job
				var err error
				switch key {
				case "analyze":
					err = s.analyze(context.Background(), &copy)
				case "board_plan":
					err = s.planFaithfulShots(context.Background(), &copy)
				case "shot_rewrite":
					err = s.rewriteShotTask(context.Background(), &copy)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			if calls.Load() != 1 {
				t.Fatal("recovery resubmitted text generation", calls.Load())
			}
		})
	}
}
