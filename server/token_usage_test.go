package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIUsageStreamChunksAndUnknowns(t *testing.T) {
	w := &cliUsageWriter{}
	stream := "{\"type\":\"item.completed\",\"secret\":\"private\"}\n" +
		"{\"type\":\"turn.completed\",\"usage\":{\"input_tokens\":120,\"cached_input_tokens\":100,\"output_tokens\":10}}\n" +
		"{\"type\":\"turn.completed\",\"usage\":{\"input_tokens\":3,\"output_tokens\":0}}"
	for i := 0; i < len(stream); i += 3 {
		end := i + 3
		if end > len(stream) {
			end = len(stream)
		}
		w.Write([]byte(stream[i:end]))
	}
	w.finish()
	if w.counts == nil || *w.counts.Input != 123 || *w.counts.Output != 10 || *w.counts.Cached != 100 || *w.counts.Total != 133 {
		t.Fatalf("bad counts: %+v", w.counts)
	}
	if strings.Contains(string(jsonBytes(w.counts)), "private") {
		t.Fatal("raw event persisted")
	}
	w = &cliUsageWriter{}
	w.Write([]byte(strings.Repeat("x", 3<<20) + "\n{\"type\":\"turn.completed\",\"usage\":{\"input_tokens\":-1,\"output_tokens\":0}}\n"))
	if w.counts == nil || w.counts.Input != nil || *w.counts.Output != 0 {
		t.Fatal("negative or zero mishandled")
	}
}

func TestUsagePublicSnapshotAndRecovery(t *testing.T) {
	s := testStudio(t)
	job := cliTask(t, s)
	job.Stage = "analyze"
	path := fakeCLI(t, "text-usage")
	job.Config = Settings{RegistryVersion: 1, Vendors: []Vendor{{ID: "cli", Name: "原厂商", Type: "cli", CLI: "codex", Executable: path}}, Text: Endpoint{VendorID: "cli", Model: "quoted-model"}}
	if _, err := s.store.db.Exec("UPDATE tasks SET body=?,config=? WHERE id=?", jsonBytes(job), jsonBytes(job.Config), job.ID); err != nil {
		t.Fatal(err)
	}
	ep, err := resolveEndpoint(job.Config, "text")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		var out map[string]bool
		if err := s.cliTextJSON(context.Background(), &job, "analyze", "private prompt", ep, &out); err != nil || !out["ok"] {
			t.Fatal(err)
		}
	}
	dir := filepath.Join(s.data, "cli", job.ID, "analyze")
	runs, _ := os.ReadFile(filepath.Join(dir, "runs"))
	if string(runs) != "run" {
		t.Fatal("reexecuted CLI", string(runs))
	}
	public := request(t, s, "GET", "/api/usage", nil, 200)
	var records []UsageRecord
	if err := json.Unmarshal(public, &records); err != nil || len(records) != 1 {
		t.Fatal(string(public), err)
	}
	u := records[0]
	if u.Vendor != "原厂商" || u.Model != "quoted-model" || u.Input == nil || *u.Input != 120 || *u.Output != 10 || u.Cost != nil {
		t.Fatalf("incorrect public record: %+v", u)
	}
	if strings.Contains(string(public), path) || strings.Contains(string(public), "private prompt") || strings.Contains(string(public), "key_env") {
		t.Fatal("private configuration leaked")
	}
	// A previously saved API response supplies real usage without another model call.
	job.Config = Settings{RegistryVersion: 1, Vendors: []Vendor{{ID: "api", Name: "原API", Type: "api", Protocol: "openai", Base: "https://example.com", KeyEnv: "PRIVATE_KEY"}}, Text: Endpoint{VendorID: "api", Model: "requested-model"}}
	if _, err := s.store.db.Exec("UPDATE tasks SET config=? WHERE id=?", jsonBytes(job.Config), job.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.db.Exec("UPDATE attempts SET response=? WHERE task_id=?", []byte(`{"model":"actual-model","usage":{"prompt_tokens":50,"completion_tokens":0,"total_tokens":50,"prompt_tokens_details":{"cached_tokens":20}},"secret":"never expose"}`), job.ID); err != nil {
		t.Fatal(err)
	}
	records, err = s.usageRecords()
	if err != nil || len(records) != 1 {
		t.Fatal(err)
	}
	u = records[0]
	if u.Model != "actual-model" || *u.Input != 50 || *u.Output != 0 || *u.Cached != 20 || *u.Total != 50 || u.Cost != nil {
		t.Fatalf("API usage lost: %+v", u)
	}
	if strings.Contains(string(jsonBytes(records)), "never expose") {
		t.Fatal("response leaked")
	}
}
