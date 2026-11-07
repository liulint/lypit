package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func TestFourViewQuotePayloadAndLegacySnapshots(t *testing.T) {
	s := testStudio(t)
	p := readyRetryProject(t, s)
	q := p.Shots[0]
	var character Asset
	for _, a := range p.Assets {
		if a.Kind == "character" {
			character = a
			break
		}
	}
	q.References = []string{character.ID}
	p.Shots[0] = q
	refs, err := s.fourViewBoardReferences(p, q)
	if err != nil || len(refs) != 2 || refs[1].File != character.Sheet.File {
		t.Fatal("missing sheet", refs, err)
	}
	old := Task{Input: p, ReferencePolicyVersion: referencePlanVersion}
	legacy, err := s.taskBoardReferences(old, q)
	if err != nil || len(legacy) != 1 || legacy[0].File != character.Master.File {
		t.Fatal("legacy snapshot changed", err)
	}
	old.ReferencePolicyVersion = 0
	legacy, err = s.taskBoardReferences(old, q)
	if err != nil || len(legacy) != 1 {
		t.Fatal("unversioned snapshot changed", err)
	}
	// New project contents must not enter a restored task.
	restored := old
	restored.Input, err = decodeBody[Project](jsonBytes(old.Input))
	if err != nil {
		t.Fatal(err)
	}
	p.Assets[0].Name = "changed outside task"
	legacy, err = s.taskBoardReferences(restored, q)
	if err != nil || len(legacy) != 1 {
		t.Fatal("restore upgraded inputs", err)
	}

	job := cliTask(t, s)
	job.Stage, job.Input, job.ProjectID, job.ShotID = "board_retry", old.Input, old.Input.ID, q.ID
	job.ReferencePolicyVersion = fourViewPolicyVersion
	plan, raw, err := s.prepareFourViewReferences(refs, job.Input, q)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Sources[1].Kind != "character" || !strings.Contains(plan.Sources[1].Name, "四视角") {
		t.Fatal("wrong sheet metadata", plan)
	}
	if err = s.saveReferencePreviews(job.Input, plan, raw); err != nil {
		t.Fatal(err)
	}
	for i, b := range plan.Boards {
		preview, err := os.ReadFile(s.media(b.File))
		if err != nil || digest(preview) != digest(raw[i]) || digest(preview) != b.Hash {
			t.Fatal("preview differs from upload", err)
		}
	}
	job.ImageReferencePlan = &plan
	calls := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		files := r.MultipartForm.File["image[]"]
		if len(files) != len(raw) {
			t.Error("wrong upload count")
			w.WriteHeader(400)
			return
		}
		for i, f := range files {
			rd, e := f.Open()
			if e != nil {
				t.Error(e)
				continue
			}
			b, e := io.ReadAll(rd)
			rd.Close()
			if e != nil || digest(b) != plan.Boards[i].Hash {
				t.Error("payload differs from quote")
			}
		}
		if !strings.Contains(r.FormValue("prompt"), "四视角") {
			t.Error("sheet instruction missing")
		}
		json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]string{"b64_json": base64.StdEncoding.EncodeToString(mockPNG("test", false, false))}}})
	}))
	defer api.Close()
	t.Setenv("FOUR_VIEW_TEST_KEY", "test")
	job.Config = Settings{Image: Endpoint{Base: api.URL, Model: "image", KeyEnv: "FOUR_VIEW_TEST_KEY"}}
	for i := 0; i < 2; i++ {
		if _, err = s.remoteImage(context.Background(), &job, "shot-"+q.ID+"-retry", "frame", refs, false); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatal("recovery submitted again", calls)
	}
	job.ImageReferencePlan.Boards[0].Hash = "tampered"
	if _, _, err = s.prepareTaskReferences(&job, "shot-"+q.ID+"-retry", refs); err == nil {
		t.Fatal("changed quote accepted")
	}
}

func TestFourViewConfirmationAndBatchPlans(t *testing.T) {
	s := testStudio(t)
	p := readyRetryProject(t, s)
	for i := range p.Shots {
		p.Shots[i].Candidates = nil
		p.Shots[i].Selected = 0
		p.Shots[i].Approved = false
	}
	before := string(jsonBytes(p))
	quote, err := s.quoteFor(p, defaultSettings(), "boards")
	if err != nil {
		t.Fatal(err)
	}
	if quote.ReferencePolicyVersion != fourViewPolicyVersion || len(quote.ShotReferencePlans) != len(p.Shots) {
		t.Fatal("missing frozen per-shot plans", quote)
	}
	if string(jsonBytes(p)) != before {
		t.Fatal("quote mutated project")
	}
	job := Task{Input: p, Stage: "boards", ReferencePolicyVersion: fourViewPolicyVersion, ShotReferencePlans: quote.ShotReferencePlans}
	for _, shot := range p.Shots {
		refs, err := s.taskBoardReferences(job, shot)
		if err != nil {
			t.Fatal(err)
		}
		plan, _, err := s.prepareTaskReferences(&job, "shot-"+shot.ID+"-0", refs)
		if err != nil || !reflect.DeepEqual(plan, quote.ShotReferencePlans["shot-"+shot.ID+"-0"]) {
			t.Fatal("batch changed quoted inputs", err)
		}
	}
	for i := range p.Assets {
		if p.Assets[i].Kind == "character" {
			p.Assets[i].Sheet.Approved = false
		}
	}
	if _, err = s.fourViewBoardReferences(p, p.Shots[0]); err == nil {
		t.Fatal("unapproved sheet accepted")
	}
	// The legacy policy still ignores new sheet confirmation requirements at collection.
	if _, err = s.boardReferenceImages(p, p.Shots[0]); err != nil {
		t.Fatal("legacy collector changed", err)
	}
}

