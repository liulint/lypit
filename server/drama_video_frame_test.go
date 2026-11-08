package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDramaVideoFrameExplicitQuoteSnapshot(t *testing.T) {
	s := testStudio(t)
	w := dramaTestWork(t, s)
	dramaTestSave(t, s, w, "script", dramaMock("script", 5))
	dramaTestSave(t, s, w, "assets", dramaMock("assets", 5))
	board := dramaTestSave(t, s, w, "storyboard", dramaMock("storyboard", 5))
	motion := dramaTestSave(t, s, w, "motion", dramaMock("motion", 5))
	source := guideReference(t, s, w.ID, "SHOT-EP001-001")
	frame, err := s.padDramaFrame(w, source)
	if err != nil {
		t.Fatal(err)
	}
	frame.Approved = true
	if err = s.saveDramaMedia(frame); err != nil {
		t.Fatal(err)
	}
	url := "/api/drama/works/" + w.ID + "/quotes"
	p := DramaProduction{Kind: "video", Document: "motion", Entry: "MOTION-EP001-001"}
	request(t, s, "POST", url, p, 400) // A plan is never silently replaced by a real image.
	p.FrameMediaID, p.FrameMediaHash = frame.ID, frame.Hash
	var q DramaQuote
	json.Unmarshal(request(t, s, "POST", url, p, 200), &q)
	if q.Spec.Mode != "first_frame" || len(q.Spec.Bindings) != 1 || q.Spec.Bindings[0].Media.ID != frame.ID || q.Inputs["@media:"+frame.ID] != frame.Hash || q.Spec.Duration != 5 {
		t.Fatal(q)
	}
	for kind, hash := range map[string]string{"storyboard": board.Hash, "motion": motion.Hash} {
		d, _ := s.dramaDoc(w.ID, kind)
		if d.Hash != hash || d.State != "accepted" {
			t.Fatal("selection rewrote source", d)
		}
	}
	var count int
	s.store.db.QueryRow("SELECT count(*) FROM tasks").Scan(&count)
	if count != 0 {
		t.Fatal("quote started a task")
	}
	s.store.db.QueryRow("SELECT count(*) FROM attempts").Scan(&count)
	if count != 0 {
		t.Fatal("quote called model")
	}
	// Revocation invalidates the quote before any task can be started.
	frame.Approved = false
	s.saveDramaMedia(frame)
	request(t, s, "POST", "/api/drama/quotes/"+q.ID+"/confirm", map[string]any{"allow_unknown": true}, 409)
	request(t, s, "POST", url, p, 400)
	frame.Approved = true
	frame.Hash = strings.Repeat("0", 64)
	s.saveDramaMedia(frame)
	request(t, s, "POST", url, p, 400) // UI snapshot changed before quoting.
}

func TestDramaVideoFrameSelectionBoundaries(t *testing.T) {
	s := testStudio(t)
	w := dramaTestWork(t, s)
	other := dramaTestWork(t, s)
	m := guideReference(t, s, w.ID, "SHOT-EP001-001")
	foreign := guideReference(t, s, other.ID, "SHOT-EP001-001")
	p := DramaProduction{Kind: "video", Document: "motion", Entry: "MOTION-EP001-001", FrameMediaID: m.ID, FrameMediaHash: m.Hash}
	entry := DramaEntry{ID: p.Entry, Fields: map[string]string{"输入参考图": "PLAN-START（顺序：1）· 本镜《画面》（用途：起始帧；控制：构图；不得控制：终态）"}}
	if _, err := s.selectedDramaVideoFrame(w.ID, p, entry); err != nil {
		t.Fatal(err)
	}
	for _, decl := range []string{"无（明确选择文生视频）", "REF-START", entry.Fields["输入参考图"] + " PLAN-END（用途：结束帧）", "PLAN-IDENTITY（用途：身份）", entry.Fields["输入参考图"] + "；图片（顺序：2；用途：身份）", strings.Replace(entry.Fields["输入参考图"], "顺序：1）", "顺序：10）", 1)} {
		bad := entry
		bad.Fields = map[string]string{"输入参考图": decl}
		if _, err := s.selectedDramaVideoFrame(w.ID, p, bad); err == nil {
			t.Fatal("overrode different reference plan", decl)
		}
	}
	for _, id := range []string{foreign.ID, strings.Repeat("a", 24)} {
		bad := p
		bad.FrameMediaID = id
		if _, err := s.selectedDramaVideoFrame(w.ID, bad, entry); err == nil {
			t.Fatal("foreign/missing media accepted")
		}
	}
	bad := p
	bad.Mode = "continue"
	if _, err := s.selectedDramaVideoFrame(w.ID, bad, entry); err == nil {
		t.Fatal("continuation replaced")
	}
	m.Entry = "SHOT-EP001-002"
	s.saveDramaMedia(m)
	if _, err := s.selectedDramaVideoFrame(w.ID, p, entry); err == nil {
		t.Fatal("wrong shot accepted")
	}
	m.Entry = "SHOT-EP001-001"
	m.TaskID = strings.Repeat("b", 24)
	s.saveDramaMedia(m)
	if _, err := s.selectedDramaVideoFrame(w.ID, p, entry); err == nil {
		t.Fatal("stale media accepted")
	}
}
