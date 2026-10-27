package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func generatedBoard(t *testing.T, s *Studio) (Project, Board) {
	t.Helper()
	p := addProject(t, s)
	if e := s.run(context.Background(), p.ID, defaults(), "storyboard", "", ""); e != nil {
		t.Fatal(e)
	}
	p, _ = s.store.project(p.ID)
	return p, *board(p)
}
func authorizeTest(t *testing.T, s *Studio, pid string, scope map[string]int) {
	t.Helper()
	id := uid()
	if e := s.store.change(func(st *State) error {
		st.Quotes[id] = StoredQuote{Quote: Quote{Schema: 2, ID: id, Used: true, ProjectID: pid, Scope: scope}, Settings: defaults()}
		st.Projects[pid].ActiveQuote = id
		return nil
	}); e != nil {
		t.Fatal(e)
	}
}
func TestCandidateIntentAndScope(t *testing.T) {
	s := testStudio(t)
	p := addProject(t, s)
	ctx := context.Background()
	authorizeTest(t, s, p.ID, map[string]int{"storyboard_image": 2})
	a, e := s.scopedRequest(ctx, p.ID, defaults(), "image", "storyboard_image", "candidate-a", "same prompt", nil, 0)
	if e != nil {
		t.Fatal(e)
	}
	b, e := s.scopedRequest(ctx, p.ID, defaults(), "image", "storyboard_image", "candidate-b", "same prompt", nil, 0)
	if e != nil || a == b {
		t.Fatalf("merged candidates: %s %s %v", a, b, e)
	}
	again, e := s.scopedRequest(ctx, p.ID, defaults(), "image", "storyboard_image", "candidate-a", "same prompt", nil, 0)
	if e != nil || again != a {
		t.Fatal("resume changed intent")
	}
	if _, e = s.scopedRequest(ctx, p.ID, defaults(), "image", "storyboard_image", "candidate-c", "same prompt", nil, 0); e == nil {
		t.Fatal("scope overrun")
	}
	p, _ = s.store.project(p.ID)
	if len(p.Attempts) != 2 {
		t.Fatal("replay or scope created extra attempts")
	}
}
func reviewServer(t *testing.T, invalid bool) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	count := &atomic.Int32{}
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		var body struct {
			Messages []struct {
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
			t.Error(e)
			return
		}
		var content []struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal(body.Messages[0].Content, &content)
		var data struct {
			IDs []string `json:"ids"`
		}
		_ = json.Unmarshal([]byte(strings.Split(content[0].Text, "\n")[1]), &data)
		reviews := []any{}
		for _, id := range data.IDs {
			if invalid {
				reviews = append(reviews, map[string]any{"id": id, "reason": "missing dimensions"})
				continue
			}
			reviews = append(reviews, map[string]any{"id": id, "identity": 50, "style": 85, "composition": 80, "continuity": 70, "technical": 90, "identity_error": true, "technical_error": false, "reason": "衣服与身份参考不一致", "issues": []string{"角色服装错误"}, "correction": "恢复主图中的橙色毛发与蓝色围巾"})
		}
		raw, _ := json.Marshal(map[string]any{"reviews": reviews})
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": string(raw)}}}})
	}))
	t.Cleanup(endpoint.Close)
	return endpoint, count
}
func fakeVision(t *testing.T, endpoint string) Settings {
	t.Helper()
	t.Setenv("LYPIT_TEST_KEY", "local-test")
	config := defaults()
	p := clone(config.Providers[0])
	p.ID = "vision-test"
	p.Adapter = "openai"
	p.BaseURL = endpoint
	p.KeyEnv = "LYPIT_TEST_KEY"
	config.Providers = append(config.Providers, p)
	config.Bindings["vision"] = Binding{p.ID, p.Models[0].ID}
	return config
}
func TestRepairOnceAndInvalidReview(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		name := "one-repair"
		if invalid {
			name = "incomplete"
		}
		t.Run(name, func(t *testing.T) {
			s := testStudio(t)
			p, b := generatedBoard(t, s)
			b.Version++
			b.GenerationID = uid()
			q := b.Shots[0]
			q.Image = ""
			q.Selected = ""
			q.Recommended = ""
			q.Candidates = nil
			b.Shots[0] = q
			if e := s.store.update(p.ID, func(p *Project) { p.Boards = append(p.Boards, b) }); e != nil {
				t.Fatal(e)
			}
			endpoint, count := reviewServer(t, invalid)
			settings := fakeVision(t, endpoint.URL)
			authorizeTest(t, s, p.ID, map[string]int{"storyboard_image": 3, "vision": 2})
			result, e := s.generateShot(context.Background(), p.ID, b, q, settings)
			if invalid {
				if e == nil || len(result.Candidates) != 2 || result.Candidates[0].Review.State != "incomplete" {
					t.Fatal("invalid review treated as pass")
				}
				if count.Load() != 1 {
					t.Fatal("invalid review triggered repair")
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			if len(result.Candidates) != 3 || result.Warning == "" || reviewOK(result.Candidates[2].Review) {
				t.Fatal("repair limit/manual handling broken")
			}
			if _, e = s.generateShot(context.Background(), p.ID, b, result, settings); e != nil {
				t.Fatal(e)
			}
			if count.Load() != 2 {
				t.Fatalf("extra paid review/repair: %d", count.Load())
			}
		})
	}
}
func TestCandidateSelectionInvalidatesQuoteAndDependencies(t *testing.T) {
	s := testStudio(t)
	p, b := generatedBoard(t, s)
	b.Approved = true
	b.Shots[1].References = []string{b.Shots[0].ID}
	if e := s.store.update(p.ID, func(p *Project) { p.Boards[0] = b }); e != nil {
		t.Fatal(e)
	}
	quote, e := s.makeQuote(p.ID, "video", "", "")
	if e != nil {
		t.Fatal(e)
	}
	chosen := b.Shots[0].Candidates[0]
	payload, _ := json.Marshal(map[string]any{"version": b.Version, "revision": p.Revision, "shot_id": b.Shots[0].ID, "candidate_id": chosen.ID})
	request := httptest.NewRequest("POST", "/api/projects/"+p.ID+"/select", strings.NewReader(string(payload)))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	s.router().ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	next, _ := s.store.project(p.ID)
	n := board(next)
	if !n.Shots[1].Stale || n.Shots[2].Stale || len(next.Attempts) != len(p.Attempts) || n.Approved {
		t.Fatal("wrong dependency or paid mutation")
	}
	if hash(next.Boards[0]) != hash(b) {
		t.Fatal("old snapshot mutated")
	}
	if s.confirm(p.ID, quote.ID, false) == nil {
		t.Fatal("stale video quote accepted")
	}
}
func TestReferenceCapacityAndMappingImpact(t *testing.T) {
	s := testStudio(t)
	_, b := generatedBoard(t, s)
	settings := defaults()
	settings.Providers[0].Models[0].MaxReferences = 1
	if preflightBoard(b, settings) == nil {
		t.Fatal("references silently dropped")
	}
	old := []SourceMapping{{File: "a", Kind: "character", Target: "c1"}, {File: "b", Kind: "scene", Target: "loc1"}}
	next := clone(old)
	next[0].Kind = "scene"
	next[0].Target = "loc2"
	touched, all := changedMappingTargets(old, next)
	if all || len(touched) != 2 || strings.Contains(strings.Join(touched, ","), "loc1") {
		t.Fatal("unrelated mapping invalidated")
	}
	scope := fillScope(b, nil, "")
	if scope["storyboard_image"] != 0 || scope["character_image"] != 0 || scope["vision"] != 0 {
		t.Fatal("successful assets not reused")
	}
	b.Shots[1].Stale = true
	scope = fillScope(b, nil, "")
	if scope["storyboard_image"] != 3 || scope["vision"] != 2 {
		t.Fatalf("wrong local scope: %v", scope)
	}
}
func TestLegacyMigrationBackupAndFutureRejection(t *testing.T) {
	dir := t.TempDir()
	db, e := sql.Open("sqlite3", filepath.Join(dir, "studio.db"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("CREATE TABLE state (id INTEGER PRIMARY KEY, body TEXT NOT NULL)"); e != nil {
		t.Fatal(e)
	}
	legacy := map[string]any{"settings": defaults(), "projects": map[string]any{"old": Project{ID: "old", Boards: []Board{{Version: 1, Characters: []Character{{ID: "c1", Image: "role.png"}}, Shots: []Shot{{ID: "s1", Image: "shot.png", Video: "movie.mp4"}}}}, Attempts: map[string]Attempt{"done": {State: "succeeded", File: "shot.png"}}}}, "quotes": map[string]any{}}
	raw, _ := json.Marshal(legacy)
	if _, e = db.Exec("INSERT INTO state VALUES(1,?)", raw); e != nil {
		t.Fatal(e)
	}
	db.Close()
	store, e := openStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	project, _ := store.project("old")
	if store.state.FormatVersion != stateVersion || len(board(project).Shots[0].Candidates) != 1 || board(project).Shots[0].Video != "movie.mp4" || len(project.Attempts) != 1 {
		t.Fatal("lost legacy records")
	}
	backups, _ := filepath.Glob(filepath.Join(dir, fmt.Sprintf("studio-before-v%d-*.db", stateVersion)))
	if len(backups) != 1 {
		t.Fatal("missing full backup")
	}
	backup, _ := sql.Open("sqlite3", backups[0])
	var saved []byte
	if e = backup.QueryRow("SELECT body FROM state WHERE id=1").Scan(&saved); e != nil || string(saved) != string(raw) {
		t.Fatal("backup mismatch")
	}
	backup.Close()
	store.state.FormatVersion = stateVersion + 1
	if e = store.saveLocked(); e != nil {
		t.Fatal(e)
	}
	store.close()
	if newer, e := openStore(dir); e == nil {
		newer.close()
		t.Fatal("future version overwritten")
	}
	db, _ = sql.Open("sqlite3", filepath.Join(dir, "studio.db"))
	defer db.Close()
	var future []byte
	if e = db.QueryRow("SELECT body FROM state WHERE id=1").Scan(&future); e != nil {
		t.Fatal(e)
	}
	var st State
	_ = json.Unmarshal(future, &st)
	if st.FormatVersion != stateVersion+1 {
		t.Fatal("future data changed")
	}
	if info, e := os.Stat(backups[0]); e != nil || info.Mode().Perm() != 0600 {
		t.Fatal("backup permissions")
	}
}

func TestLegacyQuoteManifestAndLocalAssetRedo(t *testing.T) {
	s := testStudio(t)
	p, b := generatedBoard(t, s)
	scope := storyboardScope(nil)
	specs := storyboardRequestSpecs(defaults(), scope, "plan")
	count := 0
	for _, r := range specs {
		if r.Kind == "image" {
			count += r.Count
		}
	}
	if count != 50 {
		t.Fatalf("image manifest %d", count)
	}
	preview := clone(b)
	if e := prepareFillTarget(&preview, "asset:"+b.Scenes[0].ID); e != nil {
		t.Fatal(e)
	}
	if preview.Scenes[0].Image != "" || preview.Scenes[1].Image == "" || !preview.Shots[0].Stale || preview.Shots[9].Stale {
		t.Fatal("asset branch invalidation")
	}
	q, e := s.makeQuote(p.ID, "fill", "", "asset:"+b.Scenes[0].ID)
	if e != nil {
		t.Fatal(e)
	}
	if q.Scope["character_image"] != 0 || q.Scope["storyboard_image"] == 0 {
		t.Fatal("wrong asset redo scope")
	}
	if e = s.confirm(p.ID, q.ID, false); e != nil {
		t.Fatal(e)
	}
	after := waitProject(t, s, p.ID)
	if after.Status != "review" || board(after).Scenes[1].Image != b.Scenes[1].Image || board(after).Scenes[0].Image == b.Scenes[0].Image {
		t.Fatal("asset redo failed: " + after.Error)
	}
}
func TestNarrationPreservesImagesAndHashMismatchBlocksVideo(t *testing.T) {
	s := testStudio(t)
	p, b := generatedBoard(t, s)
	next := clone(b)
	next.Shots[0].Narration = "只有旁白改变"
	next.Shots[0].Image = ""
	next.Shots[0].Candidates = nil
	preserveExtendedAssets(&b, &next)
	if next.Shots[0].Image != b.Shots[0].Image || len(next.Shots[0].Candidates) != 2 {
		t.Fatal("narration invalidated images")
	}
	q := next.Shots[0]
	selected := selectedCandidate(q)
	if e := os.WriteFile(s.abs(selected.Image), []byte("modified externally"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := s.validateBoardMedia(next); e == nil {
		t.Fatal("changed candidate accepted")
	}
	_ = p
}
func TestMigrationFailureLeavesOldState(t *testing.T) {
	dir := t.TempDir()
	db, e := sql.Open("sqlite3", filepath.Join(dir, "studio.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if _, e = db.Exec("CREATE TABLE state (id INTEGER PRIMARY KEY, body TEXT NOT NULL)"); e != nil {
		t.Fatal(e)
	}
	old := `{"projects":{},"quotes":{},"settings":{"providers":[],"bindings":{}}}`
	if _, e = db.Exec("INSERT INTO state VALUES(1,?)", old); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("CREATE TRIGGER reject_upgrade BEFORE UPDATE ON state BEGIN SELECT RAISE(ABORT,'test migration failure'); END"); e != nil {
		t.Fatal(e)
	}
	if store, e := openStore(dir); e == nil {
		store.close()
		t.Fatal("migration failure ignored")
	}
	var got string
	if e = db.QueryRow("SELECT body FROM state WHERE id=1").Scan(&got); e != nil || got != old {
		t.Fatal("partial migration written")
	}
	backups, _ := filepath.Glob(filepath.Join(dir, fmt.Sprintf("studio-before-v%d-*.db", stateVersion)))
	if len(backups) != 1 {
		t.Fatal("missing rollback backup")
	}
}

func TestVideoRedoIntentAndReviewScoring(t *testing.T) {
	b := Board{Schema: 2, Version: 1}
	q := Shot{ID: "s1", Duration: 6, Narration: "测试旁白", Selected: "a", Candidates: []Candidate{{ID: "a", Hash: "image-hash"}}}
	b.Shots = []Shot{q}
	before := selectionHash(b)
	voice := videoIntent(b, q, "tts")
	video := videoIntent(b, q, "video")
	q.VideoGenerationID = "explicitly-confirmed-redo"
	b.Shots[0] = q
	if selectionHash(b) == before || videoIntent(b, q, "video") == video || videoIntent(b, q, "tts") != voice {
		t.Fatal("redo binding or voice reuse broken")
	}
	review := Review{Identity: 100, Style: 100, Composition: 100, Continuity: 100, Technical: 100, IdentityError: true}
	if e := grade(&review); e != nil || review.State != "failed" {
		t.Fatal("hard identity defect ignored")
	}
	review = Review{Identity: 80, Style: 80, Composition: 80, Continuity: 80, Technical: 80}
	if e := grade(&review); e != nil || review.State != "passed" {
		t.Fatal("threshold mismatch")
	}
}

func TestLegacyRequestKeyPreventsDuplicateSubmission(t *testing.T) {
	s := testStudio(t)
	p := addProject(t, s)
	endpoint, count := reviewServer(t, true)
	settings := fakeVision(t, endpoint.URL)
	provider, model, e := selectModel(settings, "vision")
	if e != nil {
		t.Fatal(e)
	}
	prompt := "legacy unknown request"
	key := hash([]any{provider, model, "vision", prompt, []string(nil), 0})
	if e = s.store.update(p.ID, func(p *Project) {
		p.Attempts[key] = Attempt{ID: key, Kind: "vision", State: "submission_unknown", Provider: provider.ID, Model: model.ID}
	}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.request(context.Background(), p.ID, settings, "vision", prompt, nil, 0); e == nil {
		t.Fatal("legacy unknown submission retried")
	}
	if count.Load() != 0 {
		t.Fatal("legacy hash change duplicated payment")
	}
}
