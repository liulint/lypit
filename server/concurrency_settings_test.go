package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestConcurrencySettingsDefaultsValidationAndSections(t *testing.T) {
	s := testStudio(t)
	// Simulate a saved configuration predating the new fields.
	cfg, err := s.store.settings()
	if err != nil {
		t.Fatal(err)
	}
	var legacy map[string]any
	json.Unmarshal(jsonBytes(cfg), &legacy)
	delete(legacy, "image_concurrency")
	delete(legacy, "video_concurrency")
	s.store.db.Exec("UPDATE settings SET body=? WHERE id=1", jsonBytes(legacy))
	cfg, err = s.store.settings()
	if err != nil || cfg.ImageConcurrency != 2 || cfg.VideoConcurrency != 2 {
		t.Fatal(cfg, err)
	}
	update := func(section string, image, video int, status int) Settings {
		t.Helper()
		var body map[string]any
		json.Unmarshal(jsonBytes(cfg), &body)
		body["section"], body["image_concurrency"], body["video_concurrency"] = section, image, video
		var result Settings
		json.Unmarshal(request(t, s, "PUT", "/api/settings", body, status), &result)
		return result
	}
	for _, n := range []int{-1, 0, 9} {
		update("services", n, 2, 400)
		update("services", 2, n, 400)
	}
	cfg = update("services", 1, 3, 200)
	if cfg.ImageConcurrency != 1 || cfg.VideoConcurrency != 3 {
		t.Fatal(cfg)
	}
	cfg = update("vendors", 8, 8, 200)
	if cfg.ImageConcurrency != 1 || cfg.VideoConcurrency != 3 {
		t.Fatal("vendor page changed limits", cfg)
	}
	stored, _ := s.store.settings()
	if stored.ImageConcurrency != 1 || stored.VideoConcurrency != 3 {
		t.Fatal(stored)
	}
}

func TestConfiguredVideoWorkersUsageAndFailureDrain(t *testing.T) {
	for _, limit := range []int{1, 3} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			s := testStudio(t)
			job := boardTask(t, s)
			job.Stage = "videos"
			job.Config.VideoConcurrency = limit
			job.Input.Shots = []Shot{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var started atomic.Int32
			gate := make(chan struct{})
			err := s.fillVideos(ctx, &job, func(ctx context.Context, worker *Task, q Shot) (string, error) {
				if n := started.Add(1); n == int32(limit) {
					close(gate)
				}
				select {
				case <-gate:
				case <-ctx.Done():
					return "", ctx.Err()
				}
				if q.ID == "a" {
					return "", errors.New("first failed")
				}
				// Give the first failure time to stop admission of the fourth job.
				time.Sleep(10 * time.Millisecond)
				if err := s.recordVideoUsage(worker, q, Endpoint{}, "remote-"+q.ID, videoTaskResult{}); err != nil {
					return "", err
				}
				return q.ID + ".mp4", nil
			})
			if err == nil || started.Load() != int32(limit) {
				t.Fatal("limit/failure barrier", started.Load(), err)
			}
			for i := 1; i < limit; i++ {
				if job.Input.Shots[i].Video == "" {
					t.Fatal("submitted sibling lost")
				}
			}
			if len(job.VideoUsage) != limit-1 {
				t.Fatal("usage lost", job.VideoUsage)
			}
			stored, err := s.store.task(job.ID)
			if err != nil || len(stored.VideoUsage) != limit-1 {
				t.Fatal("usage not durable", err)
			}
		})
	}
}

func TestConfiguredImageWorkerLimit(t *testing.T) {
	for _, limit := range []int{1, 3} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			s := testStudio(t)
			job := boardTask(t, s)
			job.Config.ImageConcurrency = limit
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var active, peak atomic.Int32
			gate := make(chan struct{})
			err := s.fillBoardImages(ctx, &job, func(ctx context.Context, _ *Task, key, _ string, _ []Image, _ bool) (Image, error) {
				n := active.Add(1)
				defer active.Add(-1)
				for old := peak.Load(); n > old; old = peak.Load() {
					if peak.CompareAndSwap(old, n) {
						break
					}
				}
				if n == int32(limit) && peak.Load() == int32(limit) {
					select {
					case <-gate:
					default:
						close(gate)
					}
				}
				select {
				case <-gate:
				case <-ctx.Done():
					return Image{}, ctx.Err()
				}
				return Image{File: key + ".png", Hash: key}, nil
			})
			if err != nil || peak.Load() != int32(limit) {
				t.Fatal(peak.Load(), err)
			}
		})
	}
}

