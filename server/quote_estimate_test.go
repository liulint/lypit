package main

import (
	"strings"
	"testing"
)

func estimateSettings(model string) Settings {
	return Settings{Mode: "remote", RegistryVersion: 1, Video: Endpoint{VendorID: "ark", Model: model}, Vendors: []Vendor{{ID: "ark", Type: "api", Kind: "ark", Protocol: "ark", Base: "https://ark.cn-beijing.volces.com/api/v3", KeyEnv: "ARK_API_KEY"}}}
}

func TestVideoEstimateRatesAndScope(t *testing.T) {
	for _, tc := range []struct{ model, want string }{
		{"doubao-seedance-2-0-260128", "3.23"},
		{"doubao-seedance-2-0-fast-260128", "2.60"},
		{"doubao-seedance-2-0-mini-260615", "1.62"},
		{"doubao-seedance-2-5-260628", "4.71"},
		{"doubao-seedance-1-0-pro-250528", "1.02"},
	} {
		p := Project{AudioMode: "dialogue", Shots: []Shot{{ID: "a", Duration: 7}, {ID: "b", Duration: 8}}}
		q := Quote{Stage: "shot_video", Unknown: true, VideoInputs: []VideoInput{{ShotID: "a", Mode: "first_frame"}}}
		applyVideoEstimate(&q, p, estimateSettings(tc.model))
		if q.Estimate == nil || q.Estimate.Amount != tc.want || !q.Unknown || q.EstimateReason != "" || !strings.Contains(q.Estimate.Basis, "合计7秒") {
			t.Fatalf("%s: %+v", tc.model, q)
		}
		q.Stage = "videos"
		q.Estimate = nil
		applyVideoEstimate(&q, p, estimateSettings(tc.model))
		if q.Estimate == nil || q.Estimate.Amount != tc.want {
			t.Fatal("batch charged for an existing video outside input plan", q)
		}
	}
}

func TestVideoEstimateUnverifiedCostsStayUnknown(t *testing.T) {
	for _, scenario := range []string{"proxy", "cli", "custom-id", "future-model", "continue", "edit_video", "voice", "mock"} {
		cfg := estimateSettings("doubao-seedance-2-0-260128")
		p := Project{AudioMode: "dialogue", Shots: []Shot{{ID: "a", Duration: 7}}}
		q := Quote{Stage: "videos", Unknown: true, VideoInputs: []VideoInput{{ShotID: "a", Mode: "first_frame"}}}
		switch scenario {
		case "proxy":
			cfg.Vendors[0].Base = "https://proxy.example/v3"
		case "cli":
			cfg.Vendors[0].Type = "cli"
		case "custom-id":
			cfg.Video.Model = "ep-custom"
		case "future-model":
			cfg.Video.Model = "doubao-seedance-2-0-299999"
		case "continue", "edit_video":
			q.VideoInputs[0].Mode = scenario
		case "voice":
			p.AudioMode = "narration"
		case "mock":
			cfg.Mode = "mock"
		}
		applyVideoEstimate(&q, p, cfg)
		if q.Estimate != nil || !q.Unknown || (scenario != "mock" && q.EstimateReason == "") {
			t.Fatalf("unverified %s acquired estimate: %+v", scenario, q)
		}
	}
}

func TestShotVideoQuoteIncludesEstimate(t *testing.T) {
	s := testStudio(t)
	p := approveFirstVideoShot(t, s, readyRetryProject(t, s))
	p.Mode = "remote"
	t.Setenv("ARK_API_KEY", "test-key")
	q, err := s.quoteFor(p, estimateSettings("doubao-seedance-2-0-260128"), "shot_video", ShotRequest{ShotID: p.Shots[0].ID})
	if err != nil || q.Estimate == nil || !q.Unknown {
		t.Fatal("quote did not include a consent-gated estimate", q, err)
	}
}
