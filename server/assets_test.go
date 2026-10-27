package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func readyAssets(t *testing.T, s *Studio, pid string) AssetSet {
	t.Helper()
	p, e := s.store.project(pid)
	if e != nil {
		t.Fatal(e)
	}
	b := mockBoard(p, "", "")
	enrichMockBoard(&b, visualDefaults(p.Visual, p.Idea))
	input := AssetSet{Visual: b.Visual, Story: StoryPlan{Synopsis: p.Idea, Beats: []string{"start", "end"}}, Characters: b.Characters, Scenes: b.Scenes, Props: b.Props}
	a, _, e := cleanAssetSet(input, nil, p)
	if e != nil {
		t.Fatal(e)
	}
	for _, i := range assetItems(&a) {
		file := filepath.Join(pid, "fixture", strings.ReplaceAll(i.ID, ":", "-")+".png")
		if e = os.MkdirAll(filepath.Dir(s.abs(file)), 0700); e != nil {
			t.Fatal(e)
		}
		fixturePNG(t, s.abs(file), 8, 8)
		*i.Image = file
		*i.Hash, _ = fileHash(s.abs(file))
		*i.Review = Review{State: "passed", Reason: "test fixture"}
	}
	for _, i := range assetItems(&a) {
		a.Approvals[i.ID] = AssetApproval{Hash: i.fingerprint(a.Visual), At: now()}
	}
	for ci := range a.Characters {
		for _, v := range a.Characters[ci].Views {
			if v.Kind == "front" {
				a.Characters[ci].Image = v.Image
			}
		}
	}
	a.Locked = true
	if e = s.store.update(pid, func(p *Project) { p.AssetSets = []AssetSet{clone(a)}; p.AssetWorkflow = true; p.Visual = a.Visual }); e != nil {
		t.Fatal(e)
	}
	return a
}
func runAssetQuote(t *testing.T, s *Studio, pid, stage, target string) Project {
	t.Helper()
	q, e := s.makeQuote(pid, stage, "", target)
	if e != nil {
		t.Fatal(stage, e)
	}
	if e = s.confirm(pid, q.ID, true); e != nil {
		t.Fatal(e)
	}
	p := waitProject(t, s, pid)
	if p.Status != "review" {
		t.Fatal(stage, p.Error)
	}
	return p
}
func approveAssetsForTest(t *testing.T, s *Studio, pid string, masters bool) Project {
	t.Helper()
	p, _ := s.store.project(pid)
	a := assets(p)
	targets := []string{}
	for _, i := range assetItems(a) {
		if !masters || strings.HasSuffix(i.ID, ":front") {
			targets = append(targets, i.ID)
		}
	}
	w := modelAPI(t, s, "POST", "/api/projects/"+pid+"/assets/approve", map[string]any{"version": a.Version, "revision": p.Revision, "targets": targets})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	p, _ = s.store.project(pid)
	return p
}
func TestAssetPhasesRequireHumanApproval(t *testing.T) {
	s := testStudio(t)
	p := createProject(t, s, "one cat")
	if _, e := s.makeQuote(p.ID, "storyboard", "", ""); e == nil {
		t.Fatal("storyboard allowed without assets")
	}
	q, e := s.makeQuote(p.ID, "asset_plan", "", "")
	if e != nil {
		t.Fatal(e)
	}
	if len(q.Requests) != 1 || q.Requests[0].Kind != "text" || q.Scope["text"] != 1 {
		t.Fatal("asset plan generated images")
	}
	if e = s.confirm(p.ID, q.ID, true); e != nil {
		t.Fatal(e)
	}
	p = waitProject(t, s, p.ID)
	if len(p.Boards) != 0 || assets(p) == nil {
		t.Fatal("analysis created storyboard")
	}
	if _, e = s.makeQuote(p.ID, "reference_assets", "", ""); e == nil {
		t.Fatal("reference views allowed before main approval")
	}
	p = runAssetQuote(t, s, p.ID, "character_masters", "")
	a := assets(p)
	for _, i := range assetItems(a) {
		if !strings.HasSuffix(i.ID, ":front") && *i.Image != "" {
			t.Fatal("generated extra image")
		}
	}
	if _, e = s.makeQuote(p.ID, "reference_assets", "", ""); e == nil {
		t.Fatal("model review substituted human approval")
	}
	p = approveAssetsForTest(t, s, p.ID, true)
	q, e = s.makeQuote(p.ID, "reference_assets", "", "")
	if e != nil {
		t.Fatal(e)
	}
	expected := len(assets(p).Characters)*3 + len(assets(p).Scenes) + len(assets(p).Props)
	images := 0
	for _, r := range q.Requests {
		if r.Kind == "image" {
			images += r.Count
		}
	}
	if images != expected {
		t.Fatalf("images %d want %d", images, expected)
	}
	if e = s.confirm(p.ID, q.ID, true); e != nil {
		t.Fatal(e)
	}
	p = waitProject(t, s, p.ID)
	if p.Status != "review" {
		t.Fatal(p.Error)
	}
	a = assets(p)
	for _, c := range a.Characters {
		if len(c.Views) != 4 {
			t.Fatal("missing four views")
		}
		var front string
		for _, v := range c.Views {
			if v.Kind == "front" {
				front = v.Image
			}
		}
		for _, v := range c.Views {
			if v.Kind != "front" {
				if len(v.References) < 1 || v.References[0].File != front {
					t.Fatal("angle image did not reference approved master")
				}
			}
		}
	}
	w := modelAPI(t, s, "POST", "/api/projects/"+p.ID+"/assets/lock", map[string]int{"version": a.Version, "revision": p.Revision})
	if w.Code != 400 {
		t.Fatal("locked without human approval")
	}
	p = approveAssetsForTest(t, s, p.ID, false)
	a = assets(p)
	w = modelAPI(t, s, "POST", "/api/projects/"+p.ID+"/assets/lock", map[string]int{"version": a.Version, "revision": p.Revision})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	before, _ := s.store.project(p.ID)
	count := len(before.Attempts)
	q, e = s.makeQuote(p.ID, "storyboard", "", "")
	if e != nil {
		t.Fatal(e)
	}
	if q.Scope["character_image"] != 0 || q.Scope["text"] != 1 || q.Scope["storyboard_image"] != 36 {
		t.Fatal("storyboard includes asset regeneration")
	}
	if e = s.confirm(p.ID, q.ID, true); e != nil {
		t.Fatal(e)
	}
	p = waitProject(t, s, p.ID)
	if p.Status != "review" || board(p) == nil || board(p).Schema != 3 {
		t.Fatal(p.Error)
	}
	for key, attempt := range p.Attempts {
		if _, old := before.Attempts[key]; !old && attempt.Role == "character_image" {
			t.Fatal("regenerated character in storyboard")
		}
	}
	if len(p.Attempts) <= count {
		t.Fatal("no storyboard calls")
	}
	if board(p).AssetHash != hash(assets(p)) {
		t.Fatal("board did not bind locked asset snapshot")
	}
	if e = validateApproval(*board(p)); e != nil {
		t.Fatal(e)
	}
}
func TestAssetEditInvalidationAndQuotes(t *testing.T) {
	s := testStudio(t)
	p := addProject(t, s)
	a := readyAssets(t, s, p.ID)
	q, e := s.makeQuote(p.ID, "storyboard", "", "")
	if e != nil {
		t.Fatal(e)
	}
	p, _ = s.store.project(p.ID)
	next := clone(a)
	next.Characters[0].Description += " changed"
	body := map[string]any{"version": a.Version, "revision": p.Revision, "assets": next}
	w := modelAPI(t, s, "POST", "/api/projects/"+p.ID+"/assets/impact", body)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w = modelAPI(t, s, "PUT", "/api/projects/"+p.ID+"/assets", body)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	current, _ := s.store.project(p.ID)
	newAssets := assets(current)
	if newAssets.Locked || newAssets.Characters[0].Views[0].Image != "" || newAssets.Scenes[0].Image != a.Scenes[0].Image {
		t.Fatal("edit invalidated wrong assets")
	}
	if len(current.AssetSets) != 2 || current.AssetSets[0].Characters[0].Views[0].Image == "" {
		t.Fatal("historical image lost")
	}
	if s.confirm(p.ID, q.ID, true) == nil {
		t.Fatal("stale asset quote accepted")
	}
	// A client cannot forge approval metadata or image paths through the editor.
	forged := clone(*newAssets)
	forged.Locked = true
	forged.Approvals = a.Approvals
	forged.Characters[0].Views = a.Characters[0].Views
	w = modelAPI(t, s, "PUT", "/api/projects/"+p.ID+"/assets", map[string]any{"version": newAssets.Version, "revision": current.Revision, "assets": forged})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	current, _ = s.store.project(p.ID)
	if assets(current).Locked || assets(current).Characters[0].Views[0].Image != "" {
		t.Fatal("untrusted image metadata accepted")
	}
}
func TestSingleMasterRedoAndConcurrentConfirmation(t *testing.T) {
	s := testStudio(t)
	p := addProject(t, s)
	a := readyAssets(t, s, p.ID)
	target := a.Characters[0].ID + ":front"
	q, e := s.makeQuote(p.ID, "character_masters", "", target)
	if e != nil {
		t.Fatal(e)
	}
	if q.Scope["character_image"] != 1 {
		t.Fatal("single redo expanded images")
	}
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = s.confirm(p.ID, q.ID, true) }()
	}
	wg.Wait()
	p = waitProject(t, s, p.ID)
	if p.Status != "review" {
		t.Fatal(p.Error)
	}
	latest := assets(p)
	generated := 0
	for _, v := range latest.Characters[0].Views {
		if v.Image != "" {
			generated++
		}
	}
	if generated != 1 || latest.Locked {
		t.Fatal("master redo did not invalidate derived views")
	}
	if latest.Scenes[0].Image != a.Scenes[0].Image {
		t.Fatal("unrelated scene not reused")
	}
	images := 0
	for _, attempt := range p.Attempts {
		if attempt.Kind == "image" {
			images++
		}
	}
	if images != 1 {
		t.Fatal("duplicate confirmation submitted images", images)
	}
}
func TestAssetFileHashAndIncompleteReviewGate(t *testing.T) {
	s := testStudio(t)
	p := addProject(t, s)
	a := readyAssets(t, s, p.ID)
	q, e := s.makeQuote(p.ID, "storyboard", "", "")
	if e != nil {
		t.Fatal(e)
	}
	file := a.Characters[0].Views[0].Image
	if e = os.WriteFile(s.abs(file), []byte("tampered"), 0600); e != nil {
		t.Fatal(e)
	}
	if s.confirm(p.ID, q.ID, true) == nil {
		t.Fatal("mutated asset file accepted")
	}
	a = readyAssets(t, s, p.ID)
	a.Locked = false
	a.Approvals = map[string]AssetApproval{}
	a.Characters[0].Views[0].Review = Review{State: "incomplete"}
	if e = s.store.update(p.ID, func(p *Project) { p.AssetSets = []AssetSet{clone(a)} }); e != nil {
		t.Fatal(e)
	}
	p, _ = s.store.project(p.ID)
	w := modelAPI(t, s, "POST", "/api/projects/"+p.ID+"/assets/approve", map[string]any{"version": a.Version, "revision": p.Revision, "targets": []string{a.Characters[0].ID + ":front"}, "reason": "human"})
	if w.Code != 400 {
		t.Fatal("incomplete audit accepted")
	}
}
func TestAssetLegacyImportPreservesProduction(t *testing.T) {
	s := testStudio(t)
	p, b := generatedBoard(t, s)
	if e := s.store.update(p.ID, func(p *Project) { p.Output = "existing.mp4" }); e != nil {
		t.Fatal(e)
	}
	w := modelAPI(t, s, "POST", "/api/projects/"+p.ID+"/assets/import", map[string]any{})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	p, _ = s.store.project(p.ID)
	a := assets(p)
	if a == nil || a.Locked || p.Output != "existing.mp4" || len(p.Boards) != 1 {
		t.Fatal("import overwrote production")
	}
	if a.Characters[0].Views[0].Image != b.Characters[0].Views[0].Image {
		t.Fatal("old image not reused")
	}
	if len(a.Characters[0].Views) != 4 {
		t.Fatal("back placeholder missing")
	}
	if len(a.Approvals) != 0 {
		t.Fatal("legacy automatically approved")
	}
}
func TestAssetRecoverSavedResponseWithoutNewTasks(t *testing.T) {
	s := testStudio(t)
	p := addProject(t, s)
	p = runAssetQuote(t, s, p.ID, "asset_plan", "")
	p = runAssetQuote(t, s, p.ID, "character_masters", "")
	count := len(p.Attempts)
	if e := s.run(context.Background(), p.ID, defaults(), "character_masters", "", ""); e != nil {
		t.Fatal(e)
	}
	again, _ := s.store.project(p.ID)
	if len(again.Attempts) != count {
		t.Fatal("completed asset task resubmitted")
	}
	raw, _ := json.Marshal(again)
	if !strings.Contains(string(raw), "asset_sets") {
		t.Fatal("asset state not exported")
	}
}

