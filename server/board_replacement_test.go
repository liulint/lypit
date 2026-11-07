package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func stoppedMissingBoard(t *testing.T, s *Studio) (Project, Task) {
	t.Helper()
	p := readyRetryProject(t, s)
	p.Shots[1].Candidates = []Image{{}}
	if _, err := s.store.db.Exec("UPDATE projects SET body=? WHERE id=?", jsonBytes(p), p.ID); err != nil {
		t.Fatal(err)
	}
	input, _ := decodeBody[Project](jsonBytes(p))
	old := Task{ID: uid(), ProjectID: p.ID, Stage: "boards", Status: "failed", Input: input, Mode: "mock", Config: defaultSettings(), BoardImagesPerShot: 1}
	old.Config.Image = Endpoint{Transport: "cli", CLI: "codex", Executable: "/never-launch-this-cli", Model: "test"}
	if _, err := s.store.db.Exec("INSERT INTO tasks VALUES(?,?,?,?,?,?)", old.ID, p.ID, old.Status, jsonBytes(old), jsonBytes(old.Input), jsonBytes(old.Config)); err != nil {
		t.Fatal(err)
	}
	intent := cliImageIntent{Root: t.TempDir(), Started: time.Now().Add(-time.Second)}
	key := "shot-" + p.Shots[1].ID + "-0"
	dir := filepath.Join(s.data, "cli", old.ID, key)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "image-receipt.json"), []byte(`{"source":""}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.db.Exec("INSERT INTO attempts(id,task_id,status,response) VALUES(?,?,'unknown',?)", old.ID+":"+key, old.ID, jsonBytes(intent)); err != nil {
		t.Fatal(err)
	}
	return p, old
}

func replacementQuote(t *testing.T, s *Studio, p Project, old Task) Quote {
	t.Helper()
	var q Quote
	json.Unmarshal(request(t, s, "POST", "/api/projects/"+p.ID+"/quotes", map[string]any{"revision": p.Revision, "stage": "board_retry", "shot_id": p.Shots[1].ID, "replaces_task_id": old.ID}, 200), &q)
	return q
}

func TestMissingBoardReplacementRequiresNewConsentAndPreservesOriginal(t *testing.T) {
	s := testStudio(t)
	p, old := stoppedMissingBoard(t, s)
	before := jsonBytes(p)
	oldInput := jsonBytes(old.Input)
	q := replacementQuote(t, s, p, old)
	if len(q.Items) != 1 || q.Items[0].Count != 1 || q.ReplacesTaskID != old.ID || !strings.Contains(q.Items[0].Label, "补生成") {
		t.Fatal("incorrect replacement scope", q)
	}
	if got := getProject(t, s, p.ID); string(jsonBytes(got)) != string(before) {
		t.Fatal("quoting modified original project")
	}
	if got, _ := s.store.task(old.ID); got.Status != "failed" {
		t.Fatal("quoting abandoned old task")
	}
	message := request(t, s, "POST", "/api/tasks/"+old.ID+"/resume", map[string]any{}, 400)
	if !strings.Contains(string(message), "无法恢复") || !strings.Contains(string(message), "重新报价") {
		t.Fatal("recovery has no actionable reason", string(message))
	}
	request(t, s, "POST", "/api/quotes/"+q.ID+"/confirm", map[string]any{}, 400)
	var job Task
	json.Unmarshal(request(t, s, "POST", "/api/quotes/"+q.ID+"/confirm", map[string]any{"allow_replacement": true}, 200), &job)
	waitTask(t, s, job.ID)
	got := getProject(t, s, p.ID)
	if !reflect.DeepEqual(got.Shots[0], p.Shots[0]) || !shotHasImage(got.Shots[1]) || got.Shots[1].Approved {
		t.Fatal("replacement modified unrelated shot or failed to fill target")
	}
	saved, _ := s.store.task(old.ID)
	if saved.Status != "superseded" || string(jsonBytes(saved.Input)) != string(oldInput) {
		t.Fatal("old task/receipt not retained")
	}
	request(t, s, "POST", "/api/tasks/"+old.ID+"/resume", map[string]any{}, 400)
	var again Task
	json.Unmarshal(request(t, s, "POST", "/api/quotes/"+q.ID+"/confirm", map[string]any{"allow_replacement": true}, 200), &again)
	if again.ID != job.ID {
		t.Fatal("duplicate confirmation created another paid task")
	}
}

func TestMissingBoardReplacementRejectsOtherTargetsAndChangedReceipt(t *testing.T) {
	s := testStudio(t)
	p, old := stoppedMissingBoard(t, s)
	request(t, s, "POST", "/api/projects/"+p.ID+"/quotes", map[string]any{"revision": p.Revision, "stage": "board_retry", "shot_id": p.Shots[0].ID, "replaces_task_id": old.ID, "feedback": "change"}, 400)
	q := replacementQuote(t, s, p, old)
	if _, err := s.store.db.Exec("UPDATE attempts SET response=? WHERE task_id=?", []byte(`{"changed":true}`), old.ID); err != nil {
		t.Fatal(err)
	}
	request(t, s, "POST", "/api/quotes/"+q.ID+"/confirm", map[string]any{"allow_replacement": true}, 400)
	if got, _ := s.store.task(old.ID); got.Status != "failed" {
		t.Fatal("stale quote retired old task")
	}
}
