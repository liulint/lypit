package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testStudio(t *testing.T) *Studio {
	t.Helper()
	root, e := filepath.Abs("..")
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	dir := t.TempDir()
	if t.Name() == "TestMockFilmIntegration" && os.Getenv("LYPIT_TEST_DATA") != "" {
		dir = os.Getenv("LYPIT_TEST_DATA")
	}
	s, e := newStudio(ctx, root, dir)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		cancel()
		s.mu.Lock()
		for _, c := range s.running {
			c()
		}
		s.mu.Unlock()
		s.wg.Wait()
		s.store.close()
	})
	return s
}
func request(t *testing.T, s *Studio, method, path string, body any, want int) []byte {
	t.Helper()
	var b []byte
	if body != nil {
		b = jsonBytes(body)
	}
	req := httptest.NewRequest(method, "http://127.0.0.1:8080"+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Lypit-Client", "studio")
	w := httptest.NewRecorder()
	s.router().ServeHTTP(w, req)
	if w.Code != want {
		t.Fatalf("%s %s: status %d want %d: %s", method, path, w.Code, want, w.Body.String())
	}
	return w.Body.Bytes()
}
func getProject(t *testing.T, s *Studio, id string) Project {
	t.Helper()
	p, e := s.store.project(id)
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func createProject(t *testing.T, s *Studio) Project {
	t.Helper()
	var p Project
	b := request(t, s, "POST", "/api/projects", map[string]string{"title": "旧书里的明天", "idea": "青年在书店里发现一本记录着明天的旧书。", "style": "温暖电影感"}, 200)
	if e := json.Unmarshal(b, &p); e != nil {
		t.Fatal(e)
	}
	return p
}
func waitTask(t *testing.T, s *Studio, id string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		v, e := s.store.task(id)
		if e != nil {
			t.Fatal(e)
		}
		if v.Status == "completed" {
			return
		}
		if v.Status == "failed" || v.Status == "cancelled" {
			t.Fatalf("task %s %s: %s (data %s)", v.Stage, v.Status, v.Message, s.data)
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatal("task timeout")
}
func stage(t *testing.T, s *Studio, p Project, name string) Project {
	t.Helper()
	if name == "boards" && !p.PlanApproved {
		p = stage(t, s, p, "board_plan")
		for i := range p.PlannedShots {
			p.PlannedShots[i].ReviewStatus = "reviewed"
		}
		request(t, s, "PUT", "/api/projects/"+p.ID+"/board-plan", map[string]any{"revision": p.Revision, "shots": p.PlannedShots, "approve": true}, 200)
		p = getProject(t, s, p.ID)
	}
	var q Quote
	b := request(t, s, "POST", "/api/projects/"+p.ID+"/quotes", map[string]any{"revision": p.Revision, "stage": name}, 200)
	if e := json.Unmarshal(b, &q); e != nil {
		t.Fatal(e)
	}
	var job Task
	b = request(t, s, "POST", "/api/quotes/"+q.ID+"/confirm", map[string]bool{"allow_unknown": false}, 200)
	if e := json.Unmarshal(b, &job); e != nil {
		t.Fatal(e)
	}
	waitTask(t, s, job.ID)
	return getProject(t, s, p.ID)
}
func approve(t *testing.T, s *Studio, p Project, asset, part string) Project {
	t.Helper()
	request(t, s, "POST", "/api/projects/"+p.ID+"/assets/"+asset+"/approve", map[string]any{"revision": p.Revision, "part": part}, 200)
	return getProject(t, s, p.ID)
}
func lock(t *testing.T, s *Studio, p Project, kind string) Project {
	t.Helper()
	request(t, s, "POST", "/api/projects/"+p.ID+"/"+kind+"/lock", map[string]int{"revision": p.Revision}, 200)
	return getProject(t, s, p.ID)
}

func TestNoLoginAndLocalBoundary(t *testing.T) {
	s := testStudio(t)
	request(t, s, "GET", "/api/health", nil, 200)
	p := createProject(t, s)
	if p.ID == "" {
		t.Fatal("missing project")
	}
	r := httptest.NewRequest("POST", "http://127.0.0.1:8080/api/projects", bytes.NewReader([]byte(`{}`)))
	w := httptest.NewRecorder()
	s.router().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-site write allowed")
	}
	r = httptest.NewRequest("GET", "http://evil.invalid/api/projects", nil)
	w = httptest.NewRecorder()
	s.router().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("DNS rebinding host allowed")
	}
	request(t, s, "GET", "/api/missing", nil, 404)
	request(t, s, "GET", "/media/a/response.bin", nil, 404)
}
func TestStageGatesAndImmutableReferences(t *testing.T) {
	s := testStudio(t)
	p := createProject(t, s)
	request(t, s, "POST", "/api/projects/"+p.ID+"/quotes", map[string]any{"revision": p.Revision, "stage": "boards"}, 400)
	p = stage(t, s, p, "analyze")
	p = stage(t, s, p, "masters")
	request(t, s, "POST", "/api/projects/"+p.ID+"/quotes", map[string]any{"revision": p.Revision, "stage": "details"}, 400)
	for _, a := range p.Assets {
		if a.Kind == "character" {
			p = approve(t, s, p, a.ID, "master")
		}
	}
	p = stage(t, s, p, "details")
	for _, a := range p.Assets {
		if a.Kind == "character" {
			if len(a.Views) != 4 {
				t.Fatal("sheet was not cropped")
			}
			p = approve(t, s, p, a.ID, "sheet")
		} else {
			p = approve(t, s, p, a.ID, "master")
		}
	}
	p = lock(t, s, p, "assets")
	request(t, s, "POST", "/api/projects/"+p.ID+"/quotes", map[string]any{"revision": p.Revision, "stage": "videos"}, 400)
	im := p.Assets[0].Master
	if e := os.WriteFile(s.media(im.File), []byte("tampered"), 0600); e != nil {
		t.Fatal(e)
	}
	request(t, s, "POST", "/api/projects/"+p.ID+"/quotes", map[string]any{"revision": p.Revision, "stage": "boards"}, 400)
}
func TestQuoteStalenessAndIdempotency(t *testing.T) {
	s := testStudio(t)
	p := createProject(t, s)
	var q Quote
	_ = json.Unmarshal(request(t, s, "POST", "/api/projects/"+p.ID+"/quotes", map[string]any{"revision": p.Revision, "stage": "analyze"}, 200), &q)
	p.Title = "修改后的标题"
	request(t, s, "PUT", "/api/projects/"+p.ID, p, 200)
	request(t, s, "POST", "/api/quotes/"+q.ID+"/confirm", map[string]bool{}, 400)
	p = getProject(t, s, p.ID)
	_ = json.Unmarshal(request(t, s, "POST", "/api/projects/"+p.ID+"/quotes", map[string]any{"revision": p.Revision, "stage": "analyze"}, 200), &q)
	var wg sync.WaitGroup
	ids := make(chan string, 8)
	errs := make(chan string, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("POST", "http://127.0.0.1:8080/api/quotes/"+q.ID+"/confirm", bytes.NewReader([]byte(`{}`)))
			req.Header.Set("X-Lypit-Client", "studio")
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			s.router().ServeHTTP(w, req)
			var job Task
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &job) != nil {
				errs <- fmt.Sprint(w.Code, w.Body.String())
				return
			}
			ids <- job.ID
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for e := range errs {
		t.Fatal(e)
	}
	id := ""
	for got := range ids {
		if id != "" && id != got {
			t.Fatal("duplicate task")
		}
		id = got
	}
	waitTask(t, s, id)
	jobs, e := s.store.tasks(p.ID)
	if e != nil || len(jobs) != 1 {
		t.Fatal("expected one durable task", e, len(jobs))
	}
	history, e := s.store.history(p.ID)
	if e != nil || len(history) != 3 {
		t.Fatal("history missing", e, len(history))
	}
}
func TestPersistenceAndExclusiveStore(t *testing.T) {
	dir := t.TempDir()
	st, e := openStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	p, e := st.create("持久化", "故事", "电影", defaultAspect, defaultDuration)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = openStore(dir); e == nil {
		t.Fatal("second writer accepted")
	}
	if e = st.close(); e != nil {
		t.Fatal(e)
	}
	st, e = openStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer st.close()
	stored, e := st.project(p.ID)
	if e != nil || stored.Title != p.Title {
		t.Fatal("project not persisted", e)
	}
}
func TestUnknownProviderSubmissionNeverResubmits(t *testing.T) {
	t.Setenv("TEST_PROVIDER_KEY", "fake-test-key")
	s := testStudio(t)
	p := createProject(t, s)
	var hits atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		h := w.(http.Hijacker)
		conn, _, _ := h.Hijack()
		conn.Close()
	}))
	defer provider.Close()
	task := Task{ID: uid(), ProjectID: p.ID, Status: "running", Input: p, Config: defaultSettings()}
	_, e := s.store.db.Exec("INSERT INTO tasks VALUES(?,?,?,?,?,?)", task.ID, p.ID, "running", jsonBytes(task), jsonBytes(p), jsonBytes(task.Config))
	if e != nil {
		t.Fatal(e)
	}
	ep := Endpoint{Base: provider.URL, KeyEnv: "TEST_PROVIDER_KEY"}
	for i := 0; i < 2; i++ {
		if _, e = s.paid(context.Background(), &task, "test", ep, "/paid", "application/json", []byte(`{}`)); e == nil {
			t.Fatal("expected unknown result")
		}
	}
	if hits.Load() != 1 {
		t.Fatalf("paid requests=%d", hits.Load())
	}
}
func TestMockFilmIntegration(t *testing.T) {
	if os.Getenv("LYPIT_INTEGRATION") != "1" {
		t.Skip("set LYPIT_INTEGRATION=1 for local FFmpeg export")
	}
	// Exercise the entire film pipeline with only the two declared local dependencies available.
	tools := t.TempDir()
	for _, name := range []string{"ffmpeg", "ffprobe"} {
		path, err := exec.LookPath(name)
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(tools, name)
		if runtime.GOOS == "windows" {
			target += ".exe"
			err = copyTestFile(path, target, 0700)
		} else {
			err = os.Symlink(path, target)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", tools)
	for _, spec := range []struct {
		aspect   string
		duration int
	}{{"9:16", 60}, {"16:9", 17}} {
		t.Run(fmt.Sprintf("%s_%ds", spec.aspect, spec.duration), func(t *testing.T) { testMockFilmSpec(t, spec.aspect, spec.duration) })
	}
}
func testMockFilmSpec(t *testing.T, aspect string, duration int) {
	s := testStudio(t)
	var p Project
	if e := json.Unmarshal(request(t, s, "POST", "/api/projects", map[string]any{"title": "规格验收", "idea": "一本旧书带来的故事", "style": "温暖电影感", "aspect_ratio": aspect, "duration": duration, "audio_mode": map[bool]string{true: "narration", false: "dialogue"}[aspect == "16:9"]}, 200), &p); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"analyze", "masters"} {
		p = stage(t, s, p, name)
	}
	for _, a := range p.Assets {
		if a.Kind == "character" {
			p = approve(t, s, p, a.ID, "master")
		}
	}
	p = stage(t, s, p, "details")
	for _, a := range p.Assets {
		if a.Kind == "character" {
			p = approve(t, s, p, a.ID, "sheet")
		} else {
			p = approve(t, s, p, a.ID, "master")
		}
	}
	p = lock(t, s, p, "assets")
	q, e := s.quoteFor(p, defaultSettings(), "board_plan")
	if e != nil || len(q.Items) != 1 || q.Duration != duration || q.AspectRatio != aspect {
		t.Fatal("quote ignores video spec", q, e)
	}
	p = stage(t, s, p, "boards")
	if e := validateShotPlan(p); e != nil {
		t.Fatal(e)
	}
	for _, q := range p.Shots {
		request(t, s, "PUT", "/api/projects/"+p.ID+"/shots/"+q.ID, map[string]any{"revision": p.Revision, "selected": 0, "approved": true, "narration": q.Narration}, 200)
		p = getProject(t, s, p.ID)
	}
	p = lock(t, s, p, "boards")
	p = stage(t, s, p, "videos")
	if dialogueMode(p) && p.Voice != "" {
		t.Fatal("dialogue generated unwanted narrator")
	}
	if !dialogueMode(p) && p.Voice == "" {
		t.Fatal("narrator mode lost voice")
	}
	p = stage(t, s, p, "render")
	if p.Output == "" {
		t.Fatal("no film")
	}
	if e := s.validateFilm(context.Background(), p.Output, p); e != nil {
		t.Fatal(e)
	}
	wrongDuration := p
	wrongDuration.Duration += 5
	if e := s.validateFilm(context.Background(), p.Output, wrongDuration); e == nil {
		t.Fatal("film duration check was bypassed")
	}
	wrongAspect := p
	wrongAspect.AspectRatio = "16:9"
	if aspect == "16:9" {
		wrongAspect.AspectRatio = "9:16"
	}
	if e := s.validateFilm(context.Background(), p.Output, wrongAspect); e == nil {
		t.Fatal("film dimensions check was bypassed")
	}
	request(t, s, "GET", "/api/projects/"+p.ID+"/export", nil, 200)
	t.Log("完整本地模拟成片：", s.media(p.Output))
}

func TestProviderReceiptIsReused(t *testing.T) {
	t.Setenv("TEST_PROVIDER_KEY", "fake-test-key")
	s := testStudio(t)
	p := createProject(t, s)
	var hits atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`{"id":"remote-123"}`))
	}))
	defer provider.Close()
	task := Task{ID: uid(), ProjectID: p.ID, Status: "running", Input: p, Config: defaultSettings()}
	_, e := s.store.db.Exec("INSERT INTO tasks VALUES(?,?,?,?,?,?)", task.ID, p.ID, "running", jsonBytes(task), jsonBytes(p), jsonBytes(task.Config))
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		b, e := s.paid(context.Background(), &task, "submit", Endpoint{Base: provider.URL, KeyEnv: "TEST_PROVIDER_KEY"}, "/jobs", "application/json", []byte(`{}`))
		if e != nil || string(b) != `{"id":"remote-123"}` {
			t.Fatal("receipt not reused", e, string(b))
		}
	}
	if hits.Load() != 1 {
		t.Fatal("duplicate provider submission", hits.Load())
	}
}

