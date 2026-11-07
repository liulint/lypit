package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestReferenceBoardsUseOneAPIRequestAndPreserveLegacyPayload(t *testing.T) {
	for _, version := range []int{0, referencePlanVersion} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			s := testStudio(t)
			job := cliTask(t, s)
			job.ReferencePolicyVersion = version
			n := 6
			wantBoards := 6
			if version == referencePlanVersion {
				n = 9
				wantBoards = 3
			}
			refs := []Image{}
			shot := Shot{ID: "api"}
			for i := 0; i < n; i++ {
				b := referencePNG(t, 120, 100, uint16(i))
				file, err := s.writeMedia(job.ProjectID, fmt.Sprintf("original-%d.png", i), b)
				if err != nil {
					t.Fatal(err)
				}
				im := Image{File: file, Hash: digest(b)}
				id := fmt.Sprintf("a%d", i)
				kind := []string{"character", "scene", "prop"}[i%3]
				job.Input.Assets = append(job.Input.Assets, Asset{ID: id, Kind: kind, Name: id, Master: &im})
				shot.References = append(shot.References, id)
				refs = append(refs, im)
			}
			job.Input.Shots = []Shot{shot}
			if version == referencePlanVersion {
				plan, _, err := s.prepareReferenceBoards(refs, job.Input, "shot-api-retry")
				if err != nil {
					t.Fatal(err)
				}
				job.ImageReferencePlan = &plan
			}
			var calls atomic.Int32
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if err := r.ParseMultipartForm(1 << 20); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				if len(r.MultipartForm.File["image[]"]) != wantBoards || r.FormValue("n") != "1" {
					t.Error("changed image scope", len(r.MultipartForm.File["image[]"]), r.FormValue("n"))
				}
				if version == 0 && r.MultipartForm.File["image[]"][0].Filename != "original-0.png" {
					t.Error("historical filename changed")
				}
				json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]string{"b64_json": base64.StdEncoding.EncodeToString(mockPNG("api", false, false))}}})
			}))
			defer api.Close()
			t.Setenv("TEST_REFERENCE_API_KEY", "test-key")
			job.Config = Settings{Image: Endpoint{Base: api.URL, Model: "image", KeyEnv: "TEST_REFERENCE_API_KEY"}}
			for i := 0; i < 2; i++ {
				if _, err := s.remoteImage(context.Background(), &job, "shot-api-retry", "one image", refs, false); err != nil {
					t.Fatal(err)
				}
			}
			if calls.Load() != 1 {
				t.Fatal("request repeated", calls.Load())
			}
		})
	}
}
