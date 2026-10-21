package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func options(t *testing.T, args ...string) Options {
	t.Helper()
	o, e := parseOptions(append([]string{"-prompt", "测试提示词"}, args...), io.Discard)
	if e != nil {
		t.Fatal(e)
	}
	return o
}
func quoteFor(t *testing.T, o Options, e *Engine) (Request, Quote) {
	t.Helper()
	r, m, w, v, err := prepare(context.Background(), o, e.Media)
	if err != nil {
		t.Fatal(err)
	}
	return r, makeQuote(o, m, w, v)
}
func readRecord(t *testing.T, path string) Record {
	t.Helper()
	r, e := loadRecord(path)
	if e != nil {
		t.Fatal(e)
	}
	return r
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func mockMedia(t *testing.T, handler func(*http.Request) (int, []byte)) *http.Client {
	t.Helper()
	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "" {
			t.Error("Ark key leaked to media")
		}
		status, body := handler(r)
		return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)), Header: make(http.Header)}, nil
	})}
}
func fixture(t *testing.T, path string) {
	t.Helper()
	f, e := os.Create(path)
	if e != nil {
		t.Fatal(e)
	}
	e = png.Encode(f, image.NewRGBA(image.Rect(0, 0, 640, 480)))
	f.Close()
	if e != nil {
		t.Fatal(e)
	}
}

func testServer(t *testing.T, h http.Handler) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(h)
	address := s.Listener.Addr().String()
	t.Cleanup(func() {
		s.Close()
		c, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if err == nil {
			c.Close()
			t.Errorf("test server port still listening: %s", address)
		}
	})
	return s
}

