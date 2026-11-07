package main

import (
	"context"
	"encoding/json"
	"testing"
)

func TestIndependentImageTasksMergeAndAdmission(t *testing.T) {
	for _, trial := range []bool{false, true} {
		t.Run(map[bool]string{false: "boards", true: "draft"}[trial], func(t *testing.T) {
			s := testStudio(t)
			p := readyRetryProject(t, s)
			input := p
			if trial {
				p = stage(t, s, p, "board_plan")
				for i := range p.PlannedShots {
					p.PlannedShots[i].ReviewStatus = "reviewed"
				}
				p = saveTrialDraft(t, s, p)
				var err error
				input, err = planTrialProject(p, p.PlannedShots[0].ID)
				if err != nil {
					t.Fatal(err)
				}
			}
			jobs := make([]Task, 2)
			for i := range jobs {
				snapshot, err := decodeBody[Project](jsonBytes(input))
				if err != nil {
					t.Fatal(err)
				}
				jobs[i] = Task{ID: uid(), ProjectID: p.ID, Stage: "board_retry", Status: "running", PlanTrial: trial, ShotID: snapshot.Shots[i].ID, Feedback: "调整构图", ShotInputHash: digest(jsonBytes(snapshot.Shots[i])), Input: snapshot, Config: defaultSettings()}
			}
			save := func(job Task) {
				t.Helper()
				if _, err := s.store.db.Exec("INSERT INTO tasks VALUES(?,?,?,?,?,?)", job.ID, p.ID, job.Status, jsonBytes(job), jsonBytes(job.Input), jsonBytes(job.Config)); err != nil {
					t.Fatal(err)
				}
			}
			save(jobs[0])
			var quote Quote
			json.Unmarshal(request(t, s, "POST", "/api/projects/"+p.ID+"/quotes", map[string]any{"revision": p.Revision, "stage": "board_retry", "shot_id": jobs[1].ShotID, "feedback": "调整构图", "plan_trial": trial}, 200), &quote)
			if quote.ShotID != jobs[1].ShotID {
				t.Fatal("wrong quote target")
			}
			if !s.store.generationBusy(p.ID, "board_retry", jobs[0].ShotID, trial) {
				t.Fatal("duplicate target admitted")
			}
			if !s.store.generationBusy(p.ID, "board_retry", jobs[1].ShotID, !trial) {
				t.Fatal("mixed namespace admitted")
			}
			if _, err := s.store.db.Exec("INSERT INTO attempts(id,task_id,status) VALUES(?,?,'unknown')", jobs[0].ID+":check", jobs[0].ID); err != nil {
				t.Fatal(err)
			}
			if !s.store.generationBusy(p.ID, "board_retry", jobs[1].ShotID, trial) {
				t.Fatal("unknown submission admitted new work")
			}
			if _, err := s.store.db.Exec("UPDATE attempts SET status='received' WHERE id=?", jobs[0].ID+":check"); err != nil {
				t.Fatal(err)
			}
			save(jobs[1])
			if !s.store.generationBusy(p.ID, "board_retry", "third", trial) {
				t.Fatal("third image admitted")
			}
			for i := range jobs {
				job := &jobs[i]
				if err := s.regenerateBoard(context.Background(), job, func(_ context.Context, _ *Task, key, prompt string, refs []Image, sheet bool) (Image, error) {
					return Image{File: key + ".png", Hash: key}, nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			// Reverse completion order must preserve both results and immutable inputs.
			for _, i := range []int{1, 0} {
				if err := s.commitTask(jobs[i]); err != nil {
					t.Fatal(err)
				}
			}
			if trial {
				check, _ := decodeBody[Project](jsonBytes(p))
				check.Output = "existing-film.mp4"
				merged, err := mergeSingleShotTask(check, jobs[0])
				if err != nil || merged.Output != check.Output {
					t.Fatal("trial changed active film", err)
				}
			}
			got := getProject(t, s, p.ID)
			shots := got.Shots
			if trial {
				shots = got.PlannedShots
			}
			for i, shot := range shots[:2] {
				if shot.Candidates[shot.Selected].File != jobs[i].Input.Shots[i].Candidates[jobs[i].Input.Shots[i].Selected].File {
					t.Fatal("sibling result lost")
				}
			}
			changed, _ := decodeBody[Project](jsonBytes(p))
			if trial {
				changed.PlannedShots[0].Narration += "变更"
			} else {
				changed.Shots[0].Narration += "变更"
			}
			if _, err := mergeSingleShotTask(changed, jobs[0]); err != errConflict {
				t.Fatal("changed target accepted", err)
			}
			if !trial {
				failed := jobs[0]
				failed.ID = uid()
				failed.Status = "failed"
				save(failed)
				if !s.store.generationBusy(p.ID, "board_retry", "third", trial) {
					t.Fatal("failure barrier lost")
				}
			}
		})
	}
}
