package main

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func referencePNG(t *testing.T, w, h int, seed uint16) []byte {
	t.Helper()
	im := image.NewNRGBA64(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			im.SetNRGBA64(x, y, color.NRGBA64{R: seed + uint16(x), G: uint16(y) * 31, B: 43210, A: 65535})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, im); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestReferencePlanGroupsMoreThanSixAndPreservesEverySource(t *testing.T) {
	s := testStudio(t)
	p := createProject(t, s)
	q := Shot{ID: "target"}
	refs := []Image{}
	originals := [][]byte{}
	for i := 0; i < 9; i++ {
		kind := []string{"character", "scene", "prop"}[i/3]
		b := referencePNG(t, 120+i, 100+i, uint16(i*71))
		originals = append(originals, b)
		file, err := s.writeMedia(p.ID, string(rune('a'+i))+".png", b)
		if err != nil {
			t.Fatal(err)
		}
		im := Image{File: file, Hash: digest(b)}
		id := string(rune('a' + i))
		p.Assets = append(p.Assets, Asset{ID: id, Kind: kind, Name: kind + id, Master: &im})
		q.References = append(q.References, id)
		refs = append(refs, im)
	}
	p.Shots = []Shot{q}
	plan, boards, err := s.prepareReferenceBoards(refs, p, "shot-target-retry")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Boards) != 3 || len(plan.Sources) != 9 {
		t.Fatal("unnecessary boards or dropped source", plan)
	}
	seen := map[int]bool{}
	for i, board := range plan.Boards {
		im, _, e := image.Decode(bytes.NewReader(boards[i]))
		if e != nil {
			t.Fatal(e)
		}
		for a, cell := range board.Cells {
			src := plan.Sources[cell.Source]
			if src.Kind != board.Kind || seen[cell.Source] {
				t.Fatal("mixed category or duplicated source")
			}
			seen[cell.Source] = true
			r := image.Rect(cell.X, cell.Y, cell.X+src.Width, cell.Y+src.Height)
			if !r.In(im.Bounds()) {
				t.Fatal("cropped reference")
			}
			for _, other := range board.Cells[:a] {
				s := plan.Sources[other.Source]
				if r.Overlaps(image.Rect(other.X, other.Y, other.X+s.Width, other.Y+s.Height)) {
					t.Fatal("overlapping references")
				}
			}
			original, _, _ := image.Decode(bytes.NewReader(originals[cell.Source]))
			for y := 0; y < src.Height; y++ {
				for x := 0; x < src.Width; x++ {
					r, g, b, a := original.At(x, y).RGBA()
					rr, gg, bb, aa := im.At(cell.X+x, cell.Y+y).RGBA()
					if r != rr || g != gg || b != bb || a != aa {
						t.Fatal("colour precision, pixels or size changed")
					}
				}
			}
		}
	}
	if len(seen) != 9 {
		t.Fatal("source omitted")
	}
	again, _, err := s.prepareReferenceBoards(refs, p, "shot-target-retry")
	if err != nil || !reflect.DeepEqual(plan, again) {
		t.Fatal("plan is not deterministic", err)
	}
	for i, ref := range refs {
		b, _ := os.ReadFile(s.media(ref.File))
		if !bytes.Equal(b, originals[i]) {
			t.Fatal("original overwritten")
		}
	}
}

func TestReferenceLayoutRejectsOverflowWithoutScalingOrMixing(t *testing.T) {
	sources := make([]ReferenceSource, 6)
	for i := range sources {
		sources[i] = ReferenceSource{Name: "大图", Kind: "character", Width: 3072, Height: 2048}
	}
	if _, err := layoutReferenceBoards(sources); err == nil || !strings.Contains(err.Error(), "主动选择") {
		t.Fatal("overflow not actionable", err)
	}
	sources = sources[:5]
	if boards, err := layoutReferenceBoards(sources); err != nil || len(boards) != 5 {
		t.Fatal("valid five boards rejected", err)
	}
}