func TestAssetRedoKeepsConfirmedQuoteWhenInvalidatingBoard(t *testing.T) {
	s := testStudio(t)
	p := addProject(t, s)
	readyAssets(t, s, p.ID)
	p = runAssetQuote(t, s, p.ID, "storyboard", "")
	before := assets(p)
	target := before.Characters[0].ID + ":front"
	q, e := s.makeQuote(p.ID, "character_masters", "", target)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.confirm(p.ID, q.ID, true); e != nil {
		t.Fatal(e)
	}
	p = waitProject(t, s, p.ID)
	if p.Status != "review" || p.ActiveQuote != q.ID {
		t.Fatal("asset invalidation lost authorization", p.Status, p.ActiveQuote, q.ID, p.Error)
	}
	if !board(p).Shots[0].Stale {
		t.Fatal("dependent shot not marked stale")
	}
	count := 0
	for _, a := range p.Attempts {
		if a.QuoteID == q.ID && a.Kind == "image" {
			count++
		}
	}
	if count != 1 {
		t.Fatal("redo escaped or duplicated quoted image scope", count)
	}
	if assets(p).Scenes[0].Image != before.Scenes[0].Image {
		t.Fatal("unrelated references changed")
	}
}

func TestLegacyConfirmedScopeDoesNotAddBackView(t *testing.T) {
	s := testStudio(t)
	p := addProject(t, s)
	id := uid()
	if e := s.store.change(func(st *State) error {
		st.Quotes[id] = StoredQuote{Quote: Quote{ID: id, Schema: 2, ProjectID: p.ID, Stage: "storyboard", Used: true, Scope: storyboardScope(nil)}, Settings: defaults()}
		st.Projects[p.ID].ActiveQuote = id
		st.Projects[p.ID].Status = "interrupted"
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if e := s.resume(p.ID); e != nil {
		t.Fatal(e)
	}
	p = waitProject(t, s, p.ID)
	if p.Status != "review" {
		t.Fatal(p.Error)
	}
	if assets(p) != nil {
		t.Fatal("old authorization created new asset workflow")
	}
	for _, c := range board(p).Characters {
		if len(c.Views) != 3 {
			t.Fatal("legacy view count changed")
		}
		for _, v := range c.Views {
			if v.Kind == "back" {
				t.Fatal("legacy authorization generated back view")
			}
		}
	}
}
