package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
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

func editFixture(t *testing.T, s *Studio, p Project, ids []string) (Quote, Task) {
	t.Helper()
	q, err := s.quoteFor(p, defaultSettings(), "board_edit", ShotRequest{ShotID: p.Shots[0].ID, Feedback: "只移动合格证明", Preserve: "保持猫的身份、姿势、构图和光线", ReferenceIDs: ids})
	if err != nil {
		t.Fatal(err)
	}
	input, _ := decodeBody[Project](jsonBytes(p))
	job := Task{ID: uid(), ProjectID: p.ID, Stage: "board_edit", ShotID: p.Shots[0].ID, Feedback: q.Feedback, Status: "running", Mode: "mock", Config: defaultSettings(), Input: input, ImageEdit: q.ImageEdit, ImageReferencePlan: q.ImageReferencePlan, ReferencePolicyVersion: q.ReferencePolicyVersion}
	if _, err = s.store.db.Exec("INSERT INTO tasks VALUES(?,?,?,?,?,?)", job.ID, p.ID, job.Status, jsonBytes(job), jsonBytes(input), jsonBytes(job.Config)); err != nil {
		t.Fatal(err)
	}
	return q, job
}

func TestEditQuoteHasIndependentBaseAndAtMostFourAuxiliaryBoards(t *testing.T) {
	s := testStudio(t)
	p := readyRetryProject(t, s)
	for i := 0; i < 7; i++ {
		b := referencePNG(t, 120, 100, uint16(i))
		file, err := s.writeMedia(p.ID, "aux-"+string(rune('a'+i))+".png", b)
		if err != nil {
			t.Fatal(err)
		}
		id := "aux-" + string(rune('a'+i))
		im := Image{File: file, Hash: digest(b), Approved: true}
		p.Assets = append(p.Assets, Asset{ID: id, Kind: "prop", Name: id, Master: &im})
		p.Shots[0].References = append(p.Shots[0].References, id)
	}
	before := string(jsonBytes(p))
	q, _ := editFixture(t, s, p, nil)
	if len(q.Items) != 1 || q.Items[0].Count != 1 || q.ImageEdit == nil || q.ImageEdit.Base.Hash != p.Shots[0].Candidates[p.Shots[0].Selected].Hash {
		t.Fatal("incorrect edit scope")
	}
	plan := q.ImageReferencePlan
	if plan.Version != 3 || len(plan.Boards) > 5 || len(plan.Sources) != len(p.Shots[0].References)+1 || plan.Boards[0].Kind != "edit_base" || len(plan.Boards[0].Cells) != 1 || plan.Boards[0].Cells[0].Source != 0 {
		t.Fatal("base was packed or references lost")
	}
	for _, board := range plan.Boards[1:] {
		for _, cell := range board.Cells {
			if cell.Source == 0 {
				t.Fatal("base mixed into auxiliary board")
			}
		}
	}
	if string(jsonBytes(p)) != before {
		t.Fatal("quote changed project")
	}
	sources := make([]ReferenceSource, 5)
	for i := range sources {
		sources[i] = ReferenceSource{Name: "大辅助图", Kind: "prop", Width: 3072, Height: 2048}
	}
	if _, err := layoutReferenceBoardsWithLimit(sources, 4); err == nil || !strings.Contains(err.Error(), "最多4张") {
		t.Fatal("edit auxiliary cap not enforced", err)
	}
}

func TestEditFailureKeepsOriginalAndBindingStopsSourceChanges(t *testing.T) {
	s := testStudio(t)
	p := approveFirstVideoShot(t, s, readyRetryProject(t, s))
	_, job := editFixture(t, s, p, []string{})
	if len(job.ImageEdit.AuxiliaryIDs) != 0 || len(job.ImageReferencePlan.Boards) != 1 {
		t.Fatal("base-only edit not supported")
	}
	before := string(jsonBytes(job.Input))
	calls := 0
	err := s.editBoard(context.Background(), &job, func(context.Context, *Task, string, string, []Image, bool) (Image, error) {
		calls++
		return Image{}, errors.New("unknown")
	})
	if err == nil || calls != 1 || string(jsonBytes(job.Input)) != before {
		t.Fatal("failed edit changed original")
	}
	job.ImageEdit.Base.Hash = "changed"
	err = s.editBoard(context.Background(), &job, func(context.Context, *Task, string, string, []Image, bool) (Image, error) {
		calls++
		return Image{}, nil
	})
	if err == nil || calls != 1 {
		t.Fatal("changed base submitted")
	}
}

