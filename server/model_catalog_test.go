package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type catalogRoundTrip func(*http.Request) (*http.Response, error)

func (f catalogRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func catalogResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
}
func TestModelCatalogProviderAdapters(t *testing.T) {
	ark := Vendor{ID: "ark", Type: "api", Kind: "ark", Protocol: "ark", Base: "https://ark.example/api/v3"}
	cat, e := fetchModelCatalog(context.Background(), ark, "test-secret", catalogRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" || r.URL.Path != "/api/v3/models" || r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Fatal("incorrect metadata request", r.Method, r.URL)
		}
		return catalogResponse(200, `{"data":[{"id":"doubao-seed-text","domain":"LLM"},{"id":"doubao-seedance-1","domain":"VideoGeneration"},{"id":"doubao-seedream-4","domain":"ImageGeneration"},{"id":"new-unknown"}]}`), nil
	}))
	if e != nil || len(cat.Models) != 4 {
		t.Fatal(cat, e)
	}
	expected := map[string]string{"doubao-seed-text": "text", "doubao-seedance-1": "video"}
	for _, m := range cat.Models {
		if cap := expected[m.ID]; cap != "" && !contains(m.Services, cap) {
			t.Fatal(m)
		}
		if (m.ID == "doubao-seedream-4" || m.ID == "new-unknown") && len(m.Services) > 0 {
			t.Fatal("unimplemented model enabled", m)
		}
	}
	bailian := Vendor{ID: "q", Type: "api", Kind: "bailian", Protocol: "openai", Base: "https://unused.example/v1", Workspace: "workspace-test", Region: "cn-beijing"}
	pages := 0
	cat, e = fetchModelCatalog(context.Background(), bailian, "key", catalogRoundTrip(func(r *http.Request) (*http.Response, error) {
		pages++
		if r.URL.Host != "workspace-test.cn-beijing.maas.aliyuncs.com" || r.URL.Path != "/api/v1/models" || r.URL.Query().Get("page_size") != "100" {
			t.Fatal(r.URL)
		}
		if pages == 1 {
			return catalogResponse(200, `{"output":{"total":101,"models":[{"model":"qwen-text","name":"千问文本","capabilities":["TG"]}]}}`), nil
		}
		if r.URL.Query().Get("page_no") != "2" {
			t.Fatal(r.URL)
		}
		return catalogResponse(200, `{"output":{"total":101,"models":[{"model":"qwen-image","capabilities":["IG"]}]}}`), nil
	}))
	if e != nil || pages != 2 || len(cat.Models) != 2 {
		t.Fatal(pages, cat, e)
	}
	for _, m := range cat.Models {
		if m.ID == "qwen-image" && len(m.Services) != 0 {
			t.Fatal("native image API falsely enabled", m)
		}
	}
}
func TestCatalogErrorsAndNoCredentialRedirect(t *testing.T) {
	v := Vendor{Type: "api", Kind: "custom", Protocol: "openai", Base: "https://catalog.example/v1"}
	for _, tc := range []struct {
		status int
		body   string
	}{{401, `{"error":"private-secret"}`}, {200, `{`}, {200, `{}`}, {200, `{"has_more":true,"data":[]}`}, {200, strings.Repeat("x", (4<<20)+1)}} {
		_, e := fetchModelCatalog(context.Background(), v, "private-secret", catalogRoundTrip(func(*http.Request) (*http.Response, error) { return catalogResponse(tc.status, tc.body), nil }))
		if e == nil || strings.Contains(e.Error(), "private-secret") {
			t.Fatal("bad error handling", e)
		}
	}
	calls := 0
	_, e := fetchModelCatalog(context.Background(), v, "private-secret", catalogRoundTrip(func(*http.Request) (*http.Response, error) {
		calls++
		r := catalogResponse(302, "")
		r.Header.Set("Location", "https://other.example/models")
		return r, nil
	}))
	if e == nil || calls != 1 {
		t.Fatal("followed credential redirect", calls, e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, e = fetchModelCatalog(ctx, v, "key", http.DefaultTransport)
	if e == nil {
		t.Fatal("cancel ignored")
	}
}
func TestCatalogCacheIsolationStaleAndQuoteVersion(t *testing.T) {
	s := testStudio(t)
	cfg, _ := s.store.settings()
	v := Vendor{ID: "remote", Name: "测试", Type: "api", Kind: "custom", Protocol: "openai", Base: "https://catalog.example/v1", KeyMode: "env", KeyEnv: "CATALOG_TEST_KEY"}
	t.Setenv(v.KeyEnv, "old-secret")
	cfg.Vendors = append(cfg.Vendors, v)
	request(t, s, "PUT", "/api/settings", cfg, 200)
	cfg, _ = s.store.settings()
	version := cfg.Version
	original := http.DefaultTransport
	calls := 0
	fail := false
	http.DefaultTransport = catalogRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if fail {
			return catalogResponse(403, `{"error":"old-secret"}`), nil
		}
		return catalogResponse(200, `{"data":[{"id":"gpt-test"}]}`), nil
	})
	defer func() { http.DefaultTransport = original }()
	path := "/api/vendors/remote/models"
	request(t, s, "GET", path, nil, 200)
	request(t, s, "GET", path, nil, 200)
	if calls != 1 {
		t.Fatal("cache miss", calls)
	}
	fail = true
	var cat ModelCatalog
	_ = json.Unmarshal(request(t, s, "GET", path+"?refresh=1", nil, 200), &cat)
	if !cat.Stale || cat.Error == "" || len(cat.Models) != 1 {
		t.Fatal("failed refresh lost cached models", cat)
	}
	t.Setenv(v.KeyEnv, "new-secret")
	request(t, s, "GET", path, nil, 400)
	if calls != 3 {
		t.Fatal("credential change reused cache", calls)
	}
	after, _ := s.store.settings()
	if after.Version != version {
		t.Fatal("catalog changed quote version")
	}
	if catalogKey(v, "a") == catalogKey(v, "b") {
		t.Fatal("credential identity collision")
	}
	v.Base = "https://other.example/v1"
	if catalogKey(v, "a") == catalogKey(cfg.Vendors[len(cfg.Vendors)-1], "a") {
		t.Fatal("connection identity collision")
	}
}
func TestCurrentRegistryOnlyMergesGeneratedLegacyConnections(t *testing.T) {
	a := Vendor{ID: "legacy-text", Name: "文本服务（原配置）", Type: "cli", CLI: "codex"}
	b := a
	b.ID = "legacy-image"
	b.Name = "图片服务（原配置）"
	c := a
	c.ID = "user"
	c.Name = "我的独立连接"
	old := Settings{RegistryVersion: 1, Vendors: []Vendor{a, b, c}, Text: Endpoint{VendorID: a.ID, Model: "text"}, Image: Endpoint{VendorID: b.ID, Model: "image"}}
	migrated := upgradeCurrentRegistry(old)
	if len(migrated.Vendors) != 2 || migrated.Text.VendorID != migrated.Image.VendorID || migrated.Vendors[0].Name != "Codex CLI" {
		t.Fatal(migrated)
	}
	if old.Vendors[0].Kind != "" || old.Image.VendorID != b.ID {
		t.Fatal("historical config mutated")
	}
	different := b
	different.CredentialID = "different"
	if len(upgradeCurrentRegistry(Settings{Vendors: []Vendor{a, different}}).Vendors) != 2 {
		t.Fatal("different credentials merged")
	}
	renamed := b
	renamed.Name = "用户自定义"
	if len(upgradeCurrentRegistry(Settings{Vendors: []Vendor{a, renamed}}).Vendors) != 2 {
		t.Fatal("custom name lost")
	}
}
func TestSettingsSectionsCannotOverwriteEachOther(t *testing.T) {
	s := testStudio(t)
	cfg, _ := s.store.settings()
	payload := map[string]any{}
	_ = json.Unmarshal(jsonBytes(cfg), &payload)
	payload["section"] = "vendors"
	payload["mode"] = "remote"
	payload["text"] = Endpoint{VendorID: "missing", Model: "wrong"}
	request(t, s, "PUT", "/api/settings", payload, 200)
	saved, _ := s.store.settings()
	if saved.Mode != cfg.Mode || saved.Text.VendorID != cfg.Text.VendorID || saved.Text.Model != cfg.Text.Model {
		t.Fatal("vendor save overwrote services")
	}
	request(t, s, "PUT", "/api/settings", payload, 409) // stale version
	payload = map[string]any{}
	_ = json.Unmarshal(jsonBytes(saved), &payload)
	payload["section"] = "services"
	payload["vendors"] = []Vendor{}
	payload["text"] = Endpoint{VendorID: saved.Text.VendorID, Model: "test-model"}
	request(t, s, "PUT", "/api/settings", payload, 200)
	saved, _ = s.store.settings()
	if len(saved.Vendors) != len(cfg.Vendors) || saved.Text.Model != "test-model" {
		t.Fatal("service save overwrote connections")
	}
}
func TestModelBindingValidationUsesAdapterAndCatalog(t *testing.T) {
	v := Vendor{ID: "ark", Name: "豆包", Type: "api", Kind: "ark", Protocol: "ark", Base: "https://ark.example/v3", KeyMode: "env", KeyEnv: "CATALOG_KEY"}
	for _, tc := range []struct {
		id, cap string
		allowed bool
	}{{"doubao-seed-1", "text", true}, {"doubao-seedance-1", "text", false}, {"doubao-seedream-4", "image", false}, {"qwen-omni", "text", false}, {"endpoint-custom", "video", true}} {
		if modelAllows(v, tc.id, tc.cap) != tc.allowed {
			t.Fatal(tc)
		}
	}
	s := testStudio(t)
	t.Setenv("CATALOG_KEY", "secret")
	cfg := Settings{Vendors: []Vendor{v}, Text: Endpoint{VendorID: v.ID, Model: "custom-image"}}
	s.catalog = map[string]ModelCatalog{catalogKey(v, "secret"): {Updated: time.Now(), Models: []CatalogModel{{ID: "custom-image", Verified: true, Capabilities: []string{"image"}, Services: []string{}}}}}
	if s.validateCatalogBindings(cfg) == nil {
		t.Fatal("catalog incompatibility ignored")
	}
	if s.validateCatalogBindings(cfg, "video") != nil {
		t.Fatal("unrelated service blocks quote")
	}
}

