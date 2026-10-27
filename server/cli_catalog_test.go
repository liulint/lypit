package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const catalogFixture = `[{"slug":"model-a","display_name":"Model A","description":"test","default_reasoning_level":"low","supported_reasoning_levels":[{"effort":"low"},{"effort":"high"},{"effort":"high"}],"service_tiers":[{"id":"priority","name":"Fast"}]},{"id":"model-b","name":"Model B","additional_speed_tiers":["fast"]},{"slug":"model-a"},{"slug":"secret-model","visibility":"hide"},{"slug":"also-hidden","visibility":"hidden"},{"slug":""}]`

func TestCodexCatalogFormatsAndMetadata(t *testing.T) {
	for _, raw := range []string{catalogFixture, `{"models":` + catalogFixture + `}`} {
		c, e := parseCodexCatalog([]byte(raw))
		if e != nil {
			t.Fatal(e)
		}
		if strings.Join(c.Models, ",") != "model-a,model-b" || len(c.Options) != 2 || c.Options[0].Label != "Model A" || c.Options[0].Description != "test" || c.Options[0].DefaultReasoning != "low" || len(c.Options[0].Reasoning) != 2 || c.Options[1].ServiceTiers[0].ID != "priority" {
			t.Fatalf("incorrect catalog: %+v", c)
		}
	}
	for _, raw := range []string{"", `{}`, `{"models":null}`, `{"models":"bad"}`, `not json`} {
		if _, e := parseCodexCatalog([]byte(raw)); e == nil {
			t.Fatalf("invalid format accepted: %s", raw)
		}
	}
}

func TestCodexCatalogCacheRefreshIsolationAndStale(t *testing.T) {
	var cache cliCatalogCache
	var calls atomic.Int32
	fetch := func(context.Context) (CLICatalog, error) {
		calls.Add(1)
		return parseCodexCatalog([]byte(catalogFixture))
	}
	live := cache.get(context.Background(), "one", false, fetch)
	cached := cache.get(context.Background(), "one", false, fetch)
	if live.Source != "live" || cached.Source != "cache" || calls.Load() != 1 {
		t.Fatal("cache miss")
	}
	cached.Models[0] = "tampered"
	if cache.get(context.Background(), "one", false, fetch).Models[0] != "model-a" {
		t.Fatal("cache aliases response")
	}
	fail := func(context.Context) (CLICatalog, error) { return CLICatalog{}, errors.New("secret-token") }
	stale := cache.get(context.Background(), "one", true, fail)
	if !stale.Stale || stale.Source != "cache" || len(stale.Models) != 2 || strings.Contains(stale.Error, "secret-token") {
		t.Fatal(stale)
	}
	if c := cache.get(context.Background(), "one", false, fetch); !c.Stale {
		t.Fatal("failed refresh lost stale marker")
	}
	if c := cache.get(context.Background(), "two", true, fail); c.Source != "unavailable" || len(c.Models) != 0 {
		t.Fatal("cross identity cache reused")
	}
	cache.mu.Lock()
	e := cache.entries["one"]
	e.At = time.Now().Add(-6 * time.Minute)
	cache.entries["one"] = e
	cache.mu.Unlock()
	cache.get(context.Background(), "one", false, fetch)
	if calls.Load() != 2 {
		t.Fatal("expired cache not refreshed")
	}
}

func TestCodexCatalogConcurrentAndCancelledReaders(t *testing.T) {
	var cache cliCatalogCache
	var calls atomic.Int32
	start, release := make(chan struct{}), make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		cache.get(context.Background(), "same", false, func(context.Context) (CLICatalog, error) {
			calls.Add(1)
			close(start)
			<-release
			return parseCodexCatalog([]byte(catalogFixture))
		})
	}()
	<-start
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := cache.get(ctx, "same", false, func(context.Context) (CLICatalog, error) { t.Error("duplicate fetch"); return CLICatalog{}, nil })
	if c.Source != "unavailable" {
		t.Fatal(c)
	}
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal(calls.Load())
	}
}

func TestCodexDraftCatalogDoesNotSaveOrGenerate(t *testing.T) {
	s := testStudio(t)
	bin := fakeCLI(t, "codex", "printf '%s' '"+catalogFixture+"'")
	original := hash(s.store.settings())
	w := modelAPI(t, s, "POST", "/api/cli/models", map[string]any{"kind": "codex", "path": bin})
	var c CLICatalog
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &c) != nil || len(c.Options) != 2 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if hash(s.store.settings()) != original {
		t.Fatal("discovery saved draft")
	}
	other := fakeCLI(t, "codex", `printf '%s' '{"models":[{"id":"other"}]}'`)
	if c = s.cliModels(context.Background(), &CLIConfig{Kind: "codex", Path: other}, false); len(c.Models) != 1 || c.Models[0] != "other" {
		t.Fatal("path cache crossed")
	}
	t.Setenv("CODEX_HOME", t.TempDir())
	if c = s.cliModels(context.Background(), &CLIConfig{Kind: "codex", Path: bin}, false); c.Source != "live" {
		t.Fatal("environment cache crossed")
	}
	config := filepath.Join(os.Getenv("CODEX_HOME"), "config.toml")
	if e := os.WriteFile(config, []byte("model = 'model-b'"), 0600); e != nil {
		t.Fatal(e)
	}
	if c = s.cliModels(context.Background(), &CLIConfig{Kind: "codex", Path: bin}, false); c.Source != "live" {
		t.Fatal("configuration cache crossed")
	}
}

