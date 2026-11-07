package main

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func saveTrialDraft(t *testing.T, s *Studio, p Project) Project {
	t.Helper()
	request(t, s, "PUT", "/api/projects/"+p.ID+"/board-plan", map[string]any{"revision": p.Revision, "shots": p.PlannedShots, "approve": false}, 200)
	return getProject(t, s, p.ID)
}

func trialQuote(t *testing.T, s *Studio, p Project, stage string) Quote {
	t.Helper()
	var q Quote
	if err := json.Unmarshal(request(t, s, "POST", "/api/projects/"+p.ID+"/quotes", map[string]any{"revision": p.Revision, "stage": stage, "shot_id": p.PlannedShots[0].ID, "plan_trial": true}, 200), &q); err != nil {
		t.Fatal(err)
	}
	if !q.PlanTrial || len(q.Items) != 1 || q.Items[0].Count != 1 {
		t.Fatal("wrong trial scope", q)
	}
	return q
}

func runTrialQuote(t *testing.T, s *Studio, q Quote) Task {
	t.Helper()
	var task Task
	json.Unmarshal(request(t, s, "POST", "/api/quotes/"+q.ID+"/confirm", map[string]any{}, 200), &task)
	if !task.PlanTrial {
		t.Fatal("lost trial scope")
	}
	waitTask(t, s, task.ID)
	return task
}

func TestPlanTrialImageThenVideoBeforeOtherReviewsAndAdoption(t *testing.T) {
	s := testStudio(t)
	p := stage(t, s, readyRetryProject(t, s), "board_plan")
	active := string(jsonBytes(p.Shots))
	planApproved := p.PlanApproved
	cfg := defaultSettings()
	id := p.PlannedShots[0].ID
	if _, err := s.quoteFor(p, cfg, "board_retry", ShotRequest{PlanTrial: true, ShotID: id}); err == nil {
		t.Fatal("unreviewed trial accepted")
	}
	p.PlannedShots[0].ReviewStatus = "reviewed"
	p = saveTrialDraft(t, s, p)
	if _, err := s.quoteFor(p, cfg, "board_retry", ShotRequest{PlanTrial: true, ShotID: p.PlannedShots[1].ID}); err == nil {
		t.Fatal("other unreviewed shot accepted")
	}
	if _, err := s.quoteFor(p, cfg, "shot_video", ShotRequest{PlanTrial: true, ShotID: id}); err == nil {
		t.Fatal("video before frame accepted")
	}
	q := trialQuote(t, s, p, "board_retry")
	if got := getProject(t, s, p.ID); !reflect.DeepEqual(got, p) {
		t.Fatal("quote changed project")
	}
	task := runTrialQuote(t, s, q)
	var duplicate Task
	json.Unmarshal(request(t, s, "POST", "/api/quotes/"+q.ID+"/confirm", map[string]any{}, 200), &duplicate)
	if task.ID != duplicate.ID {
		t.Fatal("duplicate confirmation created new work")
	}
	p = getProject(t, s, p.ID)
	if string(jsonBytes(p.Shots)) != active || p.PlanApproved != planApproved || len(p.PlannedShots[0].Candidates) != 1 || len(p.PlannedShots[1].Candidates) != 0 || p.PlannedShots[1].ReviewStatus == "reviewed" {
		t.Fatal("trial replaced active or generated other shot")
	}
	if _, err := s.quoteFor(p, cfg, "shot_video", ShotRequest{PlanTrial: true, ShotID: id}); err == nil {
		t.Fatal("video before frame confirmation accepted")
	}
	first := p.PlannedShots[0]
	request(t, s, "PUT", "/api/projects/"+p.ID+"/shots/"+id, map[string]any{"revision": p.Revision, "plan_trial": true, "selected": first.Selected, "approved": true, "narration": first.Narration}, 200)
	p = getProject(t, s, p.ID)
	q = trialQuote(t, s, p, "shot_video")
	if q.Duration != first.Duration {
		t.Fatal("wrong trial seconds", q.Duration)
	}
	videoTask := runTrialQuote(t, s, q)
	p = getProject(t, s, p.ID)
	if p.PlannedShots[0].Video == "" || string(jsonBytes(p.Shots)) != active || p.PlannedShots[1].Video != "" {
		t.Fatal("video scope incorrect")
	}
	if _, err := os.Stat(s.media(p.PlannedShots[0].Video)); err != nil {
		t.Fatal(err)
	}
	savedTask, err := s.store.task(videoTask.ID)
	if err != nil || savedTask.Input.Shots[0].Video != p.PlannedShots[0].Video {
		t.Fatal("trial supplier provenance lost", err)
	}
	result := p.PlannedShots[0]
	p = saveTrialDraft(t, s, p)
	if !reflect.DeepEqual(p.PlannedShots[0], result) {
		t.Fatal("draft save discarded trial media")
	}
	for i := range p.PlannedShots {
		p.PlannedShots[i].ReviewStatus = "reviewed"
	}
	request(t, s, "PUT", "/api/projects/"+p.ID+"/board-plan", map[string]any{"revision": p.Revision, "shots": p.PlannedShots, "approve": true}, 200)
	p = getProject(t, s, p.ID)
	if !reflect.DeepEqual(p.Shots[0], result) || len(p.PlannedShots) != 0 || !p.PlanApproved {
		t.Fatal("adoption discarded trial result")
	}
	if _, err := s.quoteFor(p, cfg, "boards"); err != nil {
		t.Fatal("remaining image quote failed", err)
	}
}