func TestFourViewCLIUsesQuotedBatchBoardsAndRecoversOnce(t *testing.T) {
	s := testStudio(t)
	p := readyRetryProject(t, s)
	shot := p.Shots[0]
	refs, err := s.fourViewBoardReferences(p, shot)
	if err != nil {
		t.Fatal(err)
	}
	plan, _, err := s.prepareFourViewReferences(refs, p, shot)
	if err != nil {
		t.Fatal(err)
	}
	key := "shot-" + shot.ID + "-0"
	job := cliTask(t, s)
	job.Input = p
	job.ProjectID = p.ID
	job.Stage = "boards"
	job.ReferencePolicyVersion = fourViewPolicyVersion
	job.ShotReferencePlans = map[string]ReferencePlan{key: plan}
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	if err = os.MkdirAll(filepath.Join(home, "generated_images"), 0700); err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(t.TempDir(), "result.png")
	if err = os.WriteFile(fixture, mockPNG("four-view", false, false), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("IMAGE_FIXTURE", fixture)
	ep := Endpoint{CLI: "codex", Executable: fakeCLI(t, "image")}
	for i := 0; i < 2; i++ {
		if _, err = s.cliImage(context.Background(), &job, key, "frame", refs, false, ep); err != nil {
			t.Fatal(err)
		}
	}
	dir := filepath.Join(s.data, "cli", job.ID, key)
	args, err := os.ReadFile(filepath.Join(dir, "args.txt"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(args), "\n")
	n := 0
	for i, line := range lines {
		if line == "--image" {
			b, e := os.ReadFile(lines[i+1])
			if e != nil || n >= len(plan.Boards) || digest(b) != plan.Boards[n].Hash {
				t.Fatal("CLI input differs from quote", e)
			}
			n++
		}
	}
	if n != len(plan.Boards) {
		t.Fatal("missing CLI reference", n)
	}
	runs, err := os.ReadFile(filepath.Join(dir, "runs"))
	if err != nil || strings.Count(string(runs), "run") != 1 {
		t.Fatal("CLI was rerun", string(runs), err)
	}
}

func TestFourViewSharedFilesRetainAssetIdentity(t *testing.T) {
	s := testStudio(t)
	p := createProject(t, s)
	b := referencePNG(t, 120, 100, 1)
	file, err := s.writeMedia(p.ID, "shared.png", b)
	if err != nil {
		t.Fatal(err)
	}
	im := Image{File: file, Hash: digest(b), Approved: true}
	p.Assets = []Asset{{ID: "scene", Kind: "scene", Name: "场景", Master: &im}, {ID: "prop", Kind: "prop", Name: "道具", Master: &im}}
	shot := Shot{ID: "s", References: []string{"prop", "scene"}}
	refs, err := s.fourViewBoardReferences(p, shot)
	if err != nil {
		t.Fatal(err)
	}
	plan, _, err := s.prepareFourViewReferences(refs, p, shot)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Sources[0].AssetID != "prop" || plan.Sources[0].Kind != "prop" || plan.Sources[1].AssetID != "scene" {
		t.Fatal("file identity replaced asset identity", plan)
	}
}

// Exercise the actual coordinator -> immutable worker -> provider validation path.
func TestBatchFourViewWorkersRetainQuotedStageAndSkipExistingMedia(t *testing.T) {
	s := testStudio(t)
	p := readyRetryProject(t, s)
	p.Mode = "remote"
	original := p.Shots[0]
	p.Shots[0].Video = "existing-video.mp4"
	for i := 1; i < len(p.Shots); i++ {
		p.Shots[i].Candidates = nil
		p.Shots[i].Selected = 0
		p.Shots[i].Approved = false
	}
	cfg := defaultSettings()
	cfg.Mode = "remote"
	var calls atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		if len(r.MultipartForm.File["image[]"]) == 0 {
			t.Error("missing quoted references")
		}
		json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]string{"b64_json": base64.StdEncoding.EncodeToString(mockPNG("batch", false, false))}}})
	}))
	defer api.Close()
	t.Setenv("BATCH_FOUR_VIEW_KEY", "test")
	cfg.Image = Endpoint{Base: "https://example.com/v1", Model: "image", KeyEnv: "BATCH_FOUR_VIEW_KEY"}
	quote, err := s.quoteFor(p, cfg, "boards")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Image.Base = api.URL // Provider calls remain inside this local test server.
	job := Task{ID: uid(), ProjectID: p.ID, Stage: "boards", Status: "running", Mode: "remote", Input: p, Config: cfg, ReferencePolicyVersion: quote.ReferencePolicyVersion, ShotReferencePlans: quote.ShotReferencePlans, BoardImagesPerShot: quote.BoardImagesPerShot}
	if _, err = s.store.db.Exec("UPDATE projects SET body=? WHERE id=?", jsonBytes(p), p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.store.db.Exec("INSERT INTO tasks VALUES(?,?,?,?,?,?)", job.ID, p.ID, job.Status, jsonBytes(job), jsonBytes(p), jsonBytes(cfg)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := s.fillBoardImages(context.Background(), &job, s.makeImage); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != int32(len(p.Shots)-1) {
		t.Fatal("batch scope or recovery changed", calls.Load())
	}
	current := getProject(t, s, p.ID)
	if !reflect.DeepEqual(current.Shots[0].Candidates, original.Candidates) || current.Shots[0].Video != "existing-video.mp4" {
		t.Fatal("existing media changed")
	}
	for _, shot := range current.Shots[1:] {
		if !shotHasImage(shot) || shot.Candidates[0].ReferencePlan == nil {
			t.Fatal("missing generated image or quoted snapshot")
		}
	}
}