func TestDefaultsPricingAndApproval(t *testing.T) {
	e := newEngine("")
	o := options(t)
	_, q := quoteFor(t, o, e)
	if !o.Audio || o.Model != models["mini"] || o.Resolution != "480p" || o.Duration != 5 || o.Ratio != "16:9" || o.Count != 1 {
		t.Fatalf("wrong defaults: %+v", o)
	}
	if *q.Unit != 23 || *q.FormulaTokens != 50220 || *q.MinYuan != 1.15506 || q.Width != 864 || q.Height != 496 {
		t.Fatalf("wrong quote: %+v", q)
	}
	if q.ConfirmationID == "" || !q.CanSubmit {
		t.Fatal(q)
	}
	for _, args := range [][]string{{"-audio=false"}, {"-duration", "6"}, {"-ratio", "9:16"}, {"-count", "2"}, {"-model", "fast"}, {"-resolution", "720p"}} {
		_, other := quoteFor(t, options(t, args...), e)
		if other.ConfirmationID == q.ConfirmationID {
			t.Fatal("changed input preserved confirmation", args)
		}
	}
	for alias, unit := range map[string]float64{"mini": 23, "fast": 37, "standard": 46} {
		for _, res := range []string{"480p", "720p"} {
			_, v := quoteFor(t, options(t, "-model", alias, "-resolution", res, "-audio=false"), e)
			if *v.Unit != unit {
				t.Fatal(v)
			}
		}
	}
	_, unknown := quoteFor(t, options(t, "-model", "ep-custom"), e)
	if unknown.Unit != nil || unknown.CanSubmit {
		t.Fatal("unknown model borrowed mini pricing")
	}
	_, mapped := quoteFor(t, options(t, "-model", "ep-custom", "-price-model", "fast"), e)
	if *mapped.Unit != 37 || !mapped.CanSubmit {
		t.Fatal(mapped)
	}
}
func TestInvalidParameters(t *testing.T) {
	for _, args := range [][]string{{"-duration", "-1"}, {"-duration", "3"}, {"-duration", "16"}, {"-ratio", "adaptive"}, {"-resolution", "1080p"}, {"-count", "0"}, {"-model", "mini", "-price-model", "fast"}, {"-price-model", "invented"}, {"-audio", "false"}, {"-resume", "run.json", "-model", "fast"}} {
		if _, e := parseOptions(append([]string{"-prompt", "x"}, args...), io.Discard); e == nil {
			t.Fatalf("accepted invalid args %v", args)
		}
	}
	if _, e := parseOptions([]string{"-resume", "run.json"}, io.Discard); e != nil {
		t.Fatal(e)
	}
	if _, e := parseOptions([]string{"-dry-run"}, io.Discard); e != nil {
		t.Fatal(e)
	}
	o := options(t)
	o.Images = make(stringsFlag, 10)
	args := []string{"-prompt", "x"}
	for range o.Images {
		args = append(args, "-image", "x.png")
	}
	if _, e := parseOptions(args, io.Discard); e == nil {
		t.Fatal("accepted 10 images")
	}
}
func TestImageReferenceAndMutation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "图.png")
	fixture(t, path)
	e := newEngine("")
	o := options(t, "-image", path)
	req, q := quoteFor(t, o, e)
	if req.Content[1].Role != "reference_image" || !strings.HasPrefix(req.Content[1].ImageURL.URL, "data:image/png;base64,") {
		t.Fatal(req.Content[1])
	}
	if *q.Unit != 23 || q.Media[0].SHA256 == "" {
		t.Fatal(q)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.Write([]byte("changed"))
	f.Close()
	_, q2 := quoteFor(t, o, e)
	if q2.ConfirmationID == q.ConfirmationID {
		t.Fatal("image mutation kept approval")
	}
	bad := filepath.Join(dir, "bad.png")
	_ = os.WriteFile(bad, []byte("not an image"), 0600)
	if _, _, _, _, err := prepare(context.Background(), options(t, "-image", bad), e.Media); err == nil {
		t.Fatal("accepted invalid image")
	}
}
func TestNoPaidRequestWithoutMatchingConfirmation(t *testing.T) {
	var posts atomic.Int32
	s := testServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { posts.Add(1) }))
	defer s.Close()
	e := newEngine("mock-secret")
	e.Base = s.URL
	o := options(t, "-out", filepath.Join(t.TempDir(), "job"))
	req, q := quoteFor(t, o, e)
	for _, confirm := range []string{"", "wrong"} {
		o.Confirm = confirm
		if _, _, err := e.submit(context.Background(), o, q, req); err == nil {
			t.Fatal("accepted unconfirmed request")
		}
	}
	if posts.Load() != 0 {
		t.Fatal("unconfirmed POST")
	}
	var out bytes.Buffer
	if err := execute(context.Background(), []string{"-dry-run", "-json"}, &out, io.Discard, e); err != nil {
		t.Fatal(err)
	}
	if posts.Load() != 0 {
		t.Fatal("dry run POST")
	}
}
func TestSubmitResumeDownloadFailureAndNoOverwrite(t *testing.T) {
	var posts, gets atomic.Int32
	s := testServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer mock-secret" {
			t.Error("missing API authorization")
		}
		if r.Method == "POST" {
			posts.Add(1)
			var req Request
			if json.NewDecoder(r.Body).Decode(&req) != nil {
				t.Error("bad body")
			}
			if !req.Audio || req.Duration != 5 || req.Ratio != "16:9" || req.Resolution != "480p" || req.Model != models["mini"] {
				t.Errorf("wrong body %+v", req)
			}
			fmt.Fprint(w, `{"id":"cgt-test"}`)
			return
		}
		gets.Add(1)
		fmt.Fprintf(w, `{"id":"cgt-test","status":"succeeded","model":%q,"resolution":"480p","duration":5,"ratio":"16:9","generate_audio":true,"usage":{"completion_tokens":60000,"total_tokens":70000},"content":{"video_url":"https://example.com/result.mp4"}}`, models["mini"])
	}))
	defer s.Close()
	e := newEngine("mock-secret")
	e.Base = s.URL
	o := options(t, "-out", t.TempDir())
	req, q := quoteFor(t, o, e)
	o.Confirm = q.ConfirmationID
	path := filepath.Join(o.Out, "run.json")
	e.Media = mockMedia(t, func(r *http.Request) (int, []byte) {
		saved := readRecord(t, path)
		if saved.Tasks[0].Usage == nil || saved.Tasks[0].EstimatedFee == nil {
			t.Error("download before usage persisted")
		}
		return 503, []byte("unavailable")
	})
	record, _, err := e.submit(context.Background(), o, q, req)
	if err == nil || record.Tasks[0].DownloadError == "" {
		t.Fatal("missing download failure")
	}
	saved := readRecord(t, path)
	if *saved.Tasks[0].EstimatedFee != 1.38 || *saved.Tasks[0].Usage.Total != 70000 {
		t.Fatal(saved.Tasks[0])
	}
	var downloads int
	e.Media = mockMedia(t, func(r *http.Request) (int, []byte) { downloads++; return 200, []byte("mock-video") })
	o.Resume = path
	if _, _, err = e.resume(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if _, _, err = e.resume(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if posts.Load() != 1 || downloads != 1 || gets.Load() != 2 {
		t.Fatalf("posts %d downloads %d gets %d", posts.Load(), downloads, gets.Load())
	}
	if _, _, err = e.submit(context.Background(), o, q, req); err == nil {
		t.Fatal("overwrote existing record")
	}
	saved = readRecord(t, path)
	if *saved.Tasks[0].EstimatedFee != 1.38 {
		t.Fatal("double accumulated fee")
	}
	// Existing file without provenance must never be silently adopted or overwritten.
	saved.Tasks[0].File = ""
	if err = saveRecord(path, &saved, false); err != nil {
		t.Fatal(err)
	}
	if _, _, err = e.resume(context.Background(), o); err == nil {
		t.Fatal("accepted untracked existing video")
	}
	if b, _ := os.ReadFile(filepath.Join(o.Out, "cgt-test.mp4")); string(b) != "mock-video" {
		t.Fatal("overwrote video")
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0600 {
		t.Fatal("record permissions")
	}
}
func TestPOSTNeverRetriedAndPartialResume(t *testing.T) {
	var posts atomic.Int32
	s := testServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			if posts.Add(1) == 1 {
				fmt.Fprint(w, `{"id":"cgt-first"}`)
			} else {
				w.WriteHeader(500)
				fmt.Fprint(w, "mock-secret")
			}
			return
		}
		fmt.Fprint(w, `{"id":"cgt-first","status":"succeeded","content":{"video_url":"https://example.com/first.mp4"}}`)
	}))
	defer s.Close()
	e := newEngine("mock-secret")
	e.Base = s.URL
	e.Media = mockMedia(t, func(*http.Request) (int, []byte) { return 200, []byte("video") })
	o := options(t, "-count", "3", "-out", t.TempDir())
	req, q := quoteFor(t, o, e)
	o.Confirm = q.ConfirmationID
	r, path, err := e.submit(context.Background(), o, q, req)
	if err == nil || strings.Contains(err.Error(), "mock-secret") || posts.Load() != 2 {
		t.Fatal("POST retried or secret exposed", err)
	}
	if r.Tasks[0].ID != "cgt-first" || r.Tasks[1].Status != "submission_unknown" || r.Tasks[2].Status != "not_submitted" {
		t.Fatal(r.Tasks)
	}
	o.Resume = path
	if _, _, err = e.resume(context.Background(), o); err == nil {
		t.Fatal("did not report unsubmitted tasks")
	}
	if posts.Load() != 2 {
		t.Fatal("resume created paid tasks")
	}
	saved := readRecord(t, path)
	if saved.Tasks[0].File == "" {
		t.Fatal("known task was not downloaded")
	}
}
func TestAPIRedirectDoesNotLeakOrReplay(t *testing.T) {
	var destCalls atomic.Int32
	dest := testServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { destCalls.Add(1) }))
	defer dest.Close()
	source := testServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, dest.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	e := newEngine("mock-secret")
	e.Base = source.URL
	var response any
	if e.call(context.Background(), "POST", "", []byte(`{}`), &response) == nil {
		t.Fatal("accepted redirect")
	}
	if destCalls.Load() != 0 {
		t.Fatal("redirect leaked credentials")
	}
}
func TestLegacyAndMissingUsage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.json")
	if err := os.WriteFile(path, []byte(`{"task_id":"cgt-old","status":"submitted"}`), 0600); err != nil {
		t.Fatal(err)
	}
	r := readRecord(t, path)
	if len(r.Tasks) != 1 || r.Tasks[0].ID != "cgt-old" {
		t.Fatal(r)
	}
	s := testServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":"cgt-old","status":"succeeded","content":{"video_url":"https://example.com/old.mp4"}}`)
	}))
	defer s.Close()
	e := newEngine("mock-secret")
	e.Base = s.URL
	e.Media = mockMedia(t, func(*http.Request) (int, []byte) { return 200, []byte("old") })
	o := options(t)
	o.Resume = path
	updated, _, err := e.resume(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	printRecord(&b, updated, path)
	if !strings.Contains(b.String(), "未返回") || updated.Tasks[0].EstimatedFee != nil {
		t.Fatal(b.String())
	}
	unit := 23.0
	tks := int64(999999)
	task := Task{Status: "succeeded", Usage: &Usage{Total: &tks}}
	calculateFee(&task, Quote{Unit: &unit})
	if task.EstimatedFee != nil {
		t.Fatal("used total_tokens as completion")
	}
}
func TestLockTimeoutAndPublicURL(t *testing.T) {
	dir := t.TempDir()
	unlock, e := lockDirectory(dir)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = lockDirectory(dir); e == nil {
		t.Fatal("parallel lock allowed")
	}
	unlock()
	for _, raw := range []string{"file:///tmp/video.mp4", "https://user:secret@example.com/a", "http://127.0.0.1/a", "http://10.1.1.1/a", "http://[::1]/a", "http://localhost/a"} {
		if publicURL(raw) == nil {
			t.Fatal("accepted private URL", raw)
		}
	}
	for _, ip := range []string{"100.64.0.1", "169.254.169.254", "fc00::1", "192.168.0.1"} {
		if isPublic(net.ParseIP(ip)) {
			t.Fatal("accepted private IP", ip)
		}
	}
	s := testServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"id":"cgt-wait","status":"running"}`) }))
	defer s.Close()
	eng := newEngine("mock")
	eng.Base = s.URL
	o := options(t)
	o.Wait = time.Millisecond
	o.Poll = time.Second
	r := Record{Tasks: []Task{{ID: "cgt-wait"}}}
	path := filepath.Join(t.TempDir(), "run.json")
	if e = saveRecord(path, &r, true); e != nil {
		t.Fatal(e)
	}
	if e = eng.finish(context.Background(), o, &r, path); e == nil {
		t.Fatal("no wait timeout")
	}
}

func TestVideoValidationQuotesAndTransmission(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not installed")
	}
	path := filepath.Join(t.TempDir(), "reference.mp4")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-f", "lavfi", "-i", "color=c=blue:s=864x496:r=24", "-t", "2", "-c:v", "libx264", "-pix_fmt", "yuv420p", path).CombinedOutput(); err != nil {
		t.Fatalf("fixture video: %v %s", err, out)
	}
	video, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	e := newEngine("")
	e.Media = mockMedia(t, func(*http.Request) (int, []byte) { return 200, video })
	o := options(t, "-video-url", "https://example.com/reference.mp4")
	req, q := quoteFor(t, o, e)
	if !q.CanSubmit || q.Media[0].Duration != 2 || *q.Unit != 14 || *q.MinYuan != 1.27 || *q.MaxYuan != 2.81 {
		t.Fatalf("video quote %+v", q)
	}
	if req.Content[1].Role != "reference_video" || req.Content[1].VideoURL.URL != o.Videos[0] {
		t.Fatal(req)
	}
	o.Duration = 6
	_, q = quoteFor(t, o, e)
	if q.MaxYuan != nil || q.MinYuan == nil || len(q.Warnings) == 0 {
		t.Fatal("invented precise minimum token price")
	}
	for _, alias := range []string{"mini", "fast", "standard"} {
		o.Model = models[alias]
		o.PriceModel = alias
		_, q = quoteFor(t, o, e)
		if *q.Unit != rates[alias][1] {
			t.Fatal(q)
		}
	}
	e.Media = mockMedia(t, func(*http.Request) (int, []byte) { return 200, []byte("invalid video") })
	_, q = quoteFor(t, o, e)
	if q.CanSubmit || q.FormulaTokens != nil {
		t.Fatal("unverified video allowed")
	}
	// Total duration is checked on probed content, rather than trusting a user estimate.
	long := filepath.Join(t.TempDir(), "long.mp4")
	if out, err := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-f", "lavfi", "-i", "color=s=864x496:r=24", "-t", "8", "-c:v", "libx264", long).CombinedOutput(); err != nil {
		t.Fatalf("fixture %v %s", err, out)
	}
	video, err = os.ReadFile(long)
	if err != nil {
		t.Fatal(err)
	}
	e.Media = mockMedia(t, func(*http.Request) (int, []byte) { return 200, video })
	o.Videos = append(o.Videos, "https://example.com/second.mp4")
	if _, _, _, _, err := prepare(context.Background(), o, e.Media); err == nil {
		t.Fatal("accepted video total over 15 seconds")
	}
}

func TestCustomParametersReachAPIAndActualSpecification(t *testing.T) {
	var posts atomic.Int32
	s := testServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posts.Add(1)
			var req Request
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
			}
			if req.Model != "ep-verified-fast" || req.Audio || req.Resolution != "720p" || req.Ratio != "9:16" || req.Duration != 8 || len(req.Content) != 3 {
				t.Errorf("customized request lost parameters: %+v", req)
			}
			if req.Content[1].Role != "reference_image" || req.Content[2].Role != "reference_image" {
				t.Error("reference images not passed")
			}
			fmt.Fprintf(w, `{"id":"cgt-custom-%d"}`, posts.Load())
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/")
		fmt.Fprintf(w, `{"id":%q,"status":"succeeded","model":%q,"resolution":"720p","ratio":"9:16","duration":8,"generate_audio":false,"usage":{"completion_tokens":123000,"total_tokens":124000},"content":{"video_url":"https://example.com/result.mp4"}}`, id, models["fast"])
	}))
	dir := t.TempDir()
	img := filepath.Join(dir, "reference.png")
	fixture(t, img)
	e := newEngine("mock-secret")
	e.Base = s.URL
	e.Media = mockMedia(t, func(*http.Request) (int, []byte) { return 200, []byte("video") })
	o := options(t, "-model", "ep-verified-fast", "-price-model", "fast", "-audio=false", "-resolution", "720p", "-ratio", "9:16", "-duration", "8", "-count", "2", "-image", img, "-image", img, "-out", filepath.Join(dir, "job"))
	req, q := quoteFor(t, o, e)
	o.Confirm = q.ConfirmationID
	r, path, err := e.submit(context.Background(), o, q, req)
	if err != nil {
		t.Fatal(err)
	}
	if posts.Load() != 2 || len(r.Tasks) != 2 {
		t.Fatal("count not applied")
	}
	var out bytes.Buffer
	printRecord(&out, r, path)
	if !strings.Contains(out.String(), "声音 无声") || !strings.Contains(out.String(), "completion_tokens：123000") {
		t.Fatal(out.String())
	}
	if r.Tasks[0].EstimatedFee == nil || *r.Tasks[0].EstimatedFee != 4.551 {
		t.Fatal("wrong actual price", r.Tasks[0])
	}
	changed := r.Tasks[0]
	changed.Model = models["standard"]
	calculateFee(&changed, q)
	if changed.EstimatedFee != nil {
		t.Fatal("mispriced different actual model")
	}
	changed = r.Tasks[0]
	changed.Resolution = "1080p"
	calculateFee(&changed, q)
	if changed.EstimatedFee != nil {
		t.Fatal("mispriced different actual resolution")
	}
}

func TestResumeKeepsPreviouslyReturnedUsageAndMetadata(t *testing.T) {
	e := newEngine("mock-secret")
	o := options(t, "-out", t.TempDir())
	_, q := quoteFor(t, o, e)
	tokens, total := int64(60000), int64(70000)
	audio := true
	r := Record{Version: 1, Quote: q, Tasks: []Task{{ID: "cgt-partial-response", Status: "succeeded", Model: models["mini"], Resolution: "480p", Ratio: "16:9", Duration: 5, Audio: &audio, Usage: &Usage{Completion: &tokens, Total: &total}}}}
	path := filepath.Join(o.Out, "run.json")
	if err := saveRecord(path, &r, true); err != nil {
		t.Fatal(err)
	}
	s := testServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":"cgt-partial-response","status":"succeeded","usage":{"total_tokens":70000},"content":{"video_url":"https://example.com/result.mp4"}}`)
	}))
	e.Base = s.URL
	e.Media = mockMedia(t, func(*http.Request) (int, []byte) { return 200, []byte("video") })
	o.Resume = path
	updated, _, err := e.resume(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	actual := updated.Tasks[0]
	if actual.Usage.Completion == nil || *actual.Usage.Completion != tokens || actual.Audio == nil || !*actual.Audio || actual.Model != models["mini"] || actual.EstimatedFee == nil || *actual.EstimatedFee != 1.38 {
		t.Fatal("lost saved result fields", actual)
	}
}
