package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"strings"
	"testing"
)

func TestAudioModeDefaultsAndSwitchPreservesImages(t *testing.T) {
	s := testStudio(t)
	p := createProject(t, s)
	if p.AudioMode != "dialogue" {
		t.Fatal("new projects must default to dialogue")
	}
	var legacy Project
	if e := json.Unmarshal([]byte(`{"id":"legacy"}`), &legacy); e != nil || dialogueMode(legacy) {
		t.Fatal("legacy snapshot changed modes", e)
	}
	request(t, s, "POST", "/api/projects", map[string]any{"title": "x", "idea": "x", "audio_mode": "invalid"}, 400)
	var n Project
	json.Unmarshal(request(t, s, "POST", "/api/projects", map[string]any{"title": "x", "idea": "x", "audio_mode": "narration"}, 200), &n)
	if dialogueMode(n) {
		t.Fatal("explicit narrator ignored")
	}
	image := Image{File: "retained.png", Hash: "hash", Approved: true}
	p, e := s.store.update(p.ID, p.Revision, func(p *Project) error {
		p.Analyzed, p.AssetsLocked, p.BoardsLocked = true, true, true
		p.Script = "保留的剧本"
		p.Assets = []Asset{{ID: "cat", Kind: "character", Name: "橘猫", Description: "橘猫", Master: &image}}
		p.Shots = []Shot{{ID: "one", Narration: "橘猫：我的球。", Candidates: []Image{image}, References: []string{"cat"}, Approved: true, Video: "old.mp4"}}
		p.Voice = "old.wav"
		p.Output = "final.mp4"
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	oldRevision := p.Revision
	p.AudioMode = "narration"
	request(t, s, "PUT", "/api/projects/"+p.ID, p, 200)
	p = getProject(t, s, p.ID)
	if p.Revision != oldRevision+1 || !p.AssetsLocked || p.BoardsLocked || p.Script != "保留的剧本" || p.Shots[0].Candidates[0] != image || *p.Assets[0].Master != image {
		t.Fatal("mode switch discarded visual work", p)
	}
	if p.Shots[0].Narration != "" || p.Shots[0].Approved || p.Shots[0].Video != "" || p.Voice != "" || p.Output != "" {
		t.Fatal("mode switch retained stale speech", p)
	}
}

func TestDialoguePayloadAndSpeakerValidation(t *testing.T) {
	p := Project{AudioMode: "dialogue", Assets: []Asset{{ID: "cat", Kind: "character", Name: "橘猫"}}}
	q := Shot{Narration: "橘猫：一起玩吧。", References: []string{"cat"}, Duration: 5}
	if e := validateSpeech(p, q); e != nil {
		t.Fatal(e)
	}
	body := videoRequest(p, q, "model", []byte("png"))
	if body["generate_audio"] != true || !strings.Contains(string(jsonBytes(body)), q.Narration) {
		t.Fatal("missing native dialogue", body)
	}
	q.Narration = "午后的安静被打破了。"
	if validateSpeech(p, q) == nil {
		t.Fatal("unattributed narration accepted as dialogue")
	}
	q.Narration = "旁白：午后很安静。"
	if validateSpeech(p, q) == nil {
		t.Fatal("narrator speaker accepted")
	}
	q.Narration = ""
	if validateSpeech(p, q) != nil {
		t.Fatal("silent beat should be valid")
	}
	p.AudioMode = "narration"
	if videoRequest(p, q, "model", nil)["generate_audio"] != false {
		t.Fatal("legacy request changed")
	}
}

func TestDialogueQuoteDoesNotRequireVoice(t *testing.T) {
	s := testStudio(t)
	p := createProject(t, s)
	p = stage(t, s, p, "analyze")
	p = stage(t, s, p, "masters")
	for _, a := range p.Assets {
		if a.Kind == "character" {
			p = approve(t, s, p, a.ID, "master")
		}
	}
	p = stage(t, s, p, "details")
	for _, a := range p.Assets {
		part := "master"
		if a.Kind == "character" {
			part = "sheet"
		}
		p = approve(t, s, p, a.ID, part)
	}
	p = lock(t, s, p, "assets")
	p = stage(t, s, p, "boards")
	for _, q := range p.Shots {
		request(t, s, "PUT", "/api/projects/"+p.ID+"/shots/"+q.ID, map[string]any{"revision": p.Revision, "selected": 0, "approved": true, "narration": q.Narration}, 200)
		p = getProject(t, s, p.ID)
	}
	p = lock(t, s, p, "boards")
	cfg := defaultSettings()
	quote, e := s.quoteFor(p, cfg, "videos")
	if e != nil || len(quote.Items) != 1 {
		t.Fatal("dialogue charged for voice", quote, e)
	}
	p.AudioMode = "narration"
	quote, e = s.quoteFor(p, cfg, "videos")
	if e != nil || len(quote.Items) != 2 {
		t.Fatal("narration lost voice", quote, e)
	}
}

func TestDialogueAudioExportIntegration(t *testing.T) {
	if os.Getenv("LYPIT_INTEGRATION") != "1" {
		t.Skip("set LYPIT_INTEGRATION=1")
	}
	s := testStudio(t)
	p := createProject(t, s)
	p.Duration = 10
	p.Shots = []Shot{{ID: "1", Duration: 5, Video: p.ID + "/sound.mp4"}, {ID: "2", Duration: 5, Video: p.ID + "/sound.mp4"}}
	if e := os.MkdirAll(s.media(p.ID), 0700); e != nil {
		t.Fatal(e)
	}
	_, e := runCommand(context.Background(), "", "ffmpeg", "-v", "error", "-y", "-f", "lavfi", "-i", "color=c=green:s=360x640:r=24", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000", "-t", "5", "-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac", s.media(p.Shots[0].Video))
	if e != nil {
		t.Fatal(e)
	}
	job := Task{ID: uid(), ProjectID: p.ID, Input: p, Mode: "mock", Status: "running"}
	if e = s.store.saveTask(job); e != nil {
		t.Fatal(e)
	}
	file, e := s.render(context.Background(), &job)
	if e != nil {
		t.Fatal(e)
	}
	b, e := runCommand(context.Background(), "", "ffmpeg", "-v", "error", "-i", s.media(file), "-map", "0:a:0", "-f", "s16le", "-ac", "1", "-ar", "8000", "-")
	if e != nil {
		t.Fatal(e)
	}
	// Measure both clips: a silent AAC track would pass stream detection but fail here.
	for _, offset := range []int{8000, 6 * 8000} {
		var energy float64
		for i := offset * 2; i < (offset+8000)*2; i += 2 {
			v := float64(int16(binary.LittleEndian.Uint16(b[i : i+2])))
			energy += v * v
		}
		if math.Sqrt(energy/8000) < 100 {
			t.Fatal("source audio was lost")
		}
	}
	_, e = runCommand(context.Background(), "", "ffmpeg", "-v", "error", "-y", "-i", s.media(p.Shots[0].Video), "-an", "-c:v", "copy", s.media(p.ID+"/silent.mp4"))
	if e != nil {
		t.Fatal(e)
	}
	if s.requireAudio(context.Background(), p.ID+"/silent.mp4") == nil {
		t.Fatal("silent clip accepted")
	}
}
