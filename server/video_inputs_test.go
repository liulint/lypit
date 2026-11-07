package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestVideoInputModesAndLegacyPayload(t *testing.T) {
	s := testStudio(t)
	p := approveFirstVideoShot(t, s, readyRetryProject(t, s))
	tail := p.Shots[1]
	request(t, s, "PUT", "/api/projects/"+p.ID+"/shots/"+tail.ID, map[string]any{"revision": p.Revision, "selected": 0, "approved": true, "narration": tail.Narration}, 200)
	p = getProject(t, s, p.ID)
	for _, mode := range []string{"first_frame", "first_last", "reference"} {
		end := ""
		if mode == "first_last" {
			end = p.Shots[1].ID
		}
		input, e := s.videoInput(p, p.Shots[0], defaultSettings(), mode, end)
		if e != nil {
			t.Fatal(e)
		}
		task := Task{ProjectID: p.ID, Input: p, VideoInputs: []VideoInput{input}}
		body, e := s.plannedVideoRequest(context.Background(), task, p.Shots[0], "doubao-seedance-2-5-test")
		if e != nil {
			t.Fatal(e)
		}
		items := body["content"].([]map[string]any)
		if mode == "first_frame" && (len(items) != 2 || items[1]["role"] != "first_frame") {
			t.Fatal("not a strict single first frame", items)
		}
		if mode == "first_last" && (len(items) != 3 || items[1]["role"] != "first_frame" || items[2]["role"] != "last_frame") {
			t.Fatal("mixed first-last and references", items)
		}
		if mode == "reference" {
			if len(items) < 3 {
				t.Fatal("fixed asset references missing")
			}
			for _, i := range items[1:] {
				if i["role"] != "reference_image" {
					t.Fatal("mixed incompatible roles")
				}
			}
			if !strings.Contains(items[0]["text"].(string), "本镜分镜图") {
				t.Fatal("unlabeled opening reference")
			}
		}
		if mode != "reference" && body["ratio"] != "adaptive" {
			t.Fatal("2.5 ratio constraint ignored")
		}
	}
	legacy, e := s.plannedVideoRequest(context.Background(), Task{Input: p}, p.Shots[0], "old-model")
	if e != nil || legacy["content"].([]map[string]any)[1]["role"] != "reference_image" {
		t.Fatal("old task semantics changed", e)
	}
	if _, e = s.videoInput(p, p.Shots[0], defaultSettings(), "continue", ""); e == nil {
		t.Fatal("first shot can extend an absent previous clip")
	}
	if _, e = s.videoInput(p, p.Shots[0], defaultSettings(), "first_last", "missing"); e == nil {
		t.Fatal("unconfirmed end frame accepted")
	}
	cfg := defaultSettings()
	cfg.Mode = "remote"
	cfg.Video.Model = "doubao-seedance-1-5-pro-test"
	if _, e = s.videoInput(p, p.Shots[0], cfg, "reference", ""); e == nil {
		t.Fatal("known unsupported reference model accepted")
	}
}

func TestVideoQuoteFreezesModeAndReferences(t *testing.T) {
	s := testStudio(t)
	p := approveFirstVideoShot(t, s, readyRetryProject(t, s))
	var q Quote
	json.Unmarshal(request(t, s, "POST", "/api/projects/"+p.ID+"/quotes", map[string]any{"stage": "shot_video", "revision": p.Revision, "shot_id": p.Shots[0].ID}, 200), &q)
	if len(q.VideoInputs) != 1 || q.VideoInputs[0].Mode != "first_frame" || q.VideoInputs[0].Images[0].Role != "first_frame" {
		t.Fatal("new quote did not default to strict first frame")
	}
	// External file changes must invalidate the quote, even without a project revision change.
	input := q.VideoInputs[0].Images[0].Image
	if e := os.WriteFile(s.media(input.File), []byte("changed"), 0600); e != nil {
		t.Fatal(e)
	}
	request(t, s, "POST", "/api/quotes/"+q.ID+"/confirm", map[string]any{"video_mode": "reference"}, 400)
}

func TestVideoContinuationIntegration(t *testing.T) {
	if os.Getenv("LYPIT_INTEGRATION") != "1" {
		t.Skip("set LYPIT_INTEGRATION=1")
	}
	s := testStudio(t)
	p := approveFirstVideoShot(t, s, readyRetryProject(t, s))
	p = runSingleVideo(t, s, p, "")
	tail := p.Shots[1]
	request(t, s, "PUT", "/api/projects/"+p.ID+"/shots/"+tail.ID, map[string]any{"revision": p.Revision, "selected": 0, "approved": true, "narration": tail.Narration}, 200)
	p = getProject(t, s, p.ID)
	var quote Quote
	json.Unmarshal(request(t, s, "POST", "/api/projects/"+p.ID+"/quotes", map[string]any{"stage": "shot_video", "revision": p.Revision, "shot_id": tail.ID, "video_mode": "continue"}, 200), &quote)
	if quote.VideoInputs[0].SourceVideo != p.Shots[0].Video || quote.VideoInputs[0].SourceHash == "" {
		t.Fatal("upstream video was not frozen")
	}
	var task Task
	json.Unmarshal(request(t, s, "POST", "/api/quotes/"+quote.ID+"/confirm", map[string]any{"video_mode": "reference"}, 200), &task)
	waitTask(t, s, task.ID)
	p = getProject(t, s, p.ID)
	if p.Shots[1].VideoMode != "continue" || !videoDependencyCurrent(p, 1) {
		t.Fatal("continuation provenance missing")
	}
	p = runSingleVideo(t, s, p, "重新调整动作")
	if videoDependencyCurrent(p, 1) {
		t.Fatal("upstream replacement did not invalidate continuation")
	}
	p = lock(t, s, p, "boards")
	if _, e := s.quoteFor(p, defaultSettings(), "render"); e == nil {
		t.Fatal("stale continuation allowed into final film")
	}
}

func TestVideoRecoveryDoesNotRebuildUnavailableInputs(t *testing.T) {
	s := testStudio(t)
	job := cliTask(t, s)
	job.Config = defaultSettings()
	job.Stage = "shot_video"
	job.VideoInputs = []VideoInput{{ShotID: "s", Mode: "continue", SourceVideo: "missing.mp4", SourceTaskID: "missing"}}
	_, e := s.store.db.Exec("INSERT INTO attempts(id,task_id,status,response) VALUES(?,?,'received',?)", job.ID+":video-s", job.ID, []byte(`{"id":"original-remote-task"}`))
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, e = s.remoteVideo(ctx, &job, Shot{ID: "s"})
	if e != context.Canceled {
		t.Fatal("receipt recovery tried to rebuild missing source inputs", e)
	}
	var id string
	s.store.db.QueryRow("SELECT remote_id FROM attempts WHERE id=?", job.ID+":video-s").Scan(&id)
	if id != "original-remote-task" {
		t.Fatal("original supplier task was not retained")
	}
}

func TestChangedEndFrameInvalidatesDependentVideo(t *testing.T) {
	p := Project{Shots: []Shot{{ID: "a", VideoMode: "first_last", VideoEndShotID: "b", VideoEndImageHash: "original"}, {ID: "b", Approved: true, Selected: 0, Candidates: []Image{{Hash: "original"}}}}}
	if !videoDependencyCurrent(p, 0) {
		t.Fatal("unchanged end frame rejected")
	}
	p.Shots[1].Candidates[0].Hash = "new"
	if videoDependencyCurrent(p, 0) {
		t.Fatal("changed end frame remained valid")
	}
}
