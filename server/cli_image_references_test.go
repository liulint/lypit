package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSixCLIReferencesKeepAllPixelsAndUseOnlyFivePaths(t *testing.T) {
	s := testStudio(t)
	job := cliTask(t, s)
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "generated_images"), 0700); err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(t.TempDir(), "result.png")
	if err := os.WriteFile(fixture, mockPNG("result", false, false), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("IMAGE_FIXTURE", fixture)
	refs := []Image{}
	originals := [][]byte{}
	for i := 0; i < 6; i++ {
		b := mockPNG(string(rune('a'+i)), false, false)
		originals = append(originals, b)
		file, err := s.writeMedia(job.ProjectID, string(rune('a'+i))+".png", b)
		if err != nil {
			t.Fatal(err)
		}
		refs = append(refs, Image{File: file, Hash: digest(b)})
	}
	if _, err := s.cliImage(context.Background(), &job, "shot-six", "same assets", refs, false, Endpoint{CLI: "codex", Executable: fakeCLI(t, "image")}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(s.data, "cli", job.ID, "shot-six")
	args, _ := os.ReadFile(filepath.Join(dir, "args.txt"))
	count := strings.Count(string(args), "--image\n")
	if count < 1 || count > 5 {
		t.Fatal("tool reference limit exceeded", count)
	}
	plan, _, err := s.prepareReferenceBoards(refs, job.Input, "shot-six")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int]bool{}
	for i, board := range plan.Boards {
		b, _ := os.ReadFile(filepath.Join(dir, fmt.Sprintf("reference-%d.png", i+1)))
		combined, _, err := image.Decode(bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		for _, cell := range board.Cells {
			if seen[cell.Source] {
				t.Fatal("duplicate reference")
			}
			seen[cell.Source] = true
			src, _, _ := image.Decode(bytes.NewReader(originals[cell.Source]))
			for y := 0; y < src.Bounds().Dy(); y++ {
				for x := 0; x < src.Bounds().Dx(); x++ {
					r, g, b, a := src.At(x, y).RGBA()
					rr, gg, bb, aa := combined.At(cell.X+x, cell.Y+y).RGBA()
					if r != rr || g != gg || b != bb || a != aa {
						t.Fatal("reference pixels lost", cell.Source, x, y)
					}
				}
			}
		}
	}
	if len(seen) != 6 {
		t.Fatal("reference omitted")
	}
	prompt, _ := os.ReadFile(filepath.Join(dir, "prompt.txt"))
	if !strings.Contains(string(prompt), "原始参考共6份，全部保留") || !strings.Contains(string(prompt), "最多5条") {
		t.Fatal("missing reference mapping")
	}
	for i, ref := range refs {
		b, _ := os.ReadFile(s.media(ref.File))
		if !bytes.Equal(b, originals[i]) {
			t.Fatal("original asset overwritten")
		}
	}
}

func TestCLIReferenceLimitErrorIsActionableAndDoesNotRerun(t *testing.T) {
	s := testStudio(t)
	job := cliTask(t, s)
	t.Setenv("CODEX_HOME", t.TempDir())
	job.Stage = "board_retry"
	job.ShotID = "one"
	ep := Endpoint{CLI: "codex", Executable: fakeCLI(t, "reference-limit")}
	for i := 0; i < 2; i++ {
		_, err := s.cliImage(context.Background(), &job, "shot-one-retry", "", nil, false, ep)
		if err == nil || !strings.Contains(err.Error(), "最多5张参考图") {
			t.Fatal("specific tool failure hidden", err)
		}
	}
	b, _ := os.ReadFile(filepath.Join(s.data, "cli", job.ID, "shot-one-retry", "runs"))
	if string(b) != "run" {
		t.Fatal("failed inference was repeated")
	}
}

func TestFailedTaskShowsAllowlistedDiagnosticWithoutChangingStoredTask(t *testing.T) {
	s := testStudio(t)
	job := cliTask(t, s)
	job.Stage = "board_retry"
	job.ShotID = "one"
	job.Status = "failed"
	job.Message = "generic empty receipt"
	dir := filepath.Join(s.data, "cli", job.ID, "shot-one-retry")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	log := map[string]string{"stderr": "secret raw content\n`referenced_image_paths` must contain at most 5 paths"}
	if err := os.WriteFile(filepath.Join(dir, "cli-execution.json"), jsonBytes(log), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.store.saveTask(job); err != nil {
		t.Fatal(err)
	}
	body := request(t, s, "GET", "/api/tasks", nil, 200)
	if !strings.Contains(string(body), "最多5张参考图") || strings.Contains(string(body), "secret raw content") {
		t.Fatal("unsafe or unhelpful diagnostic", string(body))
	}
	var tasks []Task
	if err := json.Unmarshal(body, &tasks); err != nil {
		t.Fatal(err)
	}
	saved, err := s.store.task(job.ID)
	if err != nil || saved.Message != job.Message {
		t.Fatal("read-only diagnostic changed task", err)
	}
}