func TestHistoricalRegistrySnapshotRetainsOriginalCompatibility(t *testing.T) {
	old := Settings{RegistryVersion: 1, Vendors: []Vendor{{ID: "legacy", Name: "旧兼容连接", Type: "api", Protocol: "openai", Base: "https://example.com/v1"}}, Image: Endpoint{VendorID: "legacy", Model: "qwen-legacy-alias"}}
	if _, err := resolveEndpoint(old, "image"); err != nil {
		t.Fatal("historical task binding reinterpreted", err)
	}
	current := upgradeCurrentRegistry(old)
	if _, err := resolveEndpoint(current, "image"); err == nil {
		t.Fatal("current settings did not validate model capability")
	}
}

func TestDisableVoiceWithoutConfiguringUnusedVendorCredentials(t *testing.T) {
	s := testStudio(t)
	cfg, _ := s.store.settings()
	cfg.Vendors = append(cfg.Vendors, Vendor{ID: "optional-voice", Name: "未配置旁白", Type: "api", Kind: "custom", Protocol: "openai", Base: "https://example.com/v1", KeyMode: "local"})
	cfg.Voice = Endpoint{VendorID: "optional-voice", Model: "tts-1"}
	if err := validateRegistry(&cfg); err != nil {
		t.Fatal(err)
	}
	// Represent an existing incomplete registry, without requiring a secret to create it.
	if _, err := s.store.db.Exec("UPDATE settings SET body=? WHERE id=1", jsonBytes(cfg)); err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{}
	_ = json.Unmarshal(jsonBytes(cfg), &payload)
	payload["section"] = "services"
	payload["voice"] = Endpoint{}
	request(t, s, "PUT", "/api/settings", payload, 200)
	saved, _ := s.store.settings()
	if saved.Voice.VendorID != "" || saved.Voice.Model != "" || string(jsonBytes(saved.Vendors)) != string(jsonBytes(cfg.Vendors)) || saved.Text != cfg.Text || saved.Image != cfg.Image || saved.Video != cfg.Video {
		t.Fatal("optional service edit mutated other configuration")
	}
	_ = json.Unmarshal(jsonBytes(saved), &payload)
	payload["section"] = "vendors"
	request(t, s, "PUT", "/api/settings", payload, 400) // Credential validation remains required for vendor edits.
}
