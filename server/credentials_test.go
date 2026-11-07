package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func saveLocalKey(t *testing.T, s *Studio, cfg Settings, key string, status int) Settings {
	t.Helper()
	payload := map[string]any{}
	if err := json.Unmarshal(jsonBytes(cfg), &payload); err != nil {
		t.Fatal(err)
	}
	payload["api_keys"] = map[string]string{"local-api": key}
	b := request(t, s, "PUT", "/api/settings", payload, status)
	if key != "" && bytes.Contains(b, []byte(key)) {
		t.Fatal("key echoed in save response")
	}
	var out Settings
	if status == 200 {
		if err := json.Unmarshal(b, &out); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func localKeySettings() Settings {
	return Settings{RegistryVersion: 1, Version: 1, Mode: "remote", Vendors: []Vendor{{ID: "local-api", Name: "本机密钥", Type: "api", Protocol: "openai", Base: "https://example.com/v1", KeyMode: "local"}}, Text: Endpoint{VendorID: "local-api", Model: "test"}}
}

func TestEncryptedCredentialsRotateKeepSnapshotsAndNeverLeak(t *testing.T) {
	s := testStudio(t)
	firstKey, secondKey := "test-only-secret-111-abcdef", "test-only-secret-222-ghijkl"
	cfg := saveLocalKey(t, s, localKeySettings(), firstKey, 200)
	ep, err := resolveEndpoint(cfg, "text")
	if err != nil {
		t.Fatal(err)
	}
	if key, err := s.credential(ep); err != nil || key != firstKey {
		t.Fatal("credential not resolved", err)
	}
	job := cliTask(t, s)
	job.Config = cfg
	if _, err = s.store.db.Exec("UPDATE tasks SET config=? WHERE id=?", jsonBytes(cfg), job.ID); err != nil {
		t.Fatal(err)
	}
	// Blank edits retain the original immutable version, ignoring forged client references.
	cfg.Vendors[0].CredentialID = "ffffffffffffffffffffffff"
	cfg = saveLocalKey(t, s, cfg, "", 200)
	if cfg.Vendors[0].CredentialID != ep.CredentialID {
		t.Fatal("blank edit replaced credential")
	}
	cfg = saveLocalKey(t, s, cfg, secondKey, 200)
	newEP, _ := resolveEndpoint(cfg, "text")
	if newEP.CredentialID == ep.CredentialID {
		t.Fatal("rotation overwrote immutable version")
	}
	if key, err := s.credential(newEP); err != nil || key != secondKey {
		t.Fatal("rotated key unavailable", err)
	}
	if key, err := s.credential(ep); err != nil || key != firstKey {
		t.Fatal("old task key lost", err)
	}
	if _, err := s.credential(Endpoint{KeyMode: "local", CredentialID: ep.CredentialID, VendorID: "other"}); err == nil {
		t.Fatal("ciphertext accepted for another vendor")
	}
	// An API that echoes its Authorization header cannot put the key into attempt records.
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+firstKey {
			t.Error("snapshot used the wrong key")
		}
		_, _ = w.Write([]byte(`{"echo":"` + firstKey + `"}`))
	}))
	defer provider.Close()
	ep.Base = provider.URL
	b, err := s.paid(context.Background(), &job, "request", ep, "/jobs", "application/json", []byte(`{}`))
	if err != nil || bytes.Contains(b, []byte(firstKey)) {
		t.Fatal("provider echo leaked", err)
	}
	for _, path := range []string{"/api/settings", "/api/tasks", "/api/projects/" + job.ProjectID + "/export"} {
		response := request(t, s, "GET", path, nil, 200)
		if strings.HasSuffix(path, "/export") {
			archive, err := zip.NewReader(bytes.NewReader(response), int64(len(response)))
			if err != nil {
				t.Fatal(err)
			}
			for _, file := range archive.File {
				f, err := file.Open()
				if err != nil {
					t.Fatal(err)
				}
				content, _ := io.ReadAll(f)
				f.Close()
				response = append(response, content...)
			}
		}
		for _, secret := range []string{firstKey, secondKey} {
			if bytes.Contains(response, []byte(secret)) {
				t.Fatal("API leaked key", path)
			}
		}
	}
	for _, query := range []string{"SELECT body FROM settings", "SELECT config FROM tasks", "SELECT response FROM attempts"} {
		rows, err := s.store.db.Query(query)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var content []byte
			if err = rows.Scan(&content); err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(content, []byte(firstKey)) || bytes.Contains(content, []byte(secondKey)) {
				t.Fatal("plaintext stored in SQLite")
			}
		}
		rows.Close()
	}
	entries, err := os.ReadDir(filepath.Join(s.data, "credentials"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		path := filepath.Join(s.data, "credentials", entry.Name())
		b, err := privateCredentialFile(path, 16<<10)
		if err != nil || bytes.Contains(b, []byte(firstKey)) || bytes.Contains(b, []byte(secondKey)) {
			t.Fatal("unsafe credential file", entry.Name(), err)
		}
	}
	if err = s.store.close(); err != nil {
		t.Fatal(err)
	}
	s.store, err = openStore(s.data)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := s.store.task(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	oldEP, _ := resolveEndpoint(stored.Config, "text")
	if key, err := s.credential(oldEP); err != nil || key != firstKey {
		t.Fatal("restart lost snapshot credential", err)
	}
}

func TestCredentialSaveFailuresPreserveOldConfiguration(t *testing.T) {
	s := testStudio(t)
	cfg := saveLocalKey(t, s, localKeySettings(), "test-only-original-secret", 200)
	stale := cfg
	stale.Version--
	saveLocalKey(t, s, stale, "test-only-stale-secret", 409)
	saveLocalKey(t, s, cfg, "bad\nkey", 400)
	// Ciphertext may already have been published when SQLite fails; retain the old active reference.
	if _, err := s.store.db.Exec("CREATE TRIGGER reject_settings BEFORE UPDATE ON settings BEGIN SELECT RAISE(ABORT, 'test write failure'); END"); err != nil {
		t.Fatal(err)
	}
	saveLocalKey(t, s, cfg, "test-only-database-failure", 400)
	if _, err := s.store.db.Exec("DROP TRIGGER reject_settings"); err != nil {
		t.Fatal(err)
	}
	ep, _ := resolveEndpoint(cfg, "text")
	if key, err := s.credential(ep); err != nil || key != "test-only-original-secret" {
		t.Fatal("failed write invalidated old credential", err)
	}
	master := filepath.Join(s.data, "credentials", "master.key")
	if err := os.Remove(master); err != nil {
		t.Fatal(err)
	}
	saveLocalKey(t, s, cfg, "test-only-new-secret", 400)
	stored, err := s.store.settings()
	if err != nil || stored.Version != cfg.Version || stored.Vendors[0].CredentialID != cfg.Vendors[0].CredentialID {
		t.Fatal("failed save changed settings", err)
	}
	if _, err := os.Stat(master); !os.IsNotExist(err) {
		t.Fatal("lost master key silently replaced")
	}
}

func TestCredentialCipherTamperingAndEnvironmentCompatibility(t *testing.T) {
	s := testStudio(t)
	t.Setenv("TEST_EXISTING_API_KEY", "test-only-env-secret")
	if key, err := s.credential(Endpoint{KeyEnv: "TEST_EXISTING_API_KEY"}); err != nil || key != "test-only-env-secret" {
		t.Fatal("legacy env key broken", err)
	}
	cfg := saveLocalKey(t, s, localKeySettings(), "test-only-local-secret", 200)
	ep, _ := resolveEndpoint(cfg, "text")
	path := filepath.Join(s.data, "credentials", ep.CredentialID+".enc")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	b[len(b)-1] ^= 1
	if err = os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.credential(ep); err == nil {
		t.Fatal("tampered ciphertext accepted")
	}
	if _, err = s.credential(Endpoint{KeyMode: "local", CredentialID: "../../master.key"}); err == nil {
		t.Fatal("unsafe reference accepted")
	}
}
