package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCLIModelCatalogReadOnlyAndRefresh(t *testing.T) {
	home := filepath.Join(t.TempDir(), "Codex 中文 空格")
	if err := os.MkdirAll(home, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", home)
	path := filepath.Join(home, "models_cache.json")
	write := func(b string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(b), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(`{"fetched_at":"2020-01-01T00:00:00Z","models":[{"slug":"execution-model","display_name":"执行模型","visibility":"list","private":"never-export"},{"slug":"hidden","visibility":"hide"},{"slug":"execution-model","visibility":"list"},{"slug":"bad\nmodel","visibility":"list"}]}`)
	s := testStudio(t)
	cfg, _ := s.store.settings()
	cfg.Vendors = append(cfg.Vendors, Vendor{ID: "cli-models", Name: "Codex", Type: "cli", Kind: "codex", CLI: "codex"})
	request(t, s, "PUT", "/api/settings", cfg, 200)
	var cat ModelCatalog
	b := request(t, s, "GET", "/api/vendors/cli-models/models", nil, 200)
	if err := json.Unmarshal(b, &cat); err != nil {
		t.Fatal(err)
	}
	if len(cat.Models) != 1 || !cat.Stale || !contains(cat.Models[0].Services, "image") || cat.Models[0].Verified || strings.Contains(string(b), "never-export") {
		t.Fatal(string(b))
	}
	write(`{"models":[{"slug":"new-model","visibility":"list"}]}`)
	b = request(t, s, "GET", "/api/vendors/cli-models/models?refresh=1", nil, 200)
	if !strings.Contains(string(b), "new-model") || strings.Contains(string(b), "execution-model") {
		t.Fatal(string(b))
	}
	for _, invalid := range []string{`{`, `{}`, strings.Repeat("x", (8<<20)+1)} {
		write(invalid)
		cat = localCLIModels(cfg.Vendors[len(cfg.Vendors)-1])
		if cat.Error == "" || len(cat.Models) != 0 {
			t.Fatal("invalid cache accepted")
		}
	}
	t.Setenv("CODEX_HOME", t.TempDir())
	if localCLIModels(Vendor{CLI: "codex"}).Error == "" {
		t.Fatal("missing cache ignored")
	}
	cat = localCLIModels(Vendor{Type: "cli", CLI: "claude"})
	if len(cat.Models) != 3 || cat.Updated.Before(time.Now().Add(-time.Minute)) {
		t.Fatal(cat)
	}
	for _, m := range cat.Models {
		if contains(m.Services, "image") || m.Verified {
			t.Fatal(m)
		}
	}
}