func TestCodexCatalogTimeoutAndSanitizedError(t *testing.T) {
	s := testStudio(t)
	bin := fakeCLI(t, "codex", "echo secret-token >&2\nsleep 30 &\nwait")
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	start := time.Now()
	c := s.cliModels(ctx, &CLIConfig{Kind: "codex", Path: bin}, true)
	if time.Since(start) > 3*time.Second || c.Source != "unavailable" || strings.Contains(c.Error, "secret-token") {
		t.Fatal(c)
	}
}

func TestCodexCatalogStatusAndProviderRoutes(t *testing.T) {
	s := testStudio(t)
	bin := fakeCLI(t, "codex", "printf '%s' '"+catalogFixture+"'")
	settings := s.store.settings()
	p := cliTestProvider("codex", bin)
	settings.Providers = append(settings.Providers, p)
	if w := modelAPI(t, s, "PUT", "/api/settings", settings); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	for _, route := range []struct{ method, path string }{{"GET", "/api/providers/cli-codex/models"}, {"POST", "/api/providers/cli-codex/test"}} {
		w := modelAPI(t, s, route.method, route.path, map[string]any{})
		if w.Code != 200 || !strings.Contains(w.Body.String(), "Model A") || strings.Contains(w.Body.String(), "secret-model") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	t.Setenv("PATH", t.TempDir())
	w := modelAPI(t, s, "GET", "/api/cli/status", nil)
	var statuses []CLIStatus
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &statuses) != nil {
		t.Fatal(w.Body.String())
	}
	if len(statuses) != 3 || len(statuses[0].Models) != 2 || statuses[0].Catalog == nil {
		t.Fatal(statuses)
	}
}

func TestCodexOptionsArgsAndFingerprint(t *testing.T) {
	c := CLIConfig{Kind: "codex"}
	dir := t.TempDir()
	args, e := cliTaskArgs(c, "model-a", dir, &CLIOptions{Reasoning: "high", ServiceTier: "priority"})
	if e != nil {
		t.Fatal(e)
	}
	joined := strings.Join(args, "|")
	for _, v := range []string{"--model|model-a", `-c|model_reasoning_effort="high"`, `-c|service_tier="priority"`} {
		if !strings.Contains(joined, v) {
			t.Fatal(joined)
		}
	}
	args, e = cliTaskArgs(c, "default", dir, &CLIOptions{Reasoning: "default", ServiceTier: "default"})
	if e != nil || strings.Contains(strings.Join(args, "|"), "model_reasoning_effort") || strings.Contains(strings.Join(args, "|"), "--model") {
		t.Fatal(args, e)
	}
	for _, o := range []*CLIOptions{{Reasoning: `high";echo x`}, {ServiceTier: "arbitrary"}} {
		if _, e = cliTaskArgs(c, "a", dir, o); e == nil {
			t.Fatal("invalid parameter accepted")
		}
	}
	p := cliTestProvider("codex", "/test/bin")
	m := p.Models[0]
	before := cliImageFingerprint(p, m)
	m.CLIOptions = &CLIOptions{Reasoning: "high"}
	if before == cliImageFingerprint(p, m) {
		t.Fatal("image verification ignored CLI options")
	}
}

func TestCodexOptionsInvalidateQuote(t *testing.T) {
	s := testStudio(t)
	p := createProject(t, s, "catalog")
	settings := s.store.settings()
	cli := cliTestProvider("codex", fakeCLI(t, "codex", "exit 0"))
	settings.Providers = append(settings.Providers, cli)
	settings.Bindings["text"] = Binding{cli.ID, "test"}
	if w := modelAPI(t, s, "PUT", "/api/settings", settings); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	q, e := s.makeQuote(p.ID, "asset_plan", "", "")
	if e != nil {
		t.Fatal(e)
	}
	settings.Providers[len(settings.Providers)-1].Models[0].CLIOptions = &CLIOptions{Reasoning: "high"}
	if w := modelAPI(t, s, "PUT", "/api/settings", settings); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if e = s.confirm(p.ID, q.ID, true); e == nil {
		t.Fatal("old quote accepted after parameter change")
	}
}
