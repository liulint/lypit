package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func oldPromptDefaults(t *testing.T) map[string]string {
	t.Helper()
	b, err := os.ReadFile("testdata/prompt_templates_v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var defaults map[string]string
	if err = json.Unmarshal(b, &defaults); err != nil {
		t.Fatal(err)
	}
	return defaults
}

func TestTextRevisionPreservesSevenMediaDefaultsAndOldSnapshots(t *testing.T) {
	old := oldPromptDefaults(t)
	for _, def := range promptDefinitions {
		previous, ok := old[def.ID]
		if !ok {
			t.Fatal("missing historical default", def.ID)
		}
		textStage := def.ID == "analyze" || def.ID == "board_plan" || def.ID == "shot_rewrite"
		if !textStage && previous != def.Default {
			t.Fatal("media prompt changed in text-only revision", def.ID)
		}
		if textStage && previous == def.Default {
			t.Fatal("text revision not installed", def.ID)
		}
		vars := map[string]string{}
		for _, key := range promptKeys(previous) {
			vars[key] = "sample-" + key
		}
		job := Task{PromptTemplatesVersion: 1, Config: Settings{PromptTemplates: old}}
		before, err := job.renderPrompt(def.ID, "unused", vars)
		if err != nil {
			t.Fatal("version 1 task cannot recover after upgrade", def.ID, err)
		}
		// Recreate a serialized immutable task, as the recovery path does.
		cfg, _ := decodeBody[Settings](jsonBytes(job.Config))
		loaded := Task{PromptTemplatesVersion: 1, Config: cfg}
		after, err := loaded.renderPrompt(def.ID, "unused", vars)
		if err != nil || before != after {
			t.Fatal("old prompt changed across recovery", def.ID, err)
		}
		if textStage && strings.Contains(after, "本次已授权的文字") {
			t.Fatal("old task was silently upgraded", def.ID)
		}
	}
}

func TestTextRevisionKeepsUserOverridesAndRejectsUnknownFutureVersion(t *testing.T) {
	old := oldPromptDefaults(t)
	custom := old["analyze"] + "\n用户自己保留的风格指令"
	cfg := snapshotPrompts(Settings{PromptTemplates: map[string]string{"analyze": custom}})
	if cfg.PromptTemplates["analyze"] != custom {
		t.Fatal("user override replaced by new default")
	}
	if cfg.PromptTemplates["board_plan"] == old["board_plan"] {
		t.Fatal("untouched defaults did not upgrade")
	}
	job := Task{PromptTemplatesVersion: promptTemplatesVersion + 1, Config: cfg}
	if _, err := job.renderPrompt("analyze", "fallback", map[string]string{}); err == nil {
		t.Fatal("unknown future policy accepted")
	}
}

// Revision 2 was actually submitted before the live review. Its immutable
// snapshot must not pick up the changes prompted by that very response.
func TestTextRevisionPreservesLiveTestVersionTwoSnapshot(t *testing.T) {
	b, err := os.ReadFile("testdata/prompt_templates_v2.json")
	if err != nil {
		t.Fatal(err)
	}
	var defaults map[string]string
	if err = json.Unmarshal(b, &defaults); err != nil {
		t.Fatal(err)
	}
	job := Task{PromptTemplatesVersion: 2, Config: Settings{PromptTemplates: defaults}}
	for _, def := range promptDefinitions {
		vars := map[string]string{}
		for _, key := range promptKeys(defaults[def.ID]) {
			vars[key] = "sample-" + key
		}
		got, err := job.renderPrompt(def.ID, "unused", vars)
		want := defaults[def.ID]
		for key, value := range vars {
			want = strings.ReplaceAll(want, "{{"+key+"}}", value)
		}
		if err != nil || got != want {
			t.Fatal("live-test snapshot changed", def.ID, err)
		}
		if (def.ID == "analyze" || def.ID == "board_plan" || def.ID == "shot_rewrite") && defaults[def.ID] == def.Default {
			t.Fatal("live review did not revise text stage", def.ID)
		}
	}
}

// This is a human-authored example checked for production invariants. It is
// deliberately not a claim that a configured model generated or acted it.
func TestTextDirectionAcceptanceSampleFitsTimingAndPreservesFidelity(t *testing.T) {
	var sample struct {
		Project Project        `json:"project"`
		Shots   []directedShot `json:"shots"`
	}
	b, err := os.ReadFile("testdata/text_direction_acceptance.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &sample); err != nil {
		t.Fatal(err)
	}
	shots := []Shot{}
	for i, item := range sample.Shots {
		q, err := compileShotExecution(sample.Project, item.Shot, item.Execution)
		if err != nil {
			t.Fatalf("sample shot %d: %v", i+1, err)
		}
		shots = append(shots, q)
	}
	if err = validateFaithfulPlan(sample.Project, shots); err != nil {
		t.Fatal(err)
	}
	if err = validatePlanVideoDurations(defaultSettings(), shots); err != nil {
		t.Fatal(err)
	}
	// A short clip must not silently absorb the same amount of speech or action.
	tooShort := sample.Shots[4]
	tooShort.Duration = 3
	if _, err = compileShotExecution(sample.Project, tooShort.Shot, tooShort.Execution); err == nil {
		t.Fatal("accepted overfilled invitation")
	}
}
