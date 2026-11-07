package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func usageResult(t *testing.T, body string) videoTaskResult {
	t.Helper()
	var result videoTaskResult
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestVideoUsageRatesAndUnknowns(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"normal", "4.6000"}, {"continue", "2.8000"}, {"edit_video", "2.8000"}, {"1080p", "5.1000"},
		{"zero", "0.0000"}, {"proxy", ""}, {"missing", ""}, {"total-only", ""},
		{"negative", ""}, {"unknown-model", ""}, {"unknown-resolution", ""}, {"flex", ""}, {"draft", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			job := Task{Config: estimateSettings("doubao-seedance-2-0-260128")}
			result := usageResult(t, `{"model":"doubao-seedance-2-0-260128","resolution":"480p","usage":{"completion_tokens":100000,"total_tokens":100000}}`)
			switch tc.name {
			case "continue", "edit_video":
				job.VideoInputs = []VideoInput{{ShotID: "s", Mode: tc.name}}
			case "1080p":
				result.Resolution = "1080p"
			case "zero":
				*result.Usage.CompletionTokens = 0
			case "proxy":
				job.Config.Vendors[0].Base = "https://proxy.example/v3"
			case "missing":
				result.Usage = nil
			case "total-only":
				result.Usage.CompletionTokens = nil
			case "negative":
				*result.Usage.CompletionTokens = -1
			case "unknown-model":
				result.Model = "unknown"
			case "unknown-resolution":
				result.Resolution = "8K"
			case "flex":
				result.ServiceTier = "flex"
			case "draft":
				result.Draft = true
			}
			u := videoUsage(job, Shot{ID: "s"}, Endpoint{VendorID: "ark"}, "remote", result)
			if tc.want == "" {
				if u.Cost != nil || u.Reason == "" {
					t.Fatalf("unverified cost: %+v", u)
				}
			} else if u.Cost == nil || u.Cost.Amount != tc.want || u.Cost.Checked == "" || u.Cost.Source == "" || u.Reason != "" {
				t.Fatalf("incorrect cost: %+v", u)
			}
			if tc.name == "proxy" && (u.CompletionTokens == nil || *u.CompletionTokens != 100000) {
				t.Fatal("unknown price lost known usage")
			}
		})
	}
}

func TestVideoUsagePersistsBeforeDownloadAndRecoversWithoutPOST(t *testing.T) {
	s := testStudio(t)
	job := cliTask(t, s)
	var posts, gets atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			posts.Add(1)
			http.Error(w, "unexpected POST", 500)
			return
		}
		gets.Add(1)
		fmt.Fprint(w, `{"status":"succeeded","model":"provider-model","usage":{"completion_tokens":12345,"total_tokens":12345},"content":{"video_url":"http://invalid.local/clip.mp4"}}`)
	}))
	defer provider.Close()
	t.Setenv("VIDEO_USAGE_TEST_KEY", "private-test-key")
	job.Config = Settings{Video: Endpoint{Base: provider.URL, Model: "model", KeyEnv: "VIDEO_USAGE_TEST_KEY"}}
	job.Stage, job.Mode = "shot_video", "remote"
	if _, err := s.store.db.Exec("UPDATE tasks SET config=? WHERE id=?", jsonBytes(job.Config), job.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.db.Exec("INSERT INTO attempts(id,task_id,status,response) VALUES(?,?,'received',?)", job.ID+":video-s", job.ID, []byte(`{"id":"original"}`)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := s.remoteVideo(context.Background(), &job, Shot{ID: "s", Title: "第一镜"}); err == nil {
			t.Fatal("invalid download accepted")
		}
		saved, err := s.store.task(job.ID)
		if err != nil || len(saved.VideoUsage) != 1 || *saved.VideoUsage[0].CompletionTokens != 12345 {
			t.Fatal("usage was lost or duplicated", saved.VideoUsage, err)
		}
		job = saved
	}
	if posts.Load() != 0 || gets.Load() != 2 {
		t.Fatal("recovery resubmitted or skipped query", posts.Load(), gets.Load())
	}
	public := string(request(t, s, "GET", "/api/tasks", nil, 200))
	if !strings.Contains(public, `"completion_tokens":12345`) || strings.Contains(public, "private-test-key") || strings.Contains(public, "invalid.local") {
		t.Fatal("unsafe or missing public usage", public)
	}
}

func TestVideoUsageKeepsRateSnapshotAndEachShot(t *testing.T) {
	s := testStudio(t)
	job := cliTask(t, s)
	job.Config = estimateSettings("doubao-seedance-2-0-260128")
	ep := Endpoint{VendorID: "ark", Model: job.Config.Video.Model}
	result := usageResult(t, `{"usage":{"completion_tokens":100000}}`)
	if err := s.recordVideoUsage(&job, Shot{ID: "a"}, ep, "one", result); err != nil {
		t.Fatal(err)
	}
	job.Config.Vendors[0].Base = "https://proxy.example"
	if err := s.recordVideoUsage(&job, Shot{ID: "a"}, ep, "one", result); err != nil {
		t.Fatal(err)
	}
	if err := s.recordVideoUsage(&job, Shot{ID: "b"}, ep, "two", result); err != nil {
		t.Fatal(err)
	}
	if len(job.VideoUsage) != 2 || job.VideoUsage[0].Cost.Amount != "4.6000" || job.VideoUsage[1].Cost != nil {
		t.Fatal("snapshot or per-shot isolation changed", job.VideoUsage)
	}
}
