package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCodexImageRoutesReferencesAndReusesArtifact(t *testing.T) {
	for _, tc := range []struct {
		name, key        string
		sheet, landscape bool
	}{
		{"master", "master-one", false, false}, {"sheet", "sheet-one", true, false}, {"board", "shot-one", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := testStudio(t)
			job := cliTask(t, s)
			job.Mode = "remote"
			if tc.landscape {
				job.Input.AspectRatio = "16:9"
			}
			home := t.TempDir()
			t.Setenv("CODEX_HOME", home)
			root := filepath.Join(home, "generated_images")
			if err := os.MkdirAll(root, 0700); err != nil {
				t.Fatal(err)
			}
			fixture := filepath.Join(t.TempDir(), "fixture.png")
			if err := os.WriteFile(fixture, mockPNG("fixture", tc.sheet, tc.landscape), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("IMAGE_FIXTURE", fixture)
			path := fakeCLI(t, "image")
			job.Config = Settings{RegistryVersion: 1, Mode: "remote", Vendors: []Vendor{{ID: "local", Name: "Codex", Type: "cli", CLI: "codex", Executable: path}}, Image: Endpoint{VendorID: "local"}}
			file, err := s.writeMedia(job.ProjectID, "ref.png", mockPNG("reference", false, false))
			if err != nil {
				t.Fatal(err)
			}
			refBytes, _ := os.ReadFile(s.media(file))
			ref := Image{File: file, Hash: digest(refBytes)}
			im, err := s.makeImage(context.Background(), &job, tc.key, "角色身份保持一致", []Image{ref}, tc.sheet)
			if err != nil || im.Hash == "" {
				t.Fatal("image route failed", err)
			}
			dir := filepath.Join(s.data, "cli", job.ID, tc.key)
			args, _ := os.ReadFile(filepath.Join(dir, "args.txt"))
			for _, flag := range []string{"workspace-write", "--image", "reference-1.png", "--output-schema"} {
				if !strings.Contains(string(args), flag) {
					t.Fatal("missing image argument", flag, string(args))
				}
			}
			if strings.Contains(string(args), "dangerously") {
				t.Fatal("sandbox bypass")
			}
			prompt, _ := os.ReadFile(filepath.Join(dir, "prompt.txt"))
			if !strings.Contains(string(prompt), "$imagegen") || !strings.Contains(string(prompt), "一次生图") {
				t.Fatal("missing imagegen contract")
			}
			job.Config.Vendors[0].Executable = "/no/such/cli"
			if err := os.Remove(filepath.Join(root, "current.png")); err != nil {
				t.Fatal(err)
			}
			again, err := s.makeImage(context.Background(), &job, tc.key, "same", []Image{ref}, tc.sheet)
			if err != nil || again.Hash != im.Hash {
				t.Fatal("saved image not reused", err)
			}
			runs, _ := os.ReadFile(filepath.Join(dir, "runs"))
			if string(runs) != "run" {
				t.Fatal("image CLI repeated")
			}
		})
	}
}

func TestUnknownCodexImageDoesNotExecuteAgain(t *testing.T) {
	s := testStudio(t)
	job := cliTask(t, s)
	t.Setenv("CODEX_HOME", t.TempDir())
	ep := Endpoint{CLI: "codex", Executable: fakeCLI(t, "fail")}
	for i := 0; i < 2; i++ {
		if _, err := s.cliImage(context.Background(), &job, "master-one", "test", nil, false, ep); err == nil {
			t.Fatal("unknown image accepted")
		}
	}
	runs, _ := os.ReadFile(filepath.Join(s.data, "cli", job.ID, "master-one", "runs"))
	if string(runs) != "run" {
		t.Fatal("unknown image request was resubmitted")
	}
}

