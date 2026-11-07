package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

func approveFirstVideoShot(t *testing.T, s *Studio, p Project) Project {
	t.Helper()
	q := p.Shots[0]
	request(t, s, "PUT", "/api/projects/"+p.ID+"/shots/"+q.ID, map[string]any{"revision": p.Revision, "selected": q.Selected, "approved": true, "narration": q.Narration}, 200)
	return getProject(t, s, p.ID)
}

func TestShotVideoQuoteBeforeOtherShotsAndWithoutVoiceProvider(t *testing.T) {
	s := testStudio(t)
	p := readyRetryProject(t, s)
	if _, e := s.quoteFor(p, defaultSettings(), "shot_video", ShotRequest{ShotID: p.Shots[0].ID}); e == nil {
		t.Fatal("unapproved source accepted")
	}
	p = approveFirstVideoShot(t, s, p)
	cfg := defaultSettings()
	cfg.Mode = "remote"
	cfg.Video.Model = "video-model"
	t.Setenv("ARK_API_KEY", "test-key")
	p.Mode = "remote"
	q, e := s.quoteFor(p, cfg, "shot_video", ShotRequest{ShotID: p.Shots[0].ID})
	if e != nil || q.Duration != p.Shots[0].Duration || len(q.Items) != 1 || q.Items[0].Count != 1 || !q.Unknown {
		t.Fatal("single video quote incorrect", q, e)
	}
	if p.BoardsLocked || p.Shots[1].Approved {
		t.Fatal("test should not require other approvals")
	}
	p.BoardsLocked = true
	p.Shots[0].Video = "old.mp4"
	if _, e = s.quoteFor(p, cfg, "shot_video", ShotRequest{ShotID: p.Shots[0].ID}); e == nil {
		t.Fatal("retry without advice accepted")
	}
	q, e = s.quoteFor(p, cfg, "shot_video", ShotRequest{ShotID: p.Shots[0].ID, Feedback: "动作慢一点"})
	if e != nil || !strings.Contains(q.Items[0].Label, "重新生成") {
		t.Fatal("locked boards cannot retry video", q, e)
	}
}

func TestShotVideoFailurePreservesOldClipAndAdviceAffectsPayload(t *testing.T) {
	s := testStudio(t)
	p := approveFirstVideoShot(t, s, readyRetryProject(t, s))
	p.Shots[0].Video = "old.mp4"
	p.Output = "final.mp4"
	job := Task{ID: uid(), ProjectID: p.ID, Stage: "shot_video", Input: p, ShotID: p.Shots[0].ID, Feedback: "镜头缓慢推进"}
	before := jsonBytes(p)
	n := 0
	e := s.generateShotVideo(context.Background(), &job, func(_ context.Context, task *Task, q Shot) (string, error) {
		n++
		payload := string(jsonBytes(taskVideoRequest(*task, q, "model", []byte("png"))))
		if !strings.Contains(payload, job.Feedback) || !strings.Contains(payload, strings.Trim(string(jsonBytes(q.Description)), "\"")) || !strings.Contains(payload, q.Narration) {
			tst := payload
			t.Error(tst)
		}
		return "", errors.New("unknown submission")
	})
	if e == nil || n != 1 || string(jsonBytes(job.Input)) != string(before) {
		t.Fatal("failed generation replaced old video", e)
	}
}

func TestShotVideoResultOnlyChangesTargetAndResumesWithoutSubmission(t *testing.T) {
	s := testStudio(t)
	p := approveFirstVideoShot(t, s, readyRetryProject(t, s))
	p.Shots[0].Video = "old.mp4"
	p.Output = "old-final.mp4"
	job := Task{ID: uid(), ProjectID: p.ID, Stage: "shot_video", Input: p, ShotID: p.Shots[0].ID, Feedback: "动作自然一点"}
	file := p.ID + "/" + job.ID + "-clip-" + job.ShotID + ".mp4"
	if _, e := s.writeMedia(p.ID, job.ID+"-clip-"+job.ShotID+".mp4", []byte("test video file")); e != nil {
		t.Fatal(e)
	}
	e := s.generateShotVideo(context.Background(), &job, func(context.Context, *Task, Shot) (string, error) { return file, nil })
	if e != nil || job.Input.Shots[0].Video != file || job.Input.Output != "" || !job.Input.Shots[0].Approved || !reflect.DeepEqual(job.Input.Shots[1:], p.Shots[1:]) {
		t.Fatal("incorrect single-shot result", e)
	}
	hits := 0
	if e = s.generateShotVideo(context.Background(), &job, func(context.Context, *Task, Shot) (string, error) { hits++; return "", errors.New("unexpected") }); e != nil || hits != 0 {
		t.Fatal("resumed result was submitted again", e, hits)
	}
}

func runSingleVideo(t *testing.T, s *Studio, p Project, feedback string) Project {
	t.Helper()
	var q Quote
	json.Unmarshal(request(t, s, "POST", "/api/projects/"+p.ID+"/quotes", map[string]any{"stage": "shot_video", "revision": p.Revision, "shot_id": p.Shots[0].ID, "feedback": feedback}, 200), &q)
	var job Task
	json.Unmarshal(request(t, s, "POST", "/api/quotes/"+q.ID+"/confirm", map[string]bool{}, 200), &job)
	waitTask(t, s, job.ID)
	var again Task
	json.Unmarshal(request(t, s, "POST", "/api/quotes/"+q.ID+"/confirm", map[string]bool{}, 200), &again)
	if again.ID != job.ID {
		t.Fatal("duplicate confirmation created a new video")
	}
	return getProject(t, s, p.ID)
}

func TestShotVideoIntegration(t *testing.T) {
	if os.Getenv("LYPIT_INTEGRATION") != "1" {
		t.Skip("set LYPIT_INTEGRATION=1")
	}
	s := testStudio(t)
	p := approveFirstVideoShot(t, s, readyRetryProject(t, s))
	p = runSingleVideo(t, s, p, "")
	first := p.Shots[0].Video
	old, e := os.ReadFile(s.media(first))
	if e != nil {
		t.Fatal(e)
	}
	if p.Shots[1].Video != "" || p.Voice != "" || p.BoardsLocked {
		t.Fatal("single generation expanded scope")
	}
	p = runSingleVideo(t, s, p, "动作慢一点")
	previous, e := os.ReadFile(s.media(first))
	if e != nil || digest(previous) != digest(old) || first == p.Shots[0].Video {
		t.Fatal("retry overwrote old clip", e)
	}
	selected := p.Shots[0].Video
	q := p.Shots[1]
	request(t, s, "PUT", "/api/projects/"+p.ID+"/shots/"+q.ID, map[string]any{"revision": p.Revision, "selected": q.Selected, "approved": true, "narration": q.Narration}, 200)
	p = getProject(t, s, p.ID)
	p = lock(t, s, p, "boards")
	quote, e := s.quoteFor(p, defaultSettings(), "videos")
	if e != nil || len(quote.Items) != 1 || quote.Items[0].Count != 1 {
		t.Fatal("batch did not reuse generated clip", quote, e)
	}
	p = stage(t, s, p, "videos")
	if p.Shots[0].Video != selected {
		t.Fatal("batch replaced prior single clip")
	}
	p = stage(t, s, p, "render")
	if e = s.validateFilm(context.Background(), p.Output, p); e != nil {
		t.Fatal(e)
	}
}