func TestRestartPreservesOriginalInputAndMarksInterrupted(t *testing.T) {
	dir := t.TempDir()
	st, e := openStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	p, e := st.create("重启恢复", "故事", "电影", defaultAspect, defaultDuration)
	if e != nil {
		t.Fatal(e)
	}
	task := Task{ID: uid(), ProjectID: p.ID, Status: "running", Input: p, Config: defaultSettings()}
	_, e = st.db.Exec("INSERT INTO tasks VALUES(?,?,?,?,?,?)", task.ID, p.ID, "running", jsonBytes(task), jsonBytes(p), jsonBytes(task.Config))
	if e != nil {
		t.Fatal(e)
	}
	if e = st.close(); e != nil {
		t.Fatal(e)
	}
	st, e = openStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer st.close()
	got, e := st.task(task.ID)
	if e != nil || got.Status != "interrupted" || got.Input.ID != p.ID || got.Config.Mode != "mock" {
		t.Fatal("restart lost input/config", e, got.Status)
	}
}

func TestChangingSettingsInvalidatesUnconfirmedQuote(t *testing.T) {
	s := testStudio(t)
	p := createProject(t, s)
	var q Quote
	_ = json.Unmarshal(request(t, s, "POST", "/api/projects/"+p.ID+"/quotes", map[string]any{"revision": p.Revision, "stage": "analyze"}, 200), &q)
	cfg, e := s.store.settings()
	if e != nil {
		t.Fatal(e)
	}
	request(t, s, "PUT", "/api/settings", cfg, 200)
	request(t, s, "POST", "/api/quotes/"+q.ID+"/confirm", map[string]bool{}, 400)
}