func TestIndependentVideoAdmissionAndMerge(t *testing.T) {
	for _, trial := range []bool{false, true} {
		t.Run(fmt.Sprint(trial), func(t *testing.T) {
			s := testStudio(t)
			p := readyRetryProject(t, s)
			if trial {
				p = stage(t, s, p, "board_plan")
				for i := range p.PlannedShots {
					p.PlannedShots[i].ReviewStatus = "reviewed"
				}
			}
			input := p
			var err error
			if trial {
				input, err = planTrialProject(p, p.PlannedShots[0].ID)
				if err != nil {
					t.Fatal(err)
				}
			}
			jobs := make([]Task, 2)
			for i := range jobs {
				snapshot, _ := decodeBody[Project](jsonBytes(input))
				jobs[i] = Task{ID: uid(), ProjectID: p.ID, Stage: "shot_video", Status: "running", PlanTrial: trial, ShotID: snapshot.Shots[i].ID, ShotInputHash: digest(jsonBytes(snapshot.Shots[i])), Input: snapshot, Config: defaultSettings()}
				job := jobs[i]
				if s.store.generationBusy(p.ID, "shot_video", job.ShotID, trial) {
					t.Fatal("distinct video blocked")
				}
				if _, err := s.store.db.Exec("INSERT INTO tasks VALUES(?,?,?,?,?,?)", job.ID, p.ID, job.Status, jsonBytes(job), jsonBytes(job.Input), jsonBytes(job.Config)); err != nil {
					t.Fatal(err)
				}
			}
			if !s.store.generationBusy(p.ID, "shot_video", "third", trial) {
				t.Fatal("default video limit ignored")
			}
			cfg, _ := s.store.settings()
			cfg.VideoConcurrency = 3
			s.store.db.Exec("UPDATE settings SET body=? WHERE id=1", jsonBytes(cfg))
			if s.store.generationBusy(p.ID, "shot_video", "third", trial) {
				t.Fatal("configured video limit ignored")
			}
			if !s.store.generationBusy(p.ID, "shot_video", jobs[0].ShotID, trial) {
				t.Fatal("duplicate admitted")
			}
			s.store.db.Exec("INSERT INTO attempts(id,task_id,status) VALUES(?,?,'unknown')", jobs[0].ID+":video", jobs[0].ID)
			if !s.store.generationBusy(p.ID, "shot_video", "third", trial) {
				t.Fatal("unknown guard lost")
			}
			// Simulate reverse completion, preserving the sibling's independent result.
			for _, i := range []int{1, 0} {
				jobs[i].Input.Shots[i].Video = fmt.Sprintf("clip-%d.mp4", i)
				p, err = mergeSingleShotTask(p, jobs[i])
				if err != nil {
					t.Fatal(err)
				}
			}
			shots := p.Shots
			if trial {
				shots = p.PlannedShots
			}
			if shots[0].Video != "clip-0.mp4" || shots[1].Video != "clip-1.mp4" {
				t.Fatal("sibling overwritten")
			}
		})
	}
}

func TestVideoRecoveryBlocksNewRequestsOnUnknownReceipt(t *testing.T) {
	s := testStudio(t)
	job := boardTask(t, s)
	job.Stage = "videos"
	s.store.db.Exec("INSERT INTO attempts(id,task_id,status) VALUES(?,?,'unknown')", job.ID+":video-"+job.Input.Shots[1].ID, job.ID)
	var called []string
	err := s.fillVideos(context.Background(), &job, func(_ context.Context, _ *Task, q Shot) (string, error) {
		called = append(called, q.ID)
		return "", errors.New("unknown")
	})
	if err == nil || len(called) != 1 || called[0] != job.Input.Shots[1].ID {
		t.Fatal("new call before recovery", called, err)
	}
}
