package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDramaBatchVideoSelectedFrames(t *testing.T) {
	s := testStudio(t)
	if _, err := s.store.db.Exec("UPDATE settings SET body=? WHERE id=1", jsonBytes(estimateSettings("doubao-seedance-2-0-mini-260615"))); err != nil {
		t.Fatal(err)
	}
	w := dramaTestWork(t, s)
	dramaTestSave(t, s, w, "script", dramaMock("script", 5))
	dramaTestSave(t, s, w, "assets", dramaMock("assets", 5))
	board := dramaMock("storyboard", 5)
	motion := dramaMock("motion", 5)
	board += "\n" + strings.ReplaceAll(board[strings.Index(board, "## SHOT-"):], "EP001-001", "EP001-002")
	motion += "\n" + strings.ReplaceAll(motion[strings.Index(motion, "## MOTION-"):], "EP001-001", "EP001-002")
	dramaTestSave(t, s, w, "storyboard", board)
	dramaTestSave(t, s, w, "motion", motion)
	frames := []DramaFrameSelection{}
	for _, id := range []string{"001", "002"} {
		source := guideReference(t, s, w.ID, "SHOT-EP001-"+id)
		m, err := s.padDramaFrame(w, source)
		if err != nil {
			t.Fatal(err)
		}
		m.Approved = true
		s.saveDramaMedia(m)
		frames = append(frames, DramaFrameSelection{Entry: "MOTION-EP001-" + id, MediaID: m.ID, Hash: m.Hash})
	}
	p := DramaProduction{Kind: "video", Document: "motion", Entry: "*", FrameSelections: frames}
	url := "/api/drama/works/" + w.ID + "/quotes"
	var q DramaQuote
	json.Unmarshal(request(t, s, "POST", url, p, 200), &q)
	if q.Spec.Kind != "batch" || len(q.Spec.Items) != 2 {
		t.Fatal(q)
	}
	if q.Estimate == nil || q.Estimate.Amount != "2.32" || len(q.ItemEstimates) != 2 || !q.Unknown {
		t.Fatalf("missing batch costs: %+v", q)
	}
	for i, item := range q.ItemEstimates {
		if item.Entry != frames[i].Entry || item.Estimate == nil || item.Estimate.Amount != "1.16" {
			t.Fatalf("incorrect per-shot cost: %+v", item)
		}
	}
	var raw []byte
	if err := s.store.db.QueryRow("SELECT body FROM drama_quotes WHERE id=?", q.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var saved DramaQuote
	if err := json.Unmarshal(raw, &saved); err != nil || len(saved.ItemEstimates) != 2 || saved.Estimate == nil || saved.Estimate.Amount != "2.32" {
		t.Fatal("batch cost snapshot not persisted", err)
	}
	request(t, s, "POST", "/api/drama/quotes/"+q.ID+"/confirm", map[string]any{"allow_unknown": false}, 400)
	for i, item := range q.Spec.Items {
		if item.Mode != "first_frame" || item.Duration != 5 || item.FrameMediaID != frames[i].MediaID || len(item.Bindings) != 1 || item.Bindings[0].Media.Hash != frames[i].Hash || q.Inputs["@media:"+frames[i].MediaID] != frames[i].Hash {
			t.Fatal(item)
		}
	}
	var n int
	s.store.db.QueryRow("SELECT count(*) FROM tasks").Scan(&n)
	if n != 0 {
		t.Fatal("quote submitted tasks")
	}
	s.store.db.QueryRow("SELECT count(*) FROM attempts").Scan(&n)
	if n != 0 {
		t.Fatal("quote called model")
	}
	// Missing, repeated, foreign-shot or stale snapshots never produce a partial quote.
	for _, fs := range [][]DramaFrameSelection{frames[:1], {frames[0], frames[0]}, {{Entry: frames[0].Entry, MediaID: frames[1].MediaID, Hash: frames[1].Hash}, frames[1]}, {{Entry: frames[0].Entry, MediaID: frames[0].MediaID, Hash: "stale"}, frames[1]}} {
		bad := p
		bad.FrameSelections = fs
		request(t, s, "POST", url, bad, 400)
	}
	// A revoked frame invalidates the entire previously reviewed quote.
	m, _ := s.dramaMediaByID(w.ID, frames[1].MediaID)
	m.Approved = false
	s.saveDramaMedia(m)
	request(t, s, "POST", "/api/drama/quotes/"+q.ID+"/confirm", map[string]any{"allow_unknown": true}, 409)
	m.Approved = true
	s.saveDramaMedia(m)
	// Existing current video is skipped, even when still awaiting review; the UI must refresh its scope.
	s.saveDramaMedia(DramaMedia{ID: uid(), WorkID: w.ID, Entry: "MOTION-EP001-002", Kind: "video"})
	request(t, s, "POST", url, p, 400)
	p.FrameSelections = frames[:1]
	json.Unmarshal(request(t, s, "POST", url, p, 200), &q)
	if len(q.Spec.Items) != 1 || q.Spec.Items[0].Entry != frames[0].Entry {
		t.Fatal("existing video regenerated", q)
	}
	bad := p
	bad.Kind = "image"
	request(t, s, "POST", url, bad, 400)
	bad = p
	bad.Entry = frames[0].Entry
	request(t, s, "POST", url, bad, 400)
}
