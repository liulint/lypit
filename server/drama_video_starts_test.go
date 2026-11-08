package main

import (
	"context"
	"encoding/json"
	"image"
	"os"
	"strings"
	"testing"
)

func TestDramaStartRecommendation(t *testing.T) {
	previous := DramaEntry{ID: "SHOT-1", Fields: map[string]string{"来源": "EP001-SC001", "景别/机位": "中景，固定"}}
	current := DramaEntry{ID: "SHOT-2", Fields: map[string]string{"来源": "EP001-SC001", "景别/机位": "中景，固定", "与上一镜关系": "连续接拍"}}
	if mode, _ := dramaStartRecommendation(previous, current); mode != "tail" {
		t.Fatal(mode)
	}
	for _, key := range []string{"来源", "景别/机位", "与上一镜关系"} {
		copy := current
		copy.Fields = map[string]string{}
		for k, v := range current.Fields {
			copy.Fields[k] = v
		}
		copy.Fields[key] = "不同"
		if mode, _ := dramaStartRecommendation(previous, copy); mode != "board" {
			t.Fatal(key, mode)
		}
	}
	if mode, _ := dramaStartRecommendation(DramaEntry{}, current); mode != "board" {
		t.Fatal(mode)
	}
}

func TestDramaTailStartPlanQuoteAndInvalidation(t *testing.T) {
	s := testStudio(t)
	w := dramaTestWork(t, s)
	dramaTestSave(t, s, w, "script", dramaMock("script", 5))
	dramaTestSave(t, s, w, "assets", dramaMock("assets", 5))
	b := dramaMock("storyboard", 5)
	b += "\n" + strings.ReplaceAll(b[strings.Index(b, "## SHOT-"):], "EP001-001", "EP001-002")
	b = strings.Replace(b, "## SHOT-EP001-002 · 让出暖处", "## SHOT-EP001-002 · 让出暖处\n- 与上一镜关系：连续接拍", 1)
	board := dramaTestSave(t, s, w, "storyboard", b)
	m := dramaMock("motion", 5)
	m += "\n" + strings.ReplaceAll(m[strings.Index(m, "## MOTION-"):], "EP001-001", "EP001-002")
	motion := dramaTestSave(t, s, w, "motion", m)
	url := "/api/drama/works/" + w.ID + "/video-starts"
	body := map[string]any{"motion_base": motion.Hash, "board_base": board.Hash}
	read := func() []DramaVideoStart {
		t.Helper()
		var rows []DramaVideoStart
		if err := json.Unmarshal(request(t, s, "POST", url, body, 200), &rows); err != nil {
			t.Fatal(err)
		}
		return rows
	}
	rows := read()
	if len(rows) != 2 || rows[1].Mode != "tail" || rows[1].Waiting == "" || rows[1].Frame != nil {
		t.Fatal(rows)
	}
	// An explicit override persists across reads, including while the predecessor is absent.
	body["choices"] = []map[string]string{{"entry": rows[1].Entry, "mode": "board"}}
	read()
	delete(body, "choices")
	if rows = read(); rows[1].Choice != "board" || rows[1].Mode != "board" {
		t.Fatal(rows)
	}
	body["choices"] = []map[string]string{{"entry": rows[1].Entry, "mode": "auto"}}
	read()
	delete(body, "choices")
	file, err := s.writeMedia(w.ID, "tail-source.mp4", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Three red frames followed by exactly one blue frame. -0.1s would select red.
	_, err = runCommand(context.Background(), s.root, "ffmpeg", "-v", "error", "-y", "-f", "lavfi", "-i", "color=red:s=90x160:r=25:d=0.12", "-f", "lavfi", "-i", "color=blue:s=90x160:r=25:d=0.04", "-filter_complex", "[0:v][1:v]concat=n=2:v=1:a=0[out]", "-map", "[out]", "-c:v", "libx264", "-pix_fmt", "yuv420p", s.media(file))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(s.media(file))
	if err != nil {
		t.Fatal(err)
	}
	source := DramaMedia{ID: uid(), WorkID: w.ID, Entry: "MOTION-EP001-001", Kind: "video", File: file, Hash: digest(data), Approved: true}
	if err = s.saveDramaMedia(source); err != nil {
		t.Fatal(err)
	}
	if _, err = s.store.db.Exec("INSERT INTO drama_video_choices VALUES(?,?,?) ON CONFLICT(work_id,entry) DO UPDATE SET media_id=excluded.media_id", w.ID, source.Entry, source.ID); err != nil {
		t.Fatal(err)
	}
	rows = read()
	tail := rows[1]
	if tail.Frame == nil || tail.Source == nil || tail.Waiting != "" {
		t.Fatal(rows)
	}
	f, err := os.Open(s.media(tail.Frame.File))
	if err != nil {
		t.Fatal(err)
	}
	im, _, err := image.Decode(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	r, _, blue, _ := im.At(45, 80).RGBA()
	if r > 10000 || blue < 50000 {
		t.Fatalf("not the actual last frame: red=%d blue=%d", r, blue)
	}
	if again := read(); again[1].Frame.ID != tail.Frame.ID || again[1].Frame.Hash != tail.Frame.Hash {
		t.Fatal("non-idempotent extraction")
	}
	p := DramaProduction{Kind: "video", Document: "motion", Entry: tail.Entry, StartPlanVersion: 1, TailVideoID: source.ID, TailVideoHash: source.Hash, FrameMediaID: tail.Frame.ID, FrameMediaHash: tail.Frame.Hash}
	var q DramaQuote
	if err = json.Unmarshal(request(t, s, "POST", "/api/drama/works/"+w.ID+"/quotes", p, 200), &q); err != nil {
		t.Fatal(err)
	}
	if q.Spec.Mode != "first_frame" || q.Spec.Source != nil || q.Spec.TailSource == nil || len(q.Spec.Bindings) != 1 || q.Inputs["@tail-video:"+source.ID] != source.Hash {
		t.Fatal(q)
	}
	var tasks, attempts int
	s.store.db.QueryRow("SELECT count(*) FROM tasks").Scan(&tasks)
	s.store.db.QueryRow("SELECT count(*) FROM attempts").Scan(&attempts)
	if tasks != 0 || attempts != 0 {
		t.Fatal("preview or quote called a model")
	}
	bad := p
	bad.FrameMediaHash = "changed"
	request(t, s, "POST", "/api/drama/works/"+w.ID+"/quotes", bad, 400)
	bad = p
	bad.Entry = source.Entry
	request(t, s, "POST", "/api/drama/works/"+w.ID+"/quotes", bad, 400)
	bad = p
	bad.Mode = "continue"
	request(t, s, "POST", "/api/drama/works/"+w.ID+"/quotes", bad, 400)
	// Execute a separate frozen quote through the normal mock pipeline, including recovery.
	var runQuote DramaQuote
	json.Unmarshal(request(t, s, "POST", "/api/drama/works/"+w.ID+"/quotes", p, 200), &runQuote)
	var task Task
	json.Unmarshal(request(t, s, "POST", "/api/drama/quotes/"+runQuote.ID+"/confirm", map[string]bool{"allow_unknown": true}, 200), &task)
	s.wg.Wait()
	finished, err := s.store.task(task.ID)
	if err != nil || finished.Status != "completed" {
		t.Fatal("tail generation failed", finished, err)
	}
	medias, err := s.dramaMedias(w.ID)
	if err != nil {
		t.Fatal(err)
	}
	var generated DramaMedia
	for _, media := range medias {
		if media.Kind == "video" && media.Entry == p.Entry {
			generated = media
		}
	}
	if generated.ID == "" || !generated.Current {
		t.Fatal("tail output missing", medias)
	}
	request(t, s, "POST", "/api/drama/jobs/"+task.ID+"/recover", map[string]any{}, 200)
	s.wg.Wait()
	var after int
	s.store.db.QueryRow("SELECT count(*) FROM tasks").Scan(&after)
	if after != 1 {
		t.Fatal("recovery created another task", after)
	}
	// Switching the preceding shot invalidates both the unused quote and the already generated dependent video.
	if _, err = s.store.db.Exec("UPDATE drama_video_choices SET media_id=? WHERE work_id=? AND entry=?", uid(), w.ID, source.Entry); err != nil {
		t.Fatal(err)
	}
	request(t, s, "POST", "/api/drama/quotes/"+q.ID+"/confirm", map[string]bool{"allow_unknown": true}, 409)
	if s.dramaMediaCurrent(generated) {
		t.Fatal("dependent output remained current after source switched")
	}
	if rows = read(); rows[1].Frame != nil || rows[1].Waiting == "" {
		t.Fatal("stale source became ready", rows)
	}
	body["motion_base"] = "stale"
	request(t, s, "POST", url, body, 409)
}

func TestDramaReadyBatchScope(t *testing.T) {
	s := testStudio(t)
	w := dramaTestWork(t, s)
	dramaTestSave(t, s, w, "script", dramaMock("script", 5))
	dramaTestSave(t, s, w, "assets", dramaMock("assets", 5))
	b := dramaMock("storyboard", 5)
	b += "\n" + strings.ReplaceAll(b[strings.Index(b, "## SHOT-"):], "EP001-001", "EP001-002")
	dramaTestSave(t, s, w, "storyboard", b)
	m := dramaMock("motion", 5)
	m += "\n" + strings.ReplaceAll(m[strings.Index(m, "## MOTION-"):], "EP001-001", "EP001-002")
	dramaTestSave(t, s, w, "motion", m)
	frame, err := s.padDramaFrame(w, guideReference(t, s, w.ID, "SHOT-EP001-001"))
	if err != nil {
		t.Fatal(err)
	}
	frame.Approved = true
	s.saveDramaMedia(frame)
	p := DramaProduction{Kind: "video", Document: "motion", Entry: "*", StartPlanVersion: 1, Entries: []string{"MOTION-EP001-001"}, FrameSelections: []DramaFrameSelection{{Entry: "MOTION-EP001-001", MediaID: frame.ID, Hash: frame.Hash}}}
	var q DramaQuote
	json.Unmarshal(request(t, s, "POST", "/api/drama/works/"+w.ID+"/quotes", p, 200), &q)
	if len(q.Spec.Items) != 1 || q.Spec.Items[0].Entry != p.Entries[0] {
		t.Fatal("waiting shot included", q)
	}
	p.Entries = append(p.Entries, "MOTION-EP001-002")
	request(t, s, "POST", "/api/drama/works/"+w.ID+"/quotes", p, 400)
}