func TestCodexImageQuoteRequiresUnknownCostConsent(t *testing.T) {
	s := testStudio(t)
	p := createProject(t, s)
	p.Analyzed, p.Mode = true, "remote"
	p.Assets = []Asset{{ID: "one", Kind: "character"}}
	cfg := Settings{Version: 1, RegistryVersion: 1, Mode: "remote", Vendors: []Vendor{{ID: "local", Name: "Codex", Type: "cli", CLI: "codex", Executable: fakeCLI(t, "fail")}}, Image: Endpoint{VendorID: "local"}}
	q, err := s.quoteFor(p, cfg, "masters")
	if err != nil || !q.Unknown || len(q.Items) != 1 || q.Items[0].Count != 1 {
		t.Fatal("CLI image assumed free or quote changed", q, err)
	}
}

func TestCodexImageRejectsChangedReferenceBeforeSubmission(t *testing.T) {
	s := testStudio(t)
	job := cliTask(t, s)
	t.Setenv("CODEX_HOME", t.TempDir())
	ep := Endpoint{CLI: "codex", Executable: fakeCLI(t, "fail")}
	file, err := s.writeMedia(job.ProjectID, "reference.png", mockPNG("ref", false, false))
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.cliImage(context.Background(), &job, "master-one", "", []Image{{File: file, Hash: "changed"}}, false, ep)
	if err == nil {
		t.Fatal("modified reference accepted")
	}
	var count int
	if err = s.store.db.QueryRow("SELECT COUNT(*) FROM attempts WHERE task_id=?", job.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("invalid reference submitted", count, err)
	}
}

func TestCLIImageReceiptRejectsUnsafeAndInvalidArtifacts(t *testing.T) {
	root := t.TempDir()
	intent := cliImageIntent{Root: root, Started: time.Now().Add(-time.Second)}
	good := filepath.Join(root, "good.png")
	if err := os.WriteFile(good, mockPNG("test", true, false), 0600); err != nil {
		t.Fatal(err)
	}
	receipt := filepath.Join(t.TempDir(), "receipt.json")
	collect := func(source string, sheet bool) error {
		t.Helper()
		if err := os.WriteFile(receipt, jsonBytes(cliImageReceipt{Source: source}), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := collectCLIImage(receipt, intent, Project{}, "sheet-one", sheet)
		return err
	}
	if err := collect(good, true); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.png")
	linkCreated := os.Symlink(good, link) == nil
	outside := filepath.Join(t.TempDir(), "outside.png")
	if err := os.WriteFile(outside, mockPNG("test", true, false), 0600); err != nil {
		t.Fatal(err)
	}
	invalid := filepath.Join(root, "invalid.png")
	if err := os.WriteFile(invalid, []byte("not an image"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{outside, invalid, "relative.png", root, ""} {
		if err := collect(source, true); err == nil {
			t.Fatal("invalid artifact accepted", source)
		}
	}
	if linkCreated && collect(link, true) == nil {
		t.Fatal("symlink accepted")
	}
	if err := collect(good, false); err == nil {
		t.Fatal("wrong orientation accepted")
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(good, old, old); err != nil {
		t.Fatal(err)
	}
	if err := collect(good, true); err == nil {
		t.Fatal("old image accepted")
	}
}

func TestCLIImageRecoveryCollectsReceiptWithoutExecuting(t *testing.T) {
	s := testStudio(t)
	job := cliTask(t, s)
	root := t.TempDir()
	intent := cliImageIntent{Root: root, Started: time.Now().Add(-time.Second)}
	dir := filepath.Join(s.data, "cli", job.ID, "sheet-one")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "native.png")
	if err := os.WriteFile(source, mockPNG("native", true, false), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "image-receipt.json"), jsonBytes(cliImageReceipt{Source: source}), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := s.store.db.Exec("INSERT INTO attempts(id,task_id,status,response) VALUES(?,?,'submitting',?)", job.ID+":sheet-one", job.ID, jsonBytes(intent))
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.cliImage(context.Background(), &job, "sheet-one", "", nil, true, Endpoint{CLI: "codex", Executable: "/missing"})
	if err != nil || len(b) == 0 {
		t.Fatal("recovery failed", err)
	}
	var state string
	var saved []byte
	if err := s.store.db.QueryRow("SELECT status,response FROM attempts WHERE id=?", job.ID+":sheet-one").Scan(&state, &saved); err != nil || state != "received" || json.Valid(saved) {
		t.Fatal("image receipt not durably collected", err)
	}
}
