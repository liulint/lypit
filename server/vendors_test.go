package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVendorRegistryPreservesLegacySettings(t *testing.T) {
	old := defaultSettings()
	v := withVendorRegistry(old)
	if v.RegistryVersion != 1 || len(v.Vendors) != 4 {
		t.Fatal("legacy registry missing", v)
	}
	for _, cap := range capabilities {
		ep, e := resolveEndpoint(v, cap)
		legacy := endpointFor(old, cap)
		if e != nil || ep.Base != legacy.Base || ep.KeyEnv != legacy.KeyEnv || ep.Model != legacy.Model {
			t.Fatal("legacy endpoint changed", cap, e)
		}
	}
	if e := validateRegistry(&v); e != nil {
		t.Fatal(e)
	}
	if v.Text.Base != "" {
		t.Fatal("bindings retain redundant addresses")
	}
	if ep, e := resolveEndpoint(old, "text"); e != nil || ep.Base != old.Text.Base {
		t.Fatal("old task snapshot cannot resolve", e)
	}
}
func TestVendorBindingsRejectIncompatibleAndMissingVendors(t *testing.T) {
	v := Settings{Version: 1, RegistryVersion: 1, Mode: "remote", Vendors: []Vendor{{ID: "local", Name: "本机", Type: "cli", CLI: "codex"}}, Text: Endpoint{VendorID: "local"}}
	if e := validateRegistry(&v); e != nil {
		t.Fatal(e)
	}
	v.Image = Endpoint{VendorID: "local"}
	if e := validateRegistry(&v); e != nil {
		t.Fatal("Codex image binding rejected", e)
	}
	v.Vendors[0].CLI = "claude"
	if e := validateRegistry(&v); e == nil {
		t.Fatal("CLI bound to image without image support")
	}
	v.Image = Endpoint{}
	v.Text.VendorID = "missing"
	if e := validateRegistry(&v); e == nil {
		t.Fatal("missing vendor accepted")
	}
	v.Text.VendorID = "local"
	v.Vendors[0].Executable = "codex --anything"
	if e := validateRegistry(&v); e == nil {
		t.Fatal("command text accepted as executable path")
	}
}
func TestVendorSnapshotAndQuoteVersionBoundary(t *testing.T) {
	s := testStudio(t)
	p := createProject(t, s)
	cfg, e := s.store.settings()
	if e != nil {
		t.Fatal(e)
	}
	var q Quote
	_ = json.Unmarshal(request(t, s, "POST", "/api/projects/"+p.ID+"/quotes", map[string]any{"revision": p.Revision, "stage": "analyze"}, 200), &q)
	var task Task
	_ = json.Unmarshal(request(t, s, "POST", "/api/quotes/"+q.ID+"/confirm", map[string]bool{}, 200), &task)
	waitTask(t, s, task.ID)
	cfg.Vendors[0].Name = "改名后的厂商"
	request(t, s, "PUT", "/api/settings", cfg, 200)
	stored, e := s.store.task(task.ID)
	if e != nil || stored.Config.Vendors[0].Name == cfg.Vendors[0].Name {
		t.Fatal("task config changed retroactively", e)
	}
	p = getProject(t, s, p.ID)
	_ = json.Unmarshal(request(t, s, "POST", "/api/projects/"+p.ID+"/quotes", map[string]any{"revision": p.Revision, "stage": "masters"}, 200), &q)
	cfg, e = s.store.settings()
	if e != nil {
		t.Fatal(e)
	}
	request(t, s, "PUT", "/api/settings", cfg, 200)
	request(t, s, "POST", "/api/quotes/"+q.ID+"/confirm", map[string]bool{}, 400)
}
func cliTask(t *testing.T, s *Studio) Task {
	t.Helper()
	p := createProject(t, s)
	job := Task{ID: uid(), ProjectID: p.ID, Status: "running", Input: p, Config: defaultSettings()}
	_, e := s.store.db.Exec("INSERT INTO tasks VALUES(?,?,?,?,?,?)", job.ID, p.ID, "running", jsonBytes(job), jsonBytes(p), jsonBytes(job.Config))
	if e != nil {
		t.Fatal(e)
	}
	return job
}
func TestCLISubmissionResultReusedWithoutReexecuting(t *testing.T) {
	s := testStudio(t)
	job := cliTask(t, s)
	path := fakeCLI(t, "text")
	ep := Endpoint{Transport: "cli", CLI: "codex", Executable: path}
	var out map[string]bool
	if e := s.cliTextJSON(context.Background(), &job, "analyze", "test", ep, &out); e != nil || !out["ok"] {
		t.Fatal("fake CLI failed", e)
	}
	// The saved receipt can be consumed even when the executable is no longer available.
	ep.Executable = "/nonexistent/cli"
	if e := s.cliTextJSON(context.Background(), &job, "analyze", "test", ep, &out); e != nil {
		t.Fatal("cached result requires reexecution", e)
	}
}
func TestUnknownCLIIsNotRunAgain(t *testing.T) {
	s := testStudio(t)
	job := cliTask(t, s)
	path := fakeCLI(t, "fail")
	ep := Endpoint{Transport: "cli", CLI: "codex", Executable: path}
	var out any
	for i := 0; i < 2; i++ {
		if e := s.cliTextJSON(context.Background(), &job, "analyze", "test", ep, &out); e == nil {
			t.Fatal("expected unknown result")
		}
	}
	b, e := os.ReadFile(filepath.Join(s.data, "cli", job.ID, "analyze", "runs"))
	if e != nil || string(b) != "run" {
		t.Fatal("CLI was retried", e, string(b))
	}
}
func TestClaudeEnvelopeAndSafeArguments(t *testing.T) {
	s := testStudio(t)
	job := cliTask(t, s)
	path := fakeCLI(t, "claude")
	ep := Endpoint{Transport: "cli", CLI: "claude", Executable: path}
	var out map[string]bool
	if e := s.cliTextJSON(context.Background(), &job, "analyze", "test", ep, &out); e != nil || !out["ok"] {
		t.Fatal(e)
	}
	args := strings.Join(cliArgs(ep, "dir", "out"), "|")
	if !strings.Contains(args, "--tools||") || !strings.Contains(args, "--safe-mode") || strings.Contains(args, "dangerously") {
		t.Fatal("unsafe CLI arguments", args)
	}
}

