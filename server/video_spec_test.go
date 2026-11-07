package main

import (
	"encoding/json"
	"testing"
)

func TestVideoSpecDefaultsAndPersistence(t *testing.T) {
	s := testStudio(t)
	p := createProject(t, s)
	if p.AspectRatio != "9:16" || p.Duration != 60 {
		t.Fatal("incorrect defaults", p.AspectRatio, p.Duration)
	}
	var old Project
	if e := json.Unmarshal([]byte(`{"id":"old","revision":1}`), &old); e != nil {
		t.Fatal(e)
	}
	if old.AspectRatio != "9:16" || old.Duration != 60 {
		t.Fatal("old project spec changed")
	}
	var horizontal Project
	_ = json.Unmarshal(request(t, s, "POST", "/api/projects", map[string]any{"title": "横屏测试", "idea": "故事", "aspect_ratio": "16:9", "duration": 93}, 200), &horizontal)
	stored := getProject(t, s, horizontal.ID)
	if stored.AspectRatio != "16:9" || stored.Duration != 93 {
		t.Fatal("spec not persisted")
	}
	q, e := s.quoteFor(stored, defaultSettings(), "analyze")
	if e != nil || q.AspectRatio != "16:9" || q.Duration != 93 {
		t.Fatal("quote spec lost", e, q)
	}
}
func TestVideoSpecRejectsInvalidInput(t *testing.T) {
	s := testStudio(t)
	for _, spec := range []map[string]any{{"aspect_ratio": "1:1", "duration": 60}, {"aspect_ratio": "", "duration": 60}, {"aspect_ratio": "9:16", "duration": 0}, {"duration": 9}, {"duration": 181}, {"duration": 20.5}} {
		spec["title"] = "错误规格"
		spec["idea"] = "故事"
		request(t, s, "POST", "/api/projects", spec, 400)
	}
}
func TestChangingSpecInvalidatesAnalysisAndQuote(t *testing.T) {
	s := testStudio(t)
	p := createProject(t, s)
	p = stage(t, s, p, "analyze")
	var q Quote
	_ = json.Unmarshal(request(t, s, "POST", "/api/projects/"+p.ID+"/quotes", map[string]any{"revision": p.Revision, "stage": "masters"}, 200), &q)
	p.AspectRatio = "16:9"
	p.Duration = 30
	request(t, s, "PUT", "/api/projects/"+p.ID, p, 200)
	got := getProject(t, s, p.ID)
	if got.Analyzed || got.AssetsLocked || got.BoardsLocked || len(got.Assets) > 0 || len(got.Shots) > 0 || got.Script != "" || got.AspectRatio != "16:9" || got.Duration != 30 {
		t.Fatal("stale production survived spec change")
	}
	request(t, s, "POST", "/api/quotes/"+q.ID+"/confirm", map[string]bool{}, 400)
	history, e := s.store.history(p.ID)
	if e != nil || history[1].Duration != 60 || history[1].AspectRatio != "9:16" {
		t.Fatal("prior spec lost", e)
	}
}
func TestShotDurationsMatchEverySupportedDuration(t *testing.T) {
	for duration := 10; duration <= 180; duration++ {
		p := Project{Duration: duration}
		ds := shotDurations(p)
		sum := 0
		for _, d := range ds {
			if d < 5 || d > 8 {
				t.Fatalf("%ds: invalid clip %d", duration, d)
			}
			sum += d
		}
		if sum != duration || len(ds) != (duration+7)/8 {
			t.Fatalf("%ds: wrong schedule %v", duration, ds)
		}
	}
	for i, d := range shotDurations(Project{}) {
		if d != 8-i%2 {
			t.Fatal("default plan changed")
		}
	}
	w, h := frameSize(Project{AspectRatio: "16:9"})
	if w != 640 || h != 360 {
		t.Fatal("landscape size wrong")
	}
}
