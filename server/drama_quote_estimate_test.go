package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDramaVideoEstimateScope(t *testing.T) {
	cfg := estimateSettings("doubao-seedance-2-0-mini-260615")
	w := DramaWork{Aspect: "9:16", Duration: 33}
	first := DramaProduction{Kind: "video", Entry: "MOTION-EP001-001", Duration: 4, Mode: "first_frame"}
	second := DramaProduction{Kind: "video", Entry: "MOTION-EP001-002", Duration: 5, Mode: "first_last"}
	for _, tc := range []struct {
		spec            DramaProduction
		amount, seconds string
	}{
		{first, "0.92", "合计4秒"},
		{DramaProduction{Kind: "batch", Items: []DramaProduction{first, second}}, "2.08", "合计9秒"},
	} {
		q := DramaQuote{Spec: tc.spec, Unknown: true}
		applyDramaVideoEstimate(&q, w, cfg)
		if q.Estimate == nil || q.Estimate.Amount != tc.amount || !q.Unknown || q.EstimateReason != "" || !strings.Contains(q.Estimate.Basis, tc.seconds) || !strings.Contains(q.Estimate.Basis, "496×864") {
			t.Fatalf("incorrect quote scope: %+v", q)
		}
	}
	for _, scenario := range []string{"continue", "proxy", "unknown-model", "mixed", "mock"} {
		config := estimateSettings(cfg.Video.Model)
		q := DramaQuote{Spec: first, Unknown: true}
		switch scenario {
		case "continue":
			q.Spec.Mode = "continue"
		case "proxy":
			config.Vendors[0].Base = "https://proxy.example/v3"
		case "unknown-model":
			config.Video.Model = "ep-custom"
		case "mixed":
			q.Spec = DramaProduction{Kind: "batch", Items: []DramaProduction{first, {Kind: "voice"}}}
		case "mock":
			config.Mode = "mock"
		}
		applyDramaVideoEstimate(&q, w, config)
		if q.Estimate != nil || !q.Unknown {
			t.Fatalf("%s must not acquire a total: %+v", scenario, q)
		}
	}
}

func TestDramaBatchEstimateUnknownItemDoesNotBecomeZero(t *testing.T) {
	q := DramaQuote{Unknown: true, Spec: DramaProduction{Kind: "batch", Items: []DramaProduction{
		{Kind: "video", Entry: "first", Duration: 4, Mode: "first_frame"},
		{Kind: "video", Entry: "second", Duration: 5, Mode: "continue"},
	}}}
	applyDramaVideoEstimate(&q, DramaWork{Aspect: "9:16"}, estimateSettings("doubao-seedance-2-0-mini-260615"))
	if q.Estimate != nil || q.EstimateReason == "" || len(q.ItemEstimates) != 2 || q.ItemEstimates[0].Estimate == nil || q.ItemEstimates[1].Estimate != nil || q.ItemEstimates[1].Reason == "" {
		t.Fatalf("unknown item must prevent a total: %+v", q)
	}
}

func TestDramaVideoQuoteEstimateWireAndConsent(t *testing.T) {
	s := testStudio(t)
	cfg := estimateSettings("doubao-seedance-2-0-mini-260615")
	if _, err := s.store.db.Exec("UPDATE settings SET body=? WHERE id=1", jsonBytes(cfg)); err != nil {
		t.Fatal(err)
	}
	w := dramaTestWork(t, s)
	for _, kind := range []string{"script", "assets", "storyboard", "motion"} {
		dramaTestSave(t, s, w, kind, dramaMock(kind, 4))
	}
	frame := guideReference(t, s, w.ID, "SHOT-EP001-001")
	p := DramaProduction{Kind: "video", Document: "motion", Entry: "MOTION-EP001-001", FrameMediaID: frame.ID, FrameMediaHash: frame.Hash}
	var q DramaQuote
	if err := json.Unmarshal(request(t, s, "POST", "/api/drama/works/"+w.ID+"/quotes", p, 200), &q); err != nil {
		t.Fatal(err)
	}
	if q.Estimate == nil || q.Estimate.Amount != "0.92" || !q.Unknown {
		t.Fatalf("estimate missing from API: %+v", q)
	}
	var raw []byte
	if err := s.store.db.QueryRow("SELECT body FROM drama_quotes WHERE id=?", q.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var saved DramaQuote
	if err := json.Unmarshal(raw, &saved); err != nil || saved.Estimate == nil || saved.Estimate.Amount != q.Estimate.Amount {
		t.Fatal("estimate not saved with quote", err)
	}
	request(t, s, "POST", "/api/drama/quotes/"+q.ID+"/confirm", map[string]any{"allow_unknown": false}, 400)
	for _, table := range []string{"tasks", "attempts"} {
		var n int
		if err := s.store.db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil || n != 0 {
			t.Fatal("quote submitted inference", table, n, err)
		}
	}
}
