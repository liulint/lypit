package main

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestShotVideoWhileBoardsRunningIntegration(t *testing.T) {
	if os.Getenv("LYPIT_INTEGRATION") != "1" {
		t.Skip("set LYPIT_INTEGRATION=1")
	}
	s := testStudio(t)
	p := readyRetryProject(t, s)
	last := p.Shots[1].Candidates[0]
	p.Shots[1].Candidates[0] = Image{}
	if _, err := s.store.db.Exec("UPDATE projects SET body=? WHERE id=?", jsonBytes(p), p.ID); err != nil {
		t.Fatal(err)
	}
	input, _ := decodeBody[Project](jsonBytes(p))
	boards := Task{ID: uid(), ProjectID: p.ID, Stage: "boards", Status: "running", Input: input, Config: defaultSettings(), BoardImagesPerShot: 1}
	if _, err := s.store.db.Exec("INSERT INTO tasks VALUES(?,?,?,?,?,?)", boards.ID, p.ID, boards.Status, jsonBytes(boards), jsonBytes(input), jsonBytes(boards.Config)); err != nil {
		t.Fatal(err)
	}
	p = approveFirstVideoShot(t, s, p)
	p = runSingleVideo(t, s, p, "")
	clip := p.Shots[0].Video
	if clip == "" || p.Shots[1].Candidates[0].File != "" {
		t.Fatal("video did not complete before remaining image")
	}
	boards.Input.Shots[1].Candidates[0] = last
	if err := s.publishBoards(boards); err != nil {
		t.Fatal(err)
	}
	if err := s.commitTask(boards); err != nil {
		t.Fatal(err)
	}
	got := getProject(t, s, p.ID)
	if got.Shots[0].Video != clip || !got.Shots[0].Approved {
		t.Fatal("board completion lost video or approval")
	}
}

func TestBoardResumeAfterIndependentReviewPreservesConfirmation(t *testing.T) {
	s := testStudio(t)
	before := readyRetryProject(t, s)
	p := approveFirstVideoShot(t, s, before)
	job := Task{ID: uid(), ProjectID: p.ID, Stage: "boards", Status: "interrupted", Mode: before.Mode, Input: before, Config: defaultSettings(), BoardImagesPerShot: 1}
	if _, err := s.store.db.Exec("INSERT INTO tasks VALUES(?,?,?,?,?,?)", job.ID, p.ID, job.Status, jsonBytes(job), jsonBytes(before), jsonBytes(job.Config)); err != nil {
		t.Fatal(err)
	}
	request(t, s, "POST", "/api/tasks/"+job.ID+"/resume", map[string]any{}, 200)
	waitTask(t, s, job.ID)
	if got := getProject(t, s, p.ID); !got.Shots[0].Approved {
		t.Fatal("resumed snapshot lost confirmation")
	}
}