func TestEditConfirmationVersionsAndSwitchBack(t *testing.T) {
	s := testStudio(t)
	p := approveFirstVideoShot(t, s, readyRetryProject(t, s))
	oldIndex := p.Shots[0].Selected
	original := p.Shots[0].Candidates[oldIndex]
	oldVideo, err := s.writeMedia(p.ID, "old-version-video.mp4", []byte("old video preserved"))
	if err != nil {
		t.Fatal(err)
	}
	p.Shots[0].Video = oldVideo
	p.Output = oldVideo
	if _, err = s.store.db.Exec("UPDATE projects SET body=? WHERE id=?", jsonBytes(p), p.ID); err != nil {
		t.Fatal(err)
	}
	var q Quote
	json.Unmarshal(request(t, s, "POST", "/api/projects/"+p.ID+"/quotes", map[string]any{"revision": p.Revision, "stage": "board_edit", "shot_id": p.Shots[0].ID, "feedback": "只移动证明", "preserve": "保持主体姿势构图光线", "reference_ids": []string{}}, 200), &q)
	if q.ImageEdit == nil || len(q.ImageEdit.AuxiliaryIDs) != 0 || q.Items[0].Count != 1 {
		t.Fatal("empty auxiliary selection lost")
	}
	if current := getProject(t, s, p.ID); !reflect.DeepEqual(current, p) {
		t.Fatal("quoting modified project")
	}
	var task Task
	json.Unmarshal(request(t, s, "POST", "/api/quotes/"+q.ID+"/confirm", map[string]any{}, 200), &task)
	waitTask(t, s, task.ID)
	got := getProject(t, s, p.ID)
	image := got.Shots[0].Candidates[got.Shots[0].Selected]
	if image.Operation != "edit" || image.SourceFile != original.File || image.SourceHash != original.Hash || got.Shots[0].Approved || got.Shots[0].Video != "" || got.Output != "" {
		t.Fatal("edit lineage or confirmation incorrect")
	}
	if !reflect.DeepEqual(got.Shots[1:], p.Shots[1:]) || !reflect.DeepEqual(got.Shots[0].References, p.Shots[0].References) {
		t.Fatal("other shots or material provenance changed")
	}
	if err = s.verifyImage(original); err != nil {
		t.Fatal("original lost", err)
	}
	if b, _ := os.ReadFile(s.media(oldVideo)); string(b) != "old video preserved" {
		t.Fatal("old video removed")
	}
	var duplicate Task
	json.Unmarshal(request(t, s, "POST", "/api/quotes/"+q.ID+"/confirm", map[string]any{}, 200), &duplicate)
	if duplicate.ID != task.ID {
		t.Fatal("duplicate confirmation created a second edit")
	}
	saved, _ := s.store.task(task.ID)
	calls := 0
	if err = s.editBoard(context.Background(), &saved, func(context.Context, *Task, string, string, []Image, bool) (Image, error) {
		calls++
		return Image{}, nil
	}); err != nil || calls != 0 {
		t.Fatal("completed edit was repeated", err)
	}
	request(t, s, "PUT", "/api/projects/"+p.ID+"/shots/"+p.Shots[0].ID, map[string]any{"revision": got.Revision, "selected": oldIndex, "approved": false, "narration": got.Shots[0].Narration}, 200)
	back := getProject(t, s, p.ID)
	if back.Shots[0].Selected != oldIndex || back.Shots[0].Approved || len(back.Shots[0].Candidates) != len(p.Shots[0].Candidates)+1 {
		t.Fatal("switch-back deleted history or approved implicitly")
	}
	if err = s.verifyImage(image); err != nil {
		t.Fatal("new version lost after switch-back", err)
	}
}

func TestEditCLIUsesIndependentBaseAndNeverReruns(t *testing.T) {
	s := testStudio(t)
	p := readyRetryProject(t, s)
	_, job := editFixture(t, s, p, nil)
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "generated_images"), 0700); err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(t.TempDir(), "result.png")
	if err := os.WriteFile(fixture, mockPNG("edit", false, false), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("IMAGE_FIXTURE", fixture)
	_, _, refs, err := s.imageEditScope(job.Input, job.ShotID, job.ImageEdit.Changes, job.ImageEdit.Preserve, job.ImageEdit.AuxiliaryIDs)
	if err != nil {
		t.Fatal(err)
	}
	ep := Endpoint{CLI: "codex", Executable: fakeCLI(t, "image")}
	key := "shot-" + job.ShotID + "-edit"
	for i := 0; i < 2; i++ {
		if _, err = s.cliImage(context.Background(), &job, key, "edit", refs, false, ep); err != nil {
			t.Fatal(err)
		}
	}
	dir := filepath.Join(s.data, "cli", job.ID, key)
	args, _ := os.ReadFile(filepath.Join(dir, "args.txt"))
	lines := strings.Split(string(args), "\n")
	paths := []string{}
	for i, line := range lines {
		if line == "--image" {
			paths = append(paths, lines[i+1])
		}
	}
	if len(paths) > 5 || filepath.Base(paths[0]) != "edit-base.png" {
		t.Fatal("base not independent or too many attachments")
	}
	base, _ := os.ReadFile(paths[0])
	original, _ := os.ReadFile(s.media(job.ImageEdit.Base.File))
	if !bytes.Equal(base, original) {
		t.Fatal("base altered or packed")
	}
	for _, path := range paths[1:] {
		if !strings.HasPrefix(filepath.Base(path), "auxiliary-") {
			t.Fatal("auxiliary input not distinguished")
		}
	}
	prompt, _ := os.ReadFile(filepath.Join(dir, "prompt.txt"))
	if !strings.Contains(string(prompt), "唯一编辑目标") || !strings.Contains(string(prompt), "最多4张") {
		t.Fatal("missing edit input semantics")
	}
	runs, _ := os.ReadFile(filepath.Join(dir, "runs"))
	if string(runs) != "run" {
		t.Fatal("edit was resubmitted")
	}
}

