package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	s, e := newStudio(ctx, root, t.TempDir(), "")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		cancel()
		s.mu.Lock()
		for _, stop := range s.running {
			stop()
		}
		s.mu.Unlock()
		s.wg.Wait()
		s.store.close()
	})
	return s
}
func addProject(t *testing.T, s *Studio) Project {
	t.Helper()
	p := Project{ID: uid(), Idea: "一只小猫的城市旅行", Revision: 1, Created: now(), Status: "draft", Attempts: map[string]Attempt{}}
	if e := s.store.change(func(st *State) error { st.Projects[p.ID] = &p; return nil }); e != nil {
		t.Fatal(e)
	}
	return p
}
func waitProject(t *testing.T, s *Studio, id string) Project {
	t.Helper()
	end := time.Now().Add(25 * time.Second)
	for time.Now().Before(end) {
		p, _ := s.store.project(id)
		s.mu.Lock()
		_, active := s.running[id]
		s.mu.Unlock()
		if !active && p.Status != "running" {
			return p
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("任务超时")
	return Project{}
}
func TestBoardValidationAndDependency(t *testing.T) {
	b := mockBoard(Project{Idea: "cat"}, "", "")
	if e := normalizeBoard(&b); e != nil {
		t.Fatal(e)
	}
	b.Shots[0].References = []string{"s2"}
	if normalizeBoard(&b) == nil {
		t.Fatal("forward reference accepted")
	}
	b.Shots[0].References = nil
	b.Shots[0].Duration = 15
	if normalizeBoard(&b) == nil {
		t.Fatal("overlong film accepted")
	}
}
func TestPreserveOnlyUnaffectedAssets(t *testing.T) {
	old := mockBoard(Project{}, "", "")
	old.Characters[0].Image = "character.png"
	for i := range old.Shots {
		old.Shots[i].Image = old.Shots[i].ID + ".png"
		old.Shots[i].Video = old.Shots[i].ID + ".mp4"
	}
	next := clone(old)
	next.Shots[0].ImagePrompt += " rainy"
	next.Shots[0].Image = ""
	next.Shots[0].Video = ""
	preserveAssets(&old, &next)
	if next.Shots[1].Image != old.Shots[1].Image || next.Shots[0].Image != "" {
		t.Fatal("wrong invalidation")
	}
	next = clone(old)
	for i := range next.Shots {
		next.Shots[i].Image = ""
		next.Shots[i].Video = ""
	}
	next.Shots[1].References = []string{"s1"}
	next.Shots[0].ImagePrompt += "rain"
	preserveAssets(&old, &next)
	if next.Shots[1].Image != "" {
		t.Fatal("dependent reused")
	}
}
func TestConfirmRevisionSettingsReplay(t *testing.T) {
	s := testStudio(t)
	p := addProject(t, s)
	readyAssets(t, s, p.ID)
	q, e := s.makeQuote(p.ID, "storyboard", "", "")
	if e != nil {
		t.Fatal(e)
	}
	s.store.update(p.ID, func(p *Project) { p.Revision++ })
	if s.confirm(p.ID, q.ID, false) == nil {
		t.Fatal("stale revision accepted")
	}
	q, e = s.makeQuote(p.ID, "storyboard", "", "")
	if e != nil {
		t.Fatal(e)
	}
	s.store.change(func(st *State) error { st.Settings.Providers[0].Name = "changed"; return nil })
	if s.confirm(p.ID, q.ID, false) == nil {
		t.Fatal("stale config accepted")
	}
	q, e = s.makeQuote(p.ID, "storyboard", "", "")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.confirm(p.ID, q.ID, false); e != nil {
		t.Fatal(e)
	}
	if e = s.confirm(p.ID, q.ID, false); e != nil {
		t.Fatal(e)
	}
	p = waitProject(t, s, p.ID)
	if p.Status != "review" || len(p.Boards) != 1 {
		t.Fatalf("state=%s boards=%d err=%s", p.Status, len(p.Boards), p.Error)
	}
	count := len(p.Attempts)
	if count == 0 {
		t.Fatal("no requests recorded")
	}
	if e = s.confirm(p.ID, q.ID, false); e != nil {
		t.Fatal(e)
	}
	if e = s.run(context.Background(), p.ID, defaults(), "storyboard_resume", "", ""); e != nil {
		t.Fatal(e)
	}
	resumed, _ := s.store.project(p.ID)
	if len(resumed.Attempts) != count {
		t.Fatal("resume duplicated requests")
	}
}
func TestUnknownRequestNeverPostsTwice(t *testing.T) {
	s := testStudio(t)
	p := addProject(t, s)
	var posts atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { posts.Add(1); w.WriteHeader(502) }))
	defer endpoint.Close()
	settings := defaults()
	settings.Providers[0].Adapter = "openai"
	settings.Providers[0].BaseURL = endpoint.URL
	settings.Providers[0].KeyEnv = "TEST_STUDIO_KEY"
	t.Setenv("TEST_STUDIO_KEY", "private-value")
	for i := 0; i < 2; i++ {
		if _, e := s.request(context.Background(), p.ID, settings, "text", "hello", nil, 0); e == nil {
			t.Fatal("failed response succeeded")
		}
	}
	if posts.Load() != 1 {
		t.Fatalf("POST count %d", posts.Load())
	}
	raw, _ := json.Marshal(s.store.settings())
	if bytes.Contains(raw, []byte("private-value")) {
		t.Fatal("secret leak")
	}
}
func TestSavedResponseDoesNotRegenerate(t *testing.T) {
	s := testStudio(t)
	p := addProject(t, s)
	var posts atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"ok\":true}"}}],"usage":{"completion_tokens":5}}`))
	}))
	defer endpoint.Close()
	settings := defaults()
	settings.Providers[0].Adapter = "openai"
	settings.Providers[0].BaseURL = endpoint.URL
	settings.Providers[0].KeyEnv = "TEST_STUDIO_KEY"
	t.Setenv("TEST_STUDIO_KEY", "private-value")
	file, e := s.request(context.Background(), p.ID, settings, "text", "hello", nil, 0)
	if e != nil {
		t.Fatal(e)
	}
	again, e := s.request(context.Background(), p.ID, settings, "text", "hello", nil, 0)
	if e != nil || file != again || posts.Load() != 1 {
		t.Fatalf("cache failure %s %v %d", again, e, posts.Load())
	}
}
func TestAtomicClaim(t *testing.T) {
	s := testStudio(t)
	p := addProject(t, s)
	var posts atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		time.Sleep(30 * time.Millisecond)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{}"}}]}`))
	}))
	defer endpoint.Close()
	settings := defaults()
	settings.Providers[0].Adapter = "openai"
	settings.Providers[0].BaseURL = endpoint.URL
	settings.Providers[0].KeyEnv = "TEST_CLAIM_KEY"
	t.Setenv("TEST_CLAIM_KEY", "local-test")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = s.request(context.Background(), p.ID, settings, "text", "same", nil, 0)
		}()
	}
	wg.Wait()
	if posts.Load() != 1 {
		t.Fatalf("duplicate POSTs: %d", posts.Load())
	}
}
func TestSettingsCapabilitiesAndOrigin(t *testing.T) {
	s := testStudio(t)
	settings := defaults()
	settings.Providers[0].Models[0].ReferenceImages = false
	if checkReferences(settings.Providers[0].Models[0], []string{"x.png"}) == nil {
		t.Fatal("refs silently dropped")
	}
	settings.Providers[0].Adapter = "seedance"
	settings.Providers[0].BaseURL = "https://evil.example/v1"
	settings.Providers[0].KeyEnv = "KEY"
	if validateSettings(settings) == nil {
		t.Fatal("unsafe seedance endpoint")
	}
	r := s.router()
	req := httptest.NewRequest("POST", "/api/projects", bytes.NewBufferString(`{"idea":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
}
func TestRestartRecovery(t *testing.T) {
	dir := t.TempDir()
	store, e := openStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	p := &Project{ID: "p", Status: "running", Attempts: map[string]Attempt{"a": {State: "submission_unknown"}}, Tasks: []Task{{State: "running"}}}
	store.change(func(st *State) error { st.Projects["p"] = p; return nil })
	store.close()
	store, e = openStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer store.close()
	got, _ := store.project("p")
	if got.Status != "interrupted" || got.Attempts["a"].State != "submission_unknown" || got.Tasks[0].State != "interrupted" {
		t.Fatal(got)
	}
}
func TestMockFilmIntegration(t *testing.T) {
	if os.Getenv("LYPIT_INTEGRATION") != "1" {
		t.Skip("set LYPIT_INTEGRATION=1 to render a real local film")
	}
	for _, tool := range []string{"ffmpeg", "ffprobe", "hypit"} {
		if _, e := exec.LookPath(tool); e != nil {
			t.Fatal(e)
		}
	}
	s := testStudio(t)
	p := addProject(t, s)
	p = runAssetQuote(t, s, p.ID, "asset_plan", "")
	p = runAssetQuote(t, s, p.ID, "character_masters", "")
	p = approveAssetsForTest(t, s, p.ID, true)
	p = runAssetQuote(t, s, p.ID, "reference_assets", "")
	p = approveAssetsForTest(t, s, p.ID, false)
	a := assets(p)
	w := modelAPI(t, s, "POST", "/api/projects/"+p.ID+"/assets/lock", map[string]int{"version": a.Version, "revision": p.Revision})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	p, _ = s.store.project(p.ID)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	q, e := s.makeQuote(p.ID, "storyboard", "", "")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.store.change(func(st *State) error {
		sq := st.Quotes[q.ID]
		sq.Quote.Used = true
		st.Quotes[q.ID] = sq
		st.Projects[p.ID].ActiveQuote = q.ID
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if e := s.run(ctx, p.ID, defaults(), "storyboard", "", ""); e != nil {
		t.Fatal(e)
	}
	s.store.update(p.ID, func(p *Project) { p.Boards[0].Approved = true })
	videoQuote, err := s.makeQuote(p.ID, "video", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.store.change(func(st *State) error {
		sq := st.Quotes[videoQuote.ID]
		sq.Quote.Used = true
		st.Quotes[videoQuote.ID] = sq
		st.Projects[p.ID].ActiveQuote = videoQuote.ID
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if e := s.run(ctx, p.ID, defaults(), "video", "", ""); e != nil {
		t.Fatal(e)
	}
	p, _ = s.store.project(p.ID)
	if p.Output == "" {
		t.Fatal("no final film")
	}
	audits := 0
	for _, a := range p.Attempts {
		if a.Kind == "vision" && strings.Contains(a.Prompt, "当前视频起始帧") {
			audits++
			if len(a.References) < 4 {
				t.Fatal("video audit missing real frames and selected image")
			}
			for i := 0; i < 3; i++ {
				if !strings.Contains(a.References[i].File, "video-review") {
					t.Fatal("audit used storyboard instead of video frame")
				}
			}
		}
	}
	if audits != len(board(p).Shots) {
		t.Fatal("missing video audits")
	}
	previousOutput, attemptCount := p.Output, len(p.Attempts)
	if err := s.run(ctx, p.ID, defaults(), "video", "", ""); err != nil {
		t.Fatal(err)
	}
	repeated, _ := s.store.project(p.ID)
	if repeated.Output != previousOutput || len(repeated.Attempts) != attemptCount {
		t.Fatal("completed assets were not reused")
	}

	probe, e := s.probe(ctx, s.abs(p.Output))
	if e != nil {
		t.Fatal(e)
	}
	t.Logf("final file=%s duration=%s", s.abs(p.Output), probe.Format.Duration)
	if output := os.Getenv("LYPIT_DEMO_OUTPUT"); output != "" {
		b, err := os.ReadFile(s.abs(p.Output))
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(output, b, 0600); err != nil {
			t.Fatal(err)
		}
		report, err := os.ReadFile(filepath.Join(filepath.Dir(s.abs(p.Output)), "validation.json"))
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(filepath.Dir(output), "validation.json"), report, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDataDirectoryExclusiveLock(t *testing.T) {
	dir := t.TempDir()
	first, e := openStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	if second, e := openStore(dir); e == nil {
		second.close()
		t.Fatal("same directory accepted twice")
	}
	first.close()
	reopened, e := openStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	reopened.close()
}
func TestUnknownCostRequiresExplicitConsent(t *testing.T) {
	s := testStudio(t)
	p := addProject(t, s)
	readyAssets(t, s, p.ID)
	q, e := s.makeQuote(p.ID, "storyboard", "", "")
	if e != nil {
		t.Fatal(e)
	}
	s.store.change(func(st *State) error {
		sq := st.Quotes[q.ID]
		sq.Quote.Estimate = nil
		st.Quotes[q.ID] = sq
		return nil
	})
	if s.confirm(p.ID, q.ID, false) == nil {
		t.Fatal("unknown cost accepted silently")
	}
}
func TestRevisionPreservesUnaffectedImageRequests(t *testing.T) {
	s := testStudio(t)
	p := addProject(t, s)
	if e := s.run(context.Background(), p.ID, defaults(), "storyboard", "", ""); e != nil {
		t.Fatal(e)
	}
	before, _ := s.store.project(p.ID)
	old := board(before)
	if e := s.run(context.Background(), p.ID, defaults(), "storyboard", "换成夜晚", "s2"); e != nil {
		t.Fatal(e)
	}
	after, _ := s.store.project(p.ID)
	next := board(after)
	if next.Version != 2 || old.Shots[0].Image != next.Shots[0].Image || old.Shots[1].Image == next.Shots[1].Image || len(after.Attempts) != len(before.Attempts)+3 {
		t.Fatal("unrelated images regenerated")
	}
}