func TestReviewAndVideoDuringBoardGenerationMergeInEitherOrder(t *testing.T) {
	for _, videoFirst := range []bool{true, false} {
		t.Run(map[bool]string{true: "video-first", false: "boards-first"}[videoFirst], func(t *testing.T) {
			s := testStudio(t)
			p := readyRetryProject(t, s)
			last := p.Shots[1].Candidates[0]
			p.Shots[1].Candidates[0] = Image{}
			if _, err := s.store.db.Exec("UPDATE projects SET body=? WHERE id=?", jsonBytes(p), p.ID); err != nil {
				t.Fatal(err)
			}
			input, _ := decodeBody[Project](jsonBytes(p))
			boards := Task{ID: uid(), ProjectID: p.ID, Stage: "boards", Status: "running", Input: input, Config: defaultSettings(), BoardImagesPerShot: 1}
			if _, err := s.store.db.Exec("INSERT INTO tasks VALUES(?,?,?,?,?,?)", boards.ID, p.ID, boards.Status, jsonBytes(boards), jsonBytes(input), jsonBytes(boards.Config)); err != nil {
				t.Fatal(err)
			}
			p = approveFirstVideoShot(t, s, p)
			var quote Quote
			json.Unmarshal(request(t, s, "POST", "/api/projects/"+p.ID+"/quotes", map[string]any{"stage": "shot_video", "revision": p.Revision, "shot_id": p.Shots[0].ID}, 200), &quote)
			if quote.Duration != p.Shots[0].Duration || len(quote.Items) != 1 {
				t.Fatal("expanded video quote", quote)
			}
			request(t, s, "POST", "/api/projects/"+p.ID+"/boards/lock", map[string]any{"revision": p.Revision}, 400)
			request(t, s, "PUT", "/api/projects/"+p.ID+"/shots/"+p.Shots[1].ID, map[string]any{"revision": p.Revision, "approved": true, "selected": 0, "narration": p.Shots[1].Narration}, 400)
			videoInput, _ := decodeBody[Project](jsonBytes(p))
			video := Task{ID: uid(), ProjectID: p.ID, Stage: "shot_video", ShotID: p.Shots[0].ID, Input: videoInput}
			video.Input.Shots[0].Video = "new-video.mp4"
			video.Input.Shots[0].VideoTaskID = video.ID
			video.Input.Shots[0].VideoMode = "first_frame"
			boards.Input.Shots[1].Candidates[0] = last
			finishVideo := func() {
				t.Helper()
				if err := s.commitTask(video); err != nil {
					t.Fatal(err)
				}
			}
			finishBoards := func() {
				t.Helper()
				if err := s.publishBoards(boards); err != nil {
					t.Fatal(err)
				}
				if err := s.commitTask(boards); err != nil {
					t.Fatal(err)
				}
			}
			if videoFirst {
				finishVideo()
				finishBoards()
			} else {
				finishBoards()
				finishVideo()
			}
			got := getProject(t, s, p.ID)
			if !got.Shots[0].Approved || got.Shots[0].Narration != p.Shots[0].Narration || got.Shots[0].Video != "new-video.mp4" || !reflect.DeepEqual(got.Shots[1].Candidates[0], last) {
				t.Fatal("concurrent results overwritten", got.Shots)
			}
			// Re-publishing the old image snapshot is idempotent and cannot lose a clip.
			if err := s.publishBoards(boards); err != nil {
				t.Fatal(err)
			}
			if after := getProject(t, s, p.ID); after.Revision != got.Revision {
				t.Fatal("duplicate publication changed revision")
			}
		})
	}
}

func TestBoardAdmissionBlocksUnresolvedTasksAndUnknownReceipts(t *testing.T) {
	s := testStudio(t)
	job := boardTask(t, s)
	job.Status = "running"
	if _, err := s.store.db.Exec("UPDATE tasks SET status=?,body=? WHERE id=?", job.Status, jsonBytes(job), job.ID); err != nil {
		t.Fatal(err)
	}
	if s.store.operationBusy(job.ProjectID, true) {
		t.Fatal("healthy boards blocked")
	}
	if _, err := s.store.db.Exec("INSERT INTO attempts(id,task_id,status) VALUES(?,?,'submitting')", job.ID+":image", job.ID); err != nil {
		t.Fatal(err)
	}
	if s.store.operationBusy(job.ProjectID, true) {
		t.Fatal("in-flight image blocked independent video")
	}
	if _, err := s.store.db.Exec("UPDATE attempts SET status='unknown' WHERE task_id=?", job.ID); err != nil {
		t.Fatal(err)
	}
	if !s.store.operationBusy(job.ProjectID, true) {
		t.Fatal("unknown submission admitted paid work")
	}
	for _, status := range []string{"failed", "interrupted", "cancelled"} {
		job.Status = status
		if err := s.store.saveTask(job); err != nil {
			t.Fatal(err)
		}
		if !s.store.operationBusy(job.ProjectID, true) {
			t.Fatal("unresolved task admitted work", status)
		}
	}
}