func TestEditAPIBaseIsFirstAndOutputCountIsOne(t *testing.T) {
	s := testStudio(t)
	p := readyRetryProject(t, s)
	_, job := editFixture(t, s, p, nil)
	_, _, refs, err := s.imageEditScope(job.Input, job.ShotID, job.ImageEdit.Changes, job.ImageEdit.Preserve, job.ImageEdit.AuxiliaryIDs)
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		files := r.MultipartForm.File["image[]"]
		if len(files) < 1 || len(files) > 5 || files[0].Filename != "edit-base.png" || r.FormValue("n") != "1" {
			t.Error("bad edit request scope")
		}
		f, err := files[0].Open()
		if err != nil {
			t.Error(err)
			return
		}
		b, _ := io.ReadAll(f)
		f.Close()
		if digest(b) != job.ImageEdit.Base.Hash {
			t.Error("base altered")
		}
		json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]string{"b64_json": base64.StdEncoding.EncodeToString(mockPNG("edited", false, false))}}})
	}))
	defer api.Close()
	t.Setenv("EDIT_API_KEY", "test-key")
	job.Config = Settings{Image: Endpoint{Base: api.URL, Model: "image", KeyEnv: "EDIT_API_KEY"}}
	for i := 0; i < 2; i++ {
		if _, err = s.remoteImage(context.Background(), &job, "shot-"+job.ShotID+"-edit", "edit", refs, false); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("edit POST repeated")
	}
}

func TestEditQuoteRejectsChangedBaseAndMissingInstructions(t *testing.T) {
	s := testStudio(t)
	p := readyRetryProject(t, s)
	for _, bad := range []map[string]any{
		{"revision": p.Revision, "stage": "board_edit", "shot_id": p.Shots[0].ID, "feedback": "", "preserve": "保留主体"},
		{"revision": p.Revision, "stage": "board_edit", "shot_id": p.Shots[0].ID, "feedback": "修改", "preserve": ""},
	} {
		request(t, s, "POST", "/api/projects/"+p.ID+"/quotes", bad, 400)
	}
	var q Quote
	json.Unmarshal(request(t, s, "POST", "/api/projects/"+p.ID+"/quotes", map[string]any{"revision": p.Revision, "stage": "board_edit", "shot_id": p.Shots[0].ID, "feedback": "只移动证明", "preserve": "其余不变"}, 200), &q)
	q.ImageEdit.Base.Hash = "tampered"
	if _, err := s.store.db.Exec("UPDATE quotes SET body=? WHERE id=?", jsonBytes(q), q.ID); err != nil {
		t.Fatal(err)
	}
	body := request(t, s, "POST", "/api/quotes/"+q.ID+"/confirm", map[string]any{}, 400)
	if !strings.Contains(string(body), "重新报价") {
		t.Fatal("changed base not rejected", string(body))
	}
}

func TestUnknownEditCLIIsNeverRelaunched(t *testing.T) {
	s := testStudio(t)
	p := readyRetryProject(t, s)
	_, job := editFixture(t, s, p, []string{})
	t.Setenv("CODEX_HOME", t.TempDir())
	ep := Endpoint{CLI: "codex", Executable: fakeCLI(t, "fail")}
	_, _, refs, err := s.imageEditScope(job.Input, job.ShotID, job.ImageEdit.Changes, job.ImageEdit.Preserve, job.ImageEdit.AuxiliaryIDs)
	if err != nil {
		t.Fatal(err)
	}
	key := "shot-" + job.ShotID + "-edit"
	before := string(jsonBytes(job.Input))
	for i := 0; i < 2; i++ {
		if _, err = s.cliImage(context.Background(), &job, key, "edit", refs, false, ep); err == nil {
			t.Fatal("unknown edit accepted")
		}
	}
	b, _ := os.ReadFile(filepath.Join(s.data, "cli", job.ID, key, "runs"))
	if string(b) != "run" || string(jsonBytes(job.Input)) != before {
		t.Fatal("unknown edit repeated or original changed")
	}
}
