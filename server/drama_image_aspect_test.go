package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDramaImageQuoteBindsAspect(t *testing.T) {
	s := testStudio(t)
	w := dramaTestWork(t, s)
	for _, kind := range []string{"script", "assets", "storyboard"} {
		dramaTestSave(t, s, w, kind, dramaMock(kind, 5))
	}
	p, _, err := s.buildDramaProduction(w, DramaProduction{Kind: "image", Document: "storyboard", Entry: "SHOT-EP001-001", ImageAspect: "16:9"}, defaultSettings())
	if err != nil || p.ImageAspect != "9:16" || p.ImageMaxCalls != 2 || p.ImageRetryPrompt == "" || !strings.Contains(p.Prompt, "交付画幅必须为9:16") {
		t.Fatal("quote did not bind work aspect", p, err)
	}
	q := DramaQuote{Spec: p, Unknown: true}
	applyDramaImageCallScope(&q, Settings{Mode: "remote"})
	if !strings.Contains(q.Reason, "最多2次") || !q.Unknown {
		t.Fatal("maximum paid scope not disclosed", q)
	}
}

func TestDramaImageAspectRetryIsBoundedAndRecoverable(t *testing.T) {
	for _, scenario := range []string{"success", "both-wrong", "unknown-first", "unknown-retry"} {
		t.Run(scenario, func(t *testing.T) {
			s := testStudio(t)
			w := dramaTestWork(t, s)
			home := t.TempDir()
			t.Setenv("CODEX_HOME", home)
			if err := os.MkdirAll(filepath.Join(home, "generated_images"), 0700); err != nil {
				t.Fatal(err)
			}
			fixture := filepath.Join(t.TempDir(), "bad.png")
			retry := filepath.Join(t.TempDir(), "retry.png")
			if err := os.WriteFile(fixture, mockPNG("bad", true, false), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(retry, mockPNG("retry", scenario == "both-wrong", false), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("IMAGE_FIXTURE", fixture)
			t.Setenv("IMAGE_RETRY_FIXTURE", retry)
			cfg := Settings{Mode: "remote", RegistryVersion: 1, Vendors: []Vendor{{ID: "cli", Type: "cli", CLI: "codex", Executable: fakeCLI(t, "aspect-retry")}}, Image: Endpoint{VendorID: "cli"}}
			p := DramaProduction{Kind: "image", Document: "storyboard", Entry: "SHOT-EP001-001", Prompt: "cat", ImageAspect: "9:16", ImageMaxCalls: 2, ImageRetryPrompt: "严格9:16，只重生一次"}
			j := DramaJob{ID: uid(), WorkID: w.ID, Kind: "produce", Inputs: map[string]string{}, Production: &p}
			task, err := s.newDramaTask(w, cfg, j)
			if err != nil {
				t.Fatal(err)
			}
			key := "shot-drama-" + digest([]byte(p.Entry))[:16]
			if strings.HasPrefix(scenario, "unknown-") {
				unknown := key
				if scenario == "unknown-retry" {
					unknown += "-aspect-retry-1"
				}
				if _, err := s.store.db.Exec("INSERT INTO attempts(id,task_id,status,response) VALUES(?,?,'submitting',?)", task.ID+":"+unknown, task.ID, []byte(`{}`)); err != nil {
					t.Fatal(err)
				}
			}
			for run := 0; run < 2; run++ {
				err := s.executeDramaProduction(context.Background(), &task, &j)
				if scenario == "success" && err != nil || scenario != "success" && err == nil {
					t.Fatal("incorrect retry outcome", scenario, err)
				}
			}
			count := 0
			for _, k := range []string{key, key + "-aspect-retry-1"} {
				b, _ := os.ReadFile(filepath.Join(s.data, "cli", task.ID, k, "runs"))
				count += strings.Count(string(b), "run")
			}
			want := 2
			if scenario == "unknown-first" {
				want = 0
			} else if scenario == "unknown-retry" {
				want = 1
			}
			if count != want {
				t.Fatal("exceeded confirmed call budget or repeated unknown request", scenario, count)
			}
			media, err := s.dramaMedias(w.ID)
			wantMedia := 0
			if scenario == "success" {
				wantMedia = 1
			}
			if err != nil || len(media) != wantMedia {
				t.Fatal("invalid candidate published", len(media), err)
			}
		})
	}
}

func TestDramaImageRetryRecoveryRebuildsFrozenReferenceBoards(t *testing.T) {
	s := testStudio(t)
	w := dramaTestWork(t, s)
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "generated_images"), 0700); err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(t.TempDir(), "valid.png")
	if err := os.WriteFile(fixture, mockPNG("valid", false, false), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("IMAGE_FIXTURE", fixture)
	refs := []ReferenceSource{}
	for i := 0; i < 6; i++ {
		b := mockPNG(string(rune('a'+i)), false, false)
		file, err := s.writeMedia(w.ID, string(rune('a'+i))+".png", b)
		if err != nil {
			t.Fatal(err)
		}
		refs = append(refs, ReferenceSource{Image: Image{File: file, Hash: digest(b)}, Name: file, Kind: "character"})
	}
	plan, _, err := s.prepareReferenceSources(refs, 5)
	if err != nil {
		t.Fatal(err)
	}
	p := DramaProduction{Kind: "image", Document: "storyboard", Entry: "SHOT-EP001-001", Prompt: "cat", ImageAspect: "9:16", ImageMaxCalls: 2, ImageRetryPrompt: "严格9:16", ReferencePlan: &plan}
	j := DramaJob{ID: uid(), WorkID: w.ID, Kind: "produce", Inputs: map[string]string{}, Production: &p}
	cfg := Settings{Mode: "remote", RegistryVersion: 1, Vendors: []Vendor{{ID: "cli", Type: "cli", CLI: "codex", Executable: fakeCLI(t, "image")}}, Image: Endpoint{VendorID: "cli"}}
	task, err := s.newDramaTask(w, cfg, j)
	if err != nil {
		t.Fatal(err)
	}
	key := "shot-drama-" + digest([]byte(p.Entry))[:16]
	if _, err := s.store.db.Exec("INSERT INTO attempts(id,task_id,status,response) VALUES(?,?,'received',?)", task.ID+":"+key, task.ID, mockPNG("wrong", true, false)); err != nil {
		t.Fatal(err)
	}
	if err := s.executeDramaProduction(context.Background(), &task, &j); err != nil {
		t.Fatal("remaining retry did not reconstruct reference boards", err)
	}
	dir := filepath.Join(s.data, "cli", task.ID, key+"-aspect-retry-1")
	args, _ := os.ReadFile(filepath.Join(dir, "args.txt"))
	if n := strings.Count(string(args), "--image\n"); n < 1 || n > 5 {
		t.Fatal("retry lost the frozen reference plan", n)
	}
	// Once received, neither inference nor reference preparation repeats.
	if err := os.WriteFile(s.media(refs[0].Image.File), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.executeDramaProduction(context.Background(), &task, &j); err != nil {
		t.Fatal("existing receipt incorrectly required reference files", err)
	}
	runs, _ := os.ReadFile(filepath.Join(dir, "runs"))
	if string(runs) != "run" {
		t.Fatal("retry was submitted twice")
	}
}

func TestDramaImageAspectRejectsBeforePublishingAndDoesNotRepeatCLI(t *testing.T) {
	for _, valid := range []bool{false, true} {
		t.Run(map[bool]string{false: "wrong", true: "correct"}[valid], func(t *testing.T) {
			s := testStudio(t)
			w := dramaTestWork(t, s)
			home := t.TempDir()
			t.Setenv("CODEX_HOME", home)
			if err := os.MkdirAll(filepath.Join(home, "generated_images"), 0700); err != nil {
				t.Fatal(err)
			}
			fixture := filepath.Join(t.TempDir(), "fixture.png")
			if err := os.WriteFile(fixture, mockPNG("generated", !valid, false), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("IMAGE_FIXTURE", fixture)
			cfg := Settings{Mode: "remote", RegistryVersion: 1, Vendors: []Vendor{{ID: "local", Type: "cli", CLI: "codex", Executable: fakeCLI(t, "image")}}, Image: Endpoint{VendorID: "local"}}
			p := DramaProduction{Kind: "image", Document: "storyboard", Entry: "SHOT-EP001-001", Prompt: "cat", ImageAspect: "9:16"}
			j := DramaJob{ID: uid(), WorkID: w.ID, Kind: "produce", Inputs: map[string]string{}, Production: &p}
			task, err := s.newDramaTask(w, cfg, j)
			if err != nil {
				t.Fatal(err)
			}
			source := guideReference(t, s, w.ID, p.Entry)
			for run := 0; run < 2; run++ {
				err = s.executeDramaProduction(context.Background(), &task, &j)
				if valid && err != nil || !valid && (err == nil || !strings.Contains(err.Error(), "画幅验收失败")) {
					t.Fatal("incorrect aspect acceptance", err)
				}
			}
			key := "shot-drama-" + digest([]byte(p.Entry))[:16]
			dir := filepath.Join(s.data, "cli", task.ID, key)
			runs, err := os.ReadFile(filepath.Join(dir, "runs"))
			if err != nil || string(runs) != "run" {
				t.Fatal("recovery resubmitted image", string(runs), err)
			}
			prompt, _ := os.ReadFile(filepath.Join(dir, "prompt.txt"))
			if !strings.Contains(string(prompt), "严格9:16") || strings.Contains(string(prompt), "建议1024x1536") {
				t.Fatal("conflicting CLI aspect instructions")
			}
			all, err := s.dramaMedias(w.ID)
			want := 1
			if valid {
				want = 2
			}
			if err != nil || len(all) != want {
				t.Fatal("invalid image was published", len(all), err)
			}
			original, err := s.dramaMediaByID(w.ID, source.ID)
			if err != nil || !original.Approved || original.Hash != source.Hash || s.verifyDramaMedia(original) != nil {
				t.Fatal("source image was changed", err)
			}
			if !valid {
				// Existing confirmed snapshots without the new policy retain their
				// original recovery behavior and reuse the same receipt.
				p.ImageAspect = ""
				if err := s.executeDramaProduction(context.Background(), &task, &j); err != nil {
					t.Fatal("legacy snapshot semantics changed", err)
				}
			}
		})
	}
}