func TestPlanTrialDraftChangesInvalidateOnlyChangedResults(t *testing.T) {
	s := testStudio(t)
	p := stage(t, s, readyRetryProject(t, s), "board_plan")
	p.PlannedShots[0].ReviewStatus = "reviewed"
	p = saveTrialDraft(t, s, p)
	runTrialQuote(t, s, trialQuote(t, s, p, "board_retry"))
	p = getProject(t, s, p.ID)
	old := p.PlannedShots[0]
	p.PlannedShots[1].Description = "只修改第二镜"
	p = saveTrialDraft(t, s, p)
	if !reflect.DeepEqual(p.PlannedShots[0], old) {
		t.Fatal("other draft edit lost trial result")
	}
	var stale Quote
	json.Unmarshal(request(t, s, "POST", "/api/projects/"+p.ID+"/quotes", map[string]any{"revision": p.Revision, "stage": "board_retry", "shot_id": old.ID, "plan_trial": true, "feedback": "调整构图"}, 200), &stale)
	p.PlannedShots[0].FirstFrame = "新的首帧构图"
	p.PlannedShots[0].ReviewStatus = "pending"
	// Supplying old result fields from the browser must not preserve confirmation.
	p = saveTrialDraft(t, s, p)
	if len(p.PlannedShots[0].Candidates) != 0 || p.PlannedShots[0].Approved || p.PlannedShots[0].Video != "" {
		t.Fatal("changed draft retained obsolete results")
	}
	found := false
	for _, archived := range p.PreviousShots {
		found = found || reflect.DeepEqual(archived, old)
	}
	if !found {
		t.Fatal("old trial media was not archived")
	}
	request(t, s, "POST", "/api/quotes/"+stale.ID+"/confirm", map[string]any{}, 400)
	if _, err := os.Stat(s.media(old.Candidates[old.Selected].File)); err != nil {
		t.Fatal("old file removed", err)
	}
}

func TestPlanTrialFocusedValidationAndCommitConflict(t *testing.T) {
	p, shots := timedPlanFixture()
	shots[0].ReviewStatus = "reviewed"
	shots[1].FirstFrame = "" // An unfinished other shot must not block this trial.
	p.PlannedShots = shots
	project, err := planTrialProject(p, shots[0].ID)
	if err != nil || project.Shots[0].ID != shots[0].ID {
		t.Fatal("unreviewed other draft blocked trial", err)
	}
	shots[0].SourceText = "wrong source"
	if _, err := planTrialProject(p, shots[0].ID); err == nil {
		t.Fatal("invalid target source accepted")
	}

	s := testStudio(t)
	p = stage(t, s, readyRetryProject(t, s), "board_plan")
	p.PlannedShots[0].ReviewStatus = "reviewed"
	p = saveTrialDraft(t, s, p)
	project, err = planTrialProject(p, p.PlannedShots[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	job := cliTask(t, s)
	job.ProjectID, job.Input, job.PlanTrial, job.ShotID, job.Stage = p.ID, project, true, p.PlannedShots[0].ID, "board_retry"
	if err := s.regenerateBoard(context.Background(), &job, func(context.Context, *Task, string, string, []Image, bool) (Image, error) {
		return Image{File: "fake.png", Hash: "fake"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	p = saveTrialDraft(t, s, p)
	if err := s.commitTask(job); err != errConflict {
		t.Fatal("stale trial overwrote edited draft", err)
	}
}
