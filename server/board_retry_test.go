package main

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func readyRetryProject(t *testing.T, s *Studio) Project {
	t.Helper()
	p := createProject(t, s)
	p.Duration = 10
	request(t, s, "PUT", "/api/projects/"+p.ID, p, 200)
	p = getProject(t, s, p.ID)
	p = stage(t, s, p, "analyze")
	p = stage(t, s, p, "masters")
	for _, a := range p.Assets {
		if a.Kind == "character" {
			p = approve(t, s, p, a.ID, "master")
		}
	}
	p = stage(t, s, p, "details")
	for _, a := range p.Assets {
		part := "master"
		if a.Kind == "character" {
			part = "sheet"
		}
		p = approve(t, s, p, a.ID, part)
	}
	p = lock(t, s, p, "assets")
	return stage(t, s, p, "boards")
}

func TestSingleBoardImageCountAndLegacyAuthorization(t *testing.T) {
	s := testStudio(t)
	job := boardTask(t, s)
	job.BoardImagesPerShot = 1
	var calls atomic.Int32
	err := s.fillBoardImages(context.Background(), &job, func(_ context.Context, _ *Task, key, prompt string, _ []Image, _ bool) (Image, error) {
		calls.Add(1)
		return Image{File: key + ".png", Hash: key}, nil
	})
	if err != nil || calls.Load() != 2 {
		t.Fatal("expected exactly one image per shot", calls.Load(), err)
	}
	for _, q := range job.Input.Shots {
		if len(q.Candidates) != 1 {
			t.Fatal("multiple images", q)
		}
	}
	var preview struct {
		Total int `json:"total"`
	}
	json.Unmarshal(request(t, s, "GET", "/api/tasks/"+job.ID+"/preview", nil, 200), &preview)
	if preview.Total != 2 || boardImagesPerShot(Task{}) != 2 {
		t.Fatal("new or legacy count changed", preview)
	}
}

func TestShotRequestQuoteFreezesAdviceAndOnlyChangesTarget(t *testing.T) {
	s := testStudio(t)
	p := readyRetryProject(t, s)
	old := jsonBytes(p)
	for _, q := range p.Shots {
		if len(q.Candidates) != 1 {
			t.Fatal("initial generation made multiple images")
		}
	}
	body := map[string]any{"stage": "board_retry", "revision": p.Revision, "shot_id": p.Shots[0].ID, "feedback": " 改成近景，光线暗一点 "}
	var quote Quote
	json.Unmarshal(request(t, s, "POST", "/api/projects/"+p.ID+"/quotes", body, 200), &quote)
	if len(quote.Items) != 1 || quote.Items[0].Count != 1 || quote.Feedback != "改成近景，光线暗一点" || quote.ShotID != p.Shots[0].ID {
		t.Fatal("bad retry quote", quote)
	}
	if string(old) != string(jsonBytes(getProject(t, s, p.ID))) {
		t.Fatal("quoting changed the original")
	}
	var job Task
	json.Unmarshal(request(t, s, "POST", "/api/quotes/"+quote.ID+"/confirm", map[string]any{"allow_unknown": false, "feedback": "tampered", "shot_id": p.Shots[1].ID}, 200), &job)
	waitTask(t, s, job.ID)
	var duplicate Task
	json.Unmarshal(request(t, s, "POST", "/api/quotes/"+quote.ID+"/confirm", map[string]bool{}, 200), &duplicate)
	if duplicate.ID != job.ID {
		t.Fatal("duplicate confirmation submitted new work")
	}
	saved, e := s.store.task(job.ID)
	if e != nil || saved.Feedback != quote.Feedback {
		t.Fatal("advice not frozen", e)
	}
	got := getProject(t, s, p.ID)
	if !reflect.DeepEqual(got.Shots[1:], p.Shots[1:]) || !reflect.DeepEqual(got.Assets, p.Assets) {
		t.Fatal("unrelated images changed")
	}
	target := got.Shots[0]
	if target.Selected != 1 || len(target.Candidates) != 2 || !reflect.DeepEqual(target.Candidates[0], p.Shots[0].Candidates[0]) || target.Approved || target.Narration != p.Shots[0].Narration {
		t.Fatal("bad retry result", target)
	}
	if e = s.verifyImage(target.Candidates[0]); e != nil {
		t.Fatal("original file lost", e)
	}
	if e = s.verifyImage(target.Candidates[1]); e != nil {
		t.Fatal("new image invalid", e)
	}
	// Saved completed input must be safe to resume without submitting or appending again.
	hits := 0
	if e = s.regenerateBoard(context.Background(), &saved, func(context.Context, *Task, string, string, []Image, bool) (Image, error) {
		hits++
		return Image{}, errors.New("unexpected")
	}); e != nil || hits != 0 {
		t.Fatal("completed retry was repeated", e, hits)
	}
}

func TestShotRequestAdviceErrorsAndFailurePreserveOriginal(t *testing.T) {
	s := testStudio(t)
	p := readyRetryProject(t, s)
	for _, v := range []ShotRequest{{ShotID: p.Shots[0].ID, Feedback: " "}, {ShotID: "missing", Feedback: "近景"}, {ShotID: p.Shots[0].ID, Feedback: strings.Repeat("字", 1001)}} {
		if _, e := s.quoteFor(p, defaultSettings(), "board_retry", v); e == nil {
			t.Fatal("invalid retry accepted", v.ShotID)
		}
	}
	before := string(jsonBytes(p))
	job := Task{ID: uid(), ProjectID: p.ID, Stage: "board_retry", Input: p, ShotID: p.Shots[0].ID, Feedback: "改成近景"}
	count := 0
	e := s.regenerateBoard(context.Background(), &job, func(_ context.Context, _ *Task, key, prompt string, refs []Image, _ bool) (Image, error) {
		count++
		if !strings.Contains(prompt, job.Feedback) || !strings.Contains(prompt, p.Shots[0].FirstFrame) || len(refs) == 0 {
			t.Error("missing original context or advice")
		}
		return Image{}, errors.New("unknown result")
	})
	if e == nil || count != 1 || string(jsonBytes(job.Input)) != before || string(jsonBytes(getProject(t, s, p.ID))) != before {
		t.Fatal("failed retry replaced original", e)
	}
	p.BoardsLocked = true
	if _, e = s.quoteFor(p, defaultSettings(), "board_retry", ShotRequest{ShotID: p.Shots[0].ID, Feedback: "近景"}); e == nil {
		t.Fatal("locked storyboard changed")
	}
}
