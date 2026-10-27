package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func modelAPI(t *testing.T, s *Studio, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.router().ServeHTTP(w, req)
	return w
}
func remoteTestProvider() Provider {
	return Provider{ID: "remote", Name: "Remote", Adapter: "openai", BaseURL: "https://example.invalid/v1", CredentialSource: "file", Models: []Model{{ID: "text", Capabilities: []string{"text"}, Structured: true, Sizes: []string{}}}}
}
func TestCredentialLifecycleAndRedaction(t *testing.T) {
	s := testStudio(t)
	p := remoteTestProvider()
	if err := s.store.change(func(st *State) error { st.Settings.Providers = append(st.Settings.Providers, p); return nil }); err != nil {
		t.Fatal(err)
	}
	secret := "private-test-key-should-never-leak"
	w := modelAPI(t, s, "PUT", "/api/providers/remote/credential", map[string]string{"key": secret, "version": ""})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	current, _ := providerByID(s.store.settings(), p.ID)
	got, err := s.providerKey(current)
	if err != nil || got != secret {
		t.Fatal("credential did not roundtrip", err)
	}
	stBefore := s.store.settings()
	q := createProject(t, s, "credential snapshot")
	quote, err := s.makeQuote(q.ID, "asset_plan", "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/settings", "/api/projects/" + q.ID, "/api/projects/" + q.ID + "/export"} {
		w := modelAPI(t, s, "GET", path, nil)
		if bytes.Contains(w.Body.Bytes(), []byte(secret)) {
			t.Fatalf("secret leaked via %s", path)
		}
	}
	var stored string
	if err = s.store.db.QueryRow("SELECT body FROM state WHERE id=1").Scan(&stored); err != nil || strings.Contains(stored, secret) {
		t.Fatal("plaintext in database", err)
	}
	raw, _ := os.ReadFile(filepath.Join(s.data, "secrets", current.ID+"-"+current.CredentialVersion+".enc"))
	if bytes.Contains(raw, []byte(secret)) {
		t.Fatal("plaintext on disk")
	}
	for _, file := range []string{"master.key", current.ID + "-" + current.CredentialVersion + ".enc"} {
		info, e := os.Stat(filepath.Join(s.data, "secrets", file))
		if e != nil || info.Mode().Perm() != 0600 {
			t.Fatal("credential permissions", e)
		}
	}
	w = modelAPI(t, s, "PUT", "/api/providers/remote/credential", map[string]string{"key": "replacement-key", "version": current.CredentialVersion})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if hash(stBefore) == hash(s.store.settings()) {
		t.Fatal("credential did not invalidate settings hash")
	}
	if err = s.confirm(q.ID, quote.ID, true); err == nil {
		t.Fatal("old quote accepted")
	}
	if got, err = s.providerKey(current); err != nil || got != secret {
		t.Fatal("historical credential version lost")
	}
	// Stale settings cannot restore a superseded credential reference.
	w = modelAPI(t, s, "PUT", "/api/settings", stBefore)
	if w.Code != 400 {
		t.Fatal("stale credential write accepted")
	}
	replacement, _ := providerByID(s.store.settings(), p.ID)
	// Failed durable config commit must leave the previous active version untouched.
	if _, err = s.store.db.Exec("CREATE TRIGGER fail_state BEFORE UPDATE ON state BEGIN SELECT RAISE(FAIL,'test'); END"); err != nil {
		t.Fatal(err)
	}
	w = modelAPI(t, s, "PUT", "/api/providers/remote/credential", map[string]string{"key": "third-key", "version": replacement.CredentialVersion})
	if w.Code != 400 {
		t.Fatal("failed commit accepted")
	}
	active, _ := providerByID(s.store.settings(), p.ID)
	if active.CredentialVersion != replacement.CredentialVersion {
		t.Fatal("failed write replaced active key")
	}
	if _, err = s.store.db.Exec("DROP TRIGGER fail_state"); err != nil {
		t.Fatal(err)
	}
	// Tampering and loss of the master key fail closed, with no environment fallback.
	path := filepath.Join(s.data, "secrets", replacement.ID+"-"+replacement.CredentialVersion+".enc")
	if err = os.WriteFile(path, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.providerKey(replacement); err == nil {
		t.Fatal("corrupt ciphertext accepted")
	}
	if err = os.Rename(filepath.Join(s.data, "secrets", "master.key"), filepath.Join(s.data, "secrets", "saved-master.key")); err != nil {
		t.Fatal(err)
	}
	if _, err = s.writeCredential(p.ID, "another-key"); err == nil {
		t.Fatal("missing master key silently replaced")
	}
}
func createProject(t *testing.T, s *Studio, idea string) Project {
	t.Helper()
	w := modelAPI(t, s, "POST", "/api/projects", map[string]string{"idea": idea})
	var p Project
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &p) != nil {
		t.Fatal(w.Body.String())
	}
	return p
}
func fixturePNG(t *testing.T, path string, w, h int) {
	t.Helper()
	im := image.NewRGBA(image.Rect(0, 0, w, h))
	im.Set(0, 0, color.RGBA{R: 80, G: 150, B: 180, A: 255})
	f, e := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	if e = png.Encode(f, im); e != nil {
		t.Fatal(e)
	}
	if e = f.Close(); e != nil {
		t.Fatal(e)
	}
}
func fakeCLI(t *testing.T, kind, task string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), kind)
	script := "#!/bin/sh\ncase \"$1\" in\n--version) echo 'test-cli 1.0'; exit 0;;\nlogin|auth) echo '{}'; exit 0;;\nesac\ncat >/dev/null\n" + task + "\n"
	if e := os.WriteFile(path, []byte(script), 0700); e != nil {
		t.Fatal(e)
	}
	return path
}
func cliTestProvider(kind, path string) Provider {
	return Provider{ID: "cli-" + kind, Name: kind, Adapter: "cli", CLI: &CLIConfig{Kind: kind, Path: path, ImageInstructions: "test image tool", TimeoutSeconds: 2}, Models: []Model{{ID: "test", Capabilities: []string{"text", "image"}, Structured: true, Sizes: []string{"8x8"}}}}
}
func TestCLIAdaptersAndSavedResponses(t *testing.T) {
	for _, kind := range []string{"codex", "claude", "gemini"} {
		t.Run(kind, func(t *testing.T) {
			response := `{"result":"{\"hello\":\"world\"}"}`
			if kind == "gemini" {
				response = `{"response":"{\"hello\":\"world\"}"}`
			}
			task := "printf '%s' '" + response + "'"
			if kind == "codex" {
				task = "printf '%s' '{\"hello\":\"world\"}' >answer.json"
			}
			bin := fakeCLI(t, kind, task)
			s := testStudio(t)
			project := createProject(t, s, "cli adapter")
			p := cliTestProvider(kind, bin)
			settings := s.store.settings()
			settings.Providers = append(settings.Providers, p)
			settings.Bindings["text"] = Binding{p.ID, "test"}
			path, e := s.request(context.Background(), project.ID, settings, "text", "test", nil, 0)
			if e != nil {
				t.Fatal(e)
			}
			b, _ := os.ReadFile(s.abs(path))
			if !json.Valid(b) || !bytes.Contains(b, []byte("world")) {
				t.Fatal("invalid normalized CLI output")
			}
			if e = os.WriteFile(bin, []byte("#!/bin/sh\nexit 99\n"), 0700); e != nil {
				t.Fatal(e)
			}
			if _, e = s.request(context.Background(), project.ID, settings, "text", "test", nil, 0); e != nil {
				t.Fatal("successful request relaunched", e)
			}
		})
	}
	// A stored response can be parsed again without executing the CLI.
	s := testStudio(t)
	p := cliTestProvider("claude", "/does/not/exist")
	dir := t.TempDir()
	if e := atomicWrite(filepath.Join(dir, "response.bin"), []byte(`{"result":"{\"ok\":true}"}`)); e != nil {
		t.Fatal(e)
	}
	if _, e := s.executeCLI(context.Background(), p, p.Models[0], "text", "", nil, dir, true); e != nil {
		t.Fatal(e)
	}
}
func TestCLIUnknownDoesNotRelaunchAndCancellation(t *testing.T) {
	s := testStudio(t)
	project := createProject(t, s, "unknown cli")
	marker := filepath.Join(t.TempDir(), "calls")
	bin := fakeCLI(t, "claude", "echo call >> '"+marker+"'\nexit 1")
	p := cliTestProvider("claude", bin)
	settings := s.store.settings()
	settings.Providers = append(settings.Providers, p)
	settings.Bindings["text"] = Binding{p.ID, "test"}
	for i := 0; i < 2; i++ {
		if _, e := s.request(context.Background(), project.ID, settings, "text", "test", nil, 0); e == nil {
			t.Fatal("failed request accepted")
		}
	}
	b, _ := os.ReadFile(marker)
	if strings.Count(string(b), "call") != 1 {
		t.Fatal("unknown request resubmitted")
	}
	bin = fakeCLI(t, "claude", "sleep 30 &\nwait")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, e := runProcess(ctx, bin, t.TempDir(), nil, nil); e == nil {
		t.Fatal("expected cancellation")
	}
	if time.Since(start) > 4*time.Second {
		t.Fatal("cancel did not terminate child group")
	}
}
func TestCLIImageValidationAndQuotedVerification(t *testing.T) {
	s := testStudio(t)
	source := filepath.Join(t.TempDir(), "source.png")
	fixturePNG(t, source, 8, 8)
	bin := fakeCLI(t, "claude", "cp '"+source+"' output.png\nprintf '%s' '{\"result\":\"{\\\"file\\\":\\\"output.png\\\"}\"}'")
	p := cliTestProvider("claude", bin)
	if e := s.store.change(func(st *State) error { st.Settings.Providers = append(st.Settings.Providers, p); return nil }); e != nil {
		t.Fatal(e)
	}
	settings := s.store.settings()
	settings.Bindings["image"] = Binding{p.ID, "test"}
	if validateSettings(settings) == nil {
		t.Fatal("unverified CLI bound to image")
	}
	probe, e := s.quoteImageProbe(p.ID, "test")
	if e != nil {
		t.Fatal(e)
	}
	if probe.Quote.Estimate != nil {
		t.Fatal("CLI unknown cost fabricated")
	}
	w := modelAPI(t, s, "POST", "/api/image-verifications/"+probe.ID+"/confirm", map[string]bool{"allow_unknown": false})
	if w.Code != 400 {
		t.Fatal("unknown cost not gated")
	}
	started, e := s.startImageProbe(probe.ID)
	if e != nil {
		t.Fatal(e)
	}
	s.launchImageProbe(started, false)
	s.wg.Wait()
	s.store.mu.Lock()
	result := s.store.state.Probes[probe.ID]
	s.store.mu.Unlock()
	if result.State != "succeeded" {
		t.Fatal(result.Error)
	}
	current, _ := providerByID(s.store.settings(), p.ID)
	if !cliImageReady(current, p.Models[0]) {
		t.Fatal("verified image not enabled")
	}
	current.CLI.ImageInstructions = "different tool"
	if cliImageReady(current, p.Models[0]) {
		t.Fatal("changed skill retained verification")
	}
	if _, e = s.startImageProbe(probe.ID); e == nil {
		t.Fatal("verification submitted twice")
	}
	dir := t.TempDir()
	fixturePNG(t, filepath.Join(dir, "valid.png"), 8, 8)
	if _, _, e = cliImageOutput(dir, []byte(`{"file":"valid.png"}`), p.Models[0]); e != nil {
		t.Fatal(e)
	}
	if e = os.Symlink(source, filepath.Join(dir, "escape.png")); e != nil {
		t.Fatal(e)
	}
	for _, file := range []string{"../outside.png", "escape.png", "missing.png"} {
		raw, _ := json.Marshal(map[string]string{"file": file})
		if _, _, e = cliImageOutput(dir, raw, p.Models[0]); e == nil {
			t.Fatalf("accepted %s", file)
		}
	}
	m := p.Models[0]
	m.Sizes = []string{"9x9"}
	if _, _, e = cliImageOutput(dir, []byte(`{"file":"valid.png"}`), m); e == nil {
		t.Fatal("wrong image size accepted")
	}
	forged := s.store.settings()
	for i := range forged.Providers {
		if forged.Providers[i].ID == p.ID {
			forged.Providers[i].CLI.ImageInstructions = "forged"
			forged.Providers[i].ImageVerified["test"] = cliImageFingerprint(forged.Providers[i], forged.Providers[i].Models[0])
		}
	}
	forged.Bindings["image"] = Binding{p.ID, "test"}
	w = modelAPI(t, s, "PUT", "/api/settings", forged)
	if w.Code != 400 {
		t.Fatal("client forged image verification")
	}
}
func TestNativeProviderRequestsAndRecovery(t *testing.T) {
	for _, adapter := range []string{"openai", "anthropic", "gemini"} {
		t.Run(adapter, func(t *testing.T) {
			s := testStudio(t)
			project := createProject(t, s, "native")
			ref := filepath.Join(project.ID, "reference.png")
			if e := os.MkdirAll(filepath.Dir(s.abs(ref)), 0700); e != nil {
				t.Fatal(e)
			}
			fixturePNG(t, s.abs(ref), 8, 8)
			var calls atomic.Int32
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var body map[string]any
				if json.NewDecoder(r.Body).Decode(&body) != nil {
					t.Error("invalid request")
				}
				encoded, _ := json.Marshal(body)
				if !bytes.Contains(encoded, []byte("image")) && !bytes.Contains(encoded, []byte("inline_data")) {
					t.Error("missing references")
				}
				switch adapter {
				case "anthropic":
					if r.URL.Path != "/messages" || r.Header.Get("x-api-key") != "test" || r.Header.Get("anthropic-version") == "" {
						t.Error("wrong anthropic auth/path")
					}
					fmt.Fprint(w, `{"content":[{"type":"text","text":"{\"ok\":true}"}],"usage":{"input_tokens":12}}`)
				case "gemini":
					if r.URL.Path != "/models/test:generateContent" || r.Header.Get("x-goog-api-key") != "test" || r.URL.RawQuery != "" {
						t.Error("wrong gemini auth/path")
					}
					fmt.Fprint(w, `{"candidates":[{"content":{"parts":[{"text":"{\"ok\":true}"}]}}],"usageMetadata":{"promptTokenCount":12}}`)
				default:
					if r.Header.Get("Authorization") != "Bearer test" {
						t.Error("wrong openai auth")
					}
					fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"ok\":true}"}}],"usage":{"prompt_tokens":12}}`)
				}
			}))
			defer endpoint.Close()
			t.Setenv("NATIVE_TEST_KEY", "test")
			settings := s.store.settings()
			p := remoteTestProvider()
			p.Adapter = adapter
			p.BaseURL = endpoint.URL
			p.CredentialSource = "env"
			p.KeyEnv = "NATIVE_TEST_KEY"
			p.Models[0].ID = "test"
			p.Models[0].Capabilities = []string{"vision"}
			p.Models[0].ReferenceImages = true
			p.Models[0].MaxReferences = 8
			settings.Providers = append(settings.Providers, p)
			settings.Bindings["vision"] = Binding{p.ID, "test"}
			path, e := s.request(context.Background(), project.ID, settings, "vision", "review", []string{ref}, 0)
			if e != nil {
				t.Fatal(e)
			}
			b, _ := os.ReadFile(s.abs(path))
			if string(b) != `{"ok":true}` {
				t.Fatal(string(b))
			}
			snapshot, _ := s.store.project(project.ID)
			for key, a := range snapshot.Attempts {
				a.State = "response_saved"
				a.File = ""
				if e = s.store.update(project.ID, func(p *Project) { p.Attempts[key] = a }); e != nil {
					t.Fatal(e)
				}
			}
			if _, e = s.request(context.Background(), project.ID, settings, "vision", "review", []string{ref}, 0); e != nil {
				t.Fatal(e)
			}
			if calls.Load() != 1 {
				t.Fatal("saved native response regenerated")
			}
			snapshot, _ = s.store.project(project.ID)
			for key, a := range snapshot.Attempts {
				a.State = "response_saved"
				a.File = ""
				if e = s.store.update(project.ID, func(p *Project) { p.Attempts[key] = a }); e != nil {
					t.Fatal(e)
				}
				response := filepath.Join(s.data, "media", project.ID, key, "response.bin")
				if e = os.Rename(response, response+".saved"); e != nil {
					t.Fatal(e)
				}
				if adapter != "openai" {
					if e = os.Rename(response+".native", response+".native-saved"); e != nil {
						t.Fatal(e)
					}
				}
			}
			if _, e = s.request(context.Background(), project.ID, settings, "vision", "review", []string{ref}, 0); e == nil {
				t.Fatal("missing saved response should fail")
			}
			if calls.Load() != 1 {
				t.Fatal("missing saved response triggered a paid resubmission")
			}

		})
	}
}

func TestCLIStatusAndSkillChanges(t *testing.T) {
	bin := fakeCLI(t, "codex", "exit 0")
	c := &CLIConfig{Kind: "codex", Path: bin}
	if status := detectCLI(context.Background(), c); !status.Installed || status.Auth != "ready" {
		t.Fatalf("status %+v", status)
	}
	missing := &CLIConfig{Kind: "gemini", Path: filepath.Join(t.TempDir(), "missing")}
	if status := detectCLI(context.Background(), missing); status.Installed {
		t.Fatal("missing CLI claimed installed")
	}
	if _, e := cliExecutable(&CLIConfig{Kind: "claude", Path: "relative/path"}); e == nil {
		t.Fatal("relative CLI path accepted")
	}
	notLogged := filepath.Join(t.TempDir(), "claude")
	if e := os.WriteFile(notLogged, []byte("#!/bin/sh\nif [ \"$1\" = '--version' ]; then echo test; exit 0; fi\nprintf '%s' '{\"loggedIn\":false}'\n"), 0700); e != nil {
		t.Fatal(e)
	}
	if status := detectCLI(context.Background(), &CLIConfig{Kind: "claude", Path: notLogged}); status.Auth != "unavailable" {
		t.Fatal("false auth status accepted")
	}
	skill := filepath.Join(t.TempDir(), "SKILL.md")
	if e := os.WriteFile(skill, []byte("image tool"), 0600); e != nil {
		t.Fatal(e)
	}
	_, digest, e := readCLISkill(skill)
	if e != nil {
		t.Fatal(e)
	}
	c.SkillPath = skill
	c.SkillDigest = digest
	if e = validateCLISkill(c); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(skill, []byte("different tool"), 0600); e != nil {
		t.Fatal(e)
	}
	if validateCLISkill(c) == nil {
		t.Fatal("changed Skill accepted")
	}
	if _, _, e = readCLISkill(filepath.Dir(skill)); e == nil {
		t.Fatal("directory accepted as Skill")
	}
	args, e := cliTaskArgs(*c, "default", t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	for _, a := range args {
		if a == "--model" {
			t.Fatal("default alias passed as real model ID")
		}
	}
}
func TestRemoteModelCatalog(t *testing.T) {
	for _, adapter := range []string{"openai", "anthropic", "gemini"} {
		t.Run(adapter, func(t *testing.T) {
			s := testStudio(t)
			var calls atomic.Int32
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != "GET" || r.URL.Path != "/models" {
					t.Error("model detection generated content")
				}
				if adapter == "gemini" {
					if r.Header.Get("x-goog-api-key") != "catalog-key" {
						t.Error("missing auth")
					}
					fmt.Fprint(w, `{"models":[{"name":"models/a"}]}`)
				} else {
					fmt.Fprint(w, `{"data":[{"id":"a"}]}`)
				}
			}))
			defer endpoint.Close()
			p := remoteTestProvider()
			p.Adapter = adapter
			p.BaseURL = endpoint.URL
			version, e := s.writeCredential(p.ID, "catalog-key")
			if e != nil {
				t.Fatal(e)
			}
			p.CredentialVersion = version
			models, e := s.providerModels(context.Background(), p)
			if e != nil || len(models) != 1 || models[0] != "a" || calls.Load() != 1 {
				t.Fatalf("catalog %v %v", models, e)
			}
		})
	}
}