func TestCLIVendorOnlyExecutesAfterUnknownCostConsent(t *testing.T) {
	s := testStudio(t)
	p := createProject(t, s)
	path := fakeCLI(t, "analyze")
	cfg := Settings{Version: 1, RegistryVersion: 1, Mode: "remote", Vendors: []Vendor{{ID: "local", Name: "本机CLI", Type: "cli", CLI: "codex", Executable: path}}, Text: Endpoint{VendorID: "local"}}
	request(t, s, "PUT", "/api/settings", cfg, 200)
	request(t, s, "GET", "/api/settings", nil, 200)
	var q Quote
	_ = json.Unmarshal(request(t, s, "POST", "/api/projects/"+p.ID+"/quotes", map[string]any{"revision": p.Revision, "stage": "analyze"}, 200), &q)
	if !q.Unknown {
		t.Fatal("CLI assumed free")
	}
	request(t, s, "POST", "/api/quotes/"+q.ID+"/confirm", map[string]bool{"allow_unknown": false}, 400)
	jobs, e := s.store.tasks(p.ID)
	if e != nil || len(jobs) != 0 {
		t.Fatal("CLI started before consent", e)
	}
	var job Task
	_ = json.Unmarshal(request(t, s, "POST", "/api/quotes/"+q.ID+"/confirm", map[string]bool{"allow_unknown": true}, 200), &job)
	waitTask(t, s, job.ID)
	got := getProject(t, s, p.ID)
	if !got.Analyzed || !strings.Contains(got.Script, "主角看向窗外") {
		t.Fatal("CLI binding was not executed")
	}
}