func TestQuotePreflightsReferencePlanAndDoesNotChangeProject(t *testing.T) {
	s := testStudio(t)
	p := readyRetryProject(t, s)
	p.Mode = "remote"
	p.AudioMode = "narration"
	p.Shots[0].Narration = "画面"
	p.Shots[0].Candidates = []Image{}
	big := referencePNG(t, 3072, 2048, 5)
	file, err := s.writeMedia(p.ID, "big.png", big)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for i := 0; i < 6; i++ {
		id := "big-" + string(rune('a'+i))
		im := Image{File: file, Hash: digest(big), Approved: true}
		p.Assets = append(p.Assets, Asset{ID: id, Kind: "prop", Name: id, Master: &im})
		ids = append(ids, id)
	}
	p.Shots[0].References = ids
	cfg := Settings{Version: 1, RegistryVersion: 1, Mode: "remote", Vendors: []Vendor{{ID: "local", Name: "Codex", Type: "cli", CLI: "codex", Executable: fakeCLI(t, "fail")}}, Image: Endpoint{VendorID: "local"}}
	before := string(jsonBytes(p))
	var beforeAttempts int
	if err = s.store.db.QueryRow("SELECT count(*) FROM attempts").Scan(&beforeAttempts); err != nil {
		t.Fatal(err)
	}
	if _, err = s.quoteFor(p, cfg, "board_retry", ShotRequest{ShotID: p.Shots[0].ID}); err == nil || !strings.Contains(err.Error(), "主动选择") {
		t.Fatal("bad layout reached quote", err)
	}
	q, err := s.quoteFor(p, cfg, "board_retry", ShotRequest{ShotID: p.Shots[0].ID, ReferenceIDs: ids[:4]})
	if err != nil {
		t.Fatal(err)
	}
	if q.ImageReferencePlan == nil || len(q.ImageReferencePlan.Sources) != 4 || len(q.Items) != 1 || q.Items[0].Count != 1 || len(q.ReferenceIDs) != 4 {
		t.Fatal("incorrect quote scope")
	}
	if string(jsonBytes(p)) != before {
		t.Fatal("quote silently selected references or changed original")
	}
	var attempts int
	if err = s.store.db.QueryRow("SELECT count(*) FROM attempts").Scan(&attempts); err != nil || attempts != beforeAttempts {
		t.Fatal(err)
	}
}

func TestConfirmedReferencePlanMismatchStopsBeforeSubmission(t *testing.T) {
	s := testStudio(t)
	job := cliTask(t, s)
	b := referencePNG(t, 120, 100, 7)
	file, err := s.writeMedia(job.ProjectID, "ref.png", b)
	if err != nil {
		t.Fatal(err)
	}
	refs := []Image{{File: file, Hash: digest(b)}}
	plan, _, err := s.prepareReferenceBoards(refs, job.Input, "shot-one")
	if err != nil {
		t.Fatal(err)
	}
	plan.Boards[0].Hash = "changed"
	job.ImageReferencePlan = &plan
	t.Setenv("CODEX_HOME", t.TempDir())
	if _, err = s.cliImage(context.Background(), &job, "shot-one", "", refs, false, Endpoint{CLI: "codex", Executable: fakeCLI(t, "fail")}); err == nil || !strings.Contains(err.Error(), "报价不一致") {
		t.Fatal("mismatched quote accepted", err)
	}
	var count int
	if err = s.store.db.QueryRow("SELECT count(*) FROM attempts WHERE task_id=?", job.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("attempt written before preflight", err)
	}
	if _, err = os.Stat(filepath.Join(s.data, "cli", job.ID, "shot-one", "runs")); !os.IsNotExist(err) {
		t.Fatal("CLI executed")
	}
}

func TestReferencePlanInConfirmedQuoteCannotBeTampered(t *testing.T) {
	s := testStudio(t)
	p := readyRetryProject(t, s)
	p.Mode = "remote"
	p.Shots[0].Candidates = []Image{}
	if _, err := s.store.db.Exec("UPDATE projects SET body=? WHERE id=?", jsonBytes(p), p.ID); err != nil {
		t.Fatal(err)
	}
	cfg := Settings{Version: 1, RegistryVersion: 1, Mode: "remote", Vendors: []Vendor{{ID: "local", Name: "Codex", Type: "cli", CLI: "codex", Executable: fakeCLI(t, "fail")}}, Image: Endpoint{VendorID: "local"}}
	if _, err := s.store.db.Exec("UPDATE settings SET body=? WHERE id=1", jsonBytes(cfg)); err != nil {
		t.Fatal(err)
	}
	var q Quote
	json.Unmarshal(request(t, s, "POST", "/api/projects/"+p.ID+"/quotes", map[string]any{"revision": p.Revision, "stage": "board_retry", "shot_id": p.Shots[0].ID}, 200), &q)
	if q.ImageReferencePlan == nil {
		t.Fatal("missing quote plan")
	}
	q.ImageReferencePlan.Boards[0].Width++
	if _, err := s.store.db.Exec("UPDATE quotes SET body=? WHERE id=?", jsonBytes(q), q.ID); err != nil {
		t.Fatal(err)
	}
	body := request(t, s, "POST", "/api/quotes/"+q.ID+"/confirm", map[string]bool{"allow_unknown": true}, 400)
	if !strings.Contains(string(body), "重新报价") {
		t.Fatal("plan mismatch not explained", string(body))
	}
}
