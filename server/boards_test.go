package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func boardTask(t *testing.T, s *Studio) Task {
	job := cliTask(t, s)
	job.Stage = "boards"
	job.Mode = "remote"
	job.Input.Assets = []Asset{{ID: "asset", Kind: "character", Master: &Image{File: "ref.png", Hash: "ref", Approved: true}}}
	job.Input.Shots = []Shot{{ID: "first", Title: "第一镜", Description: "描述", References: []string{"asset"}, Candidates: []Image{}}, {ID: "second", Title: "第二镜", Description: "描述", References: []string{"asset"}, Candidates: []Image{}}}
	// The production board planner starts from this same locked project base.
	if _, err := s.store.db.Exec("UPDATE projects SET body=? WHERE id=?", jsonBytes(job.Input), job.ProjectID); err != nil {
		t.Fatal(err)
	}
	return job
}

func TestBoardImagesBoundedConcurrencyPersistsOutOfOrderAndPreviewsSafely(t *testing.T) {
	s := testStudio(t)
	job := boardTask(t, s)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	firstRelease := make(chan struct{})
	firstStarted := make(chan struct{})
	secondSaved := make(chan struct{}, 1)
	var active, peak, runs atomic.Int32
	makeImage := func(ctx context.Context, worker *Task, key, prompt string, refs []Image, sheet bool) (Image, error) {
		n := active.Add(1)
		defer active.Add(-1)
		runs.Add(1)
		for old := peak.Load(); n > old; old = peak.Load() {
			if peak.CompareAndSwap(old, n) {
				break
			}
		}
		if sheet || len(refs) != 1 {
			t.Error("image inputs changed")
		}
		if key == "shot-first-0" {
			close(firstStarted)
			select {
			case <-firstRelease:
			case <-ctx.Done():
				return Image{}, ctx.Err()
			}
		}
		if key == "shot-first-1" {
			<-firstStarted
			secondSaved <- struct{}{}
		}
		return Image{File: key + ".png", Hash: key}, nil
	}
	done := make(chan error, 1)
	go func() { done <- s.fillBoardImages(ctx, &job, makeImage) }()
	select {
	case <-secondSaved:
	case <-ctx.Done():
		t.Fatal("second image never completed concurrently")
	}
	// Wait for the coordinator's durable snapshot, not just the worker result.
	deadline := time.Now().Add(2 * time.Second)
	var saved Task
	for time.Now().Before(deadline) {
		saved, _ = s.store.task(job.ID)
		if len(saved.Input.Shots) > 0 && len(saved.Input.Shots[0].Candidates) == 2 && saved.Input.Shots[0].Candidates[1].File != "" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if len(saved.Input.Shots) == 0 || saved.Input.Shots[0].Candidates[0].File != "" || saved.Input.Shots[0].Candidates[1].File == "" {
		t.Fatal("out-of-order candidate was not durably mapped to B")
	}
	response := request(t, s, "GET", "/api/tasks/"+job.ID+"/preview", nil, 200)
	var preview struct {
		Shots     []Shot `json:"shots"`
		Completed int    `json:"completed"`
	}
	if err := json.Unmarshal(response, &preview); err != nil || len(preview.Shots) != 2 || preview.Completed < 1 {
		t.Fatal("partial preview unavailable", err)
	}
	for _, forbidden := range []string{"settings", "config", "key_env", "credential_id", "vendors"} {
		if strings.Contains(string(response), `"`+forbidden+`"`) {
			t.Fatal("private task fields leaked", forbidden)
		}
	}
	close(firstRelease)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if peak.Load() != 2 || runs.Load() != 4 {
		t.Fatal("concurrency or paid scope changed", peak.Load(), runs.Load())
	}
	if job.Progress != 95 {
		t.Fatal("wrong final image progress", job.Progress)
	}
	for _, q := range job.Input.Shots {
		for i, im := range q.Candidates {
			if im.File != fmt.Sprintf("shot-%s-%d.png", q.ID, i) {
				t.Fatal("candidate ordering changed")
			}
		}
	}
}

func TestBoardImageFailureDrainsSubmittedWorkWithoutLaunchingMore(t *testing.T) {
	s := testStudio(t)
	job := boardTask(t, s)
	var started atomic.Int32
	gate := make(chan struct{})
	var once sync.Once
	makeImage := func(ctx context.Context, _ *Task, key, _ string, _ []Image, _ bool) (Image, error) {
		if started.Add(1) == 2 {
			once.Do(func() { close(gate) })
		}
		<-gate
		if key == "shot-first-0" {
			return Image{}, errors.New("provider unknown result")
		}
		time.Sleep(10 * time.Millisecond)
		return Image{File: key + ".png", Hash: key}, nil
	}
	if err := s.fillBoardImages(context.Background(), &job, makeImage); err == nil {
		t.Fatal("image failure ignored")
	}
	if started.Load() != 2 {
		t.Fatal("more paid work launched after failure", started.Load())
	}
	saved, err := s.store.task(job.ID)
	if err != nil || saved.Input.Shots[0].Candidates[1].File == "" {
		t.Fatal("other submitted result discarded", err)
	}
}

func TestBoardRecoveryChecksUnknownReceiptsBeforeNewSubmissions(t *testing.T) {
	s := testStudio(t)
	job := boardTask(t, s)
	// A later missing slot already has an unresolved request. No earlier fresh slot may start.
	key := "shot-second-0"
	if _, err := s.store.db.Exec("INSERT INTO attempts(id,task_id,status,response) VALUES(?,?,?,?)", job.ID+":shot-first-0", job.ID, boardDeferredState, []byte(boardDeferredReceipt)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.db.Exec("INSERT INTO attempts(id,task_id,status) VALUES(?,?,'submitting')", job.ID+":"+key, job.ID); err != nil {
		t.Fatal(err)
	}
	var called []string
	err := s.fillBoardImages(context.Background(), &job, func(_ context.Context, _ *Task, key, _ string, _ []Image, _ bool) (Image, error) {
		called = append(called, key)
		return Image{}, errors.New("unknown receipt")
	})
	if err == nil || len(called) != 1 || called[0] != key {
		t.Fatal("fresh paid work admitted before resolving old unknown result", called, err)
	}
	var state string
	if err := s.store.db.QueryRow("SELECT status FROM attempts WHERE id=?", job.ID+":shot-first-0").Scan(&state); err != nil || state != boardDeferredState {
		t.Fatal("reservation cleared before unknown result resolved", err)
	}
}

func TestBoardUpgradeReservationIsUnsubmittedAndOnlyConsumedOnce(t *testing.T) {
	s := testStudio(t)
	job := boardTask(t, s)
	key := "shot-first-0"
	if _, err := s.store.db.Exec("INSERT INTO attempts(id,task_id,status,response) VALUES(?,?,?,?)", job.ID+":"+key, job.ID, boardDeferredState, []byte(boardDeferredReceipt)); err != nil {
		t.Fatal(err)
	}
	// The old CLI adapter sees the reserved key and returns BEFORE it can execute.
	_, err := s.cliImage(context.Background(), &job, key, "", nil, false, Endpoint{CLI: "codex", Executable: fakeCLI(t, "fail")})
	if err == nil {
		t.Fatal("legacy CLI ignored reservation")
	}
	var calls atomic.Int32
	err = s.fillBoardImages(context.Background(), &job, func(_ context.Context, worker *Task, key, _ string, _ []Image, _ bool) (Image, error) {
		calls.Add(1)
		_, err := s.store.db.Exec("INSERT INTO attempts(id,task_id,status) VALUES(?,?,'received')", worker.ID+":"+key, worker.ID)
		return Image{File: key + ".png", Hash: key}, err
	})
	if err != nil || calls.Load() != 4 {
		t.Fatal("upgrade replay changed paid scope", calls.Load(), err)
	}
}
