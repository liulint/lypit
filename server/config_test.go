package main

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadStudioConfig(t *testing.T) {
	for _, tc := range []struct {
		name, content, want string
	}{
		{"custom port", "server:\n  host: 127.0.0.1\n  port: 18080\n", "127.0.0.1:18080"},
		{"IPv6", "server:\n  host: '::1'\n  port: 8081\n", "[::1]:8081"},
		{"missing server", "{}", ""},
		{"missing host", "server:\n  port: 8080", ""},
		{"missing port", "server:\n  host: localhost", ""},
		{"zero port", "server:\n  host: localhost\n  port: 0", ""},
		{"large port", "server:\n  host: localhost\n  port: 65536", ""},
		{"invalid type", "server:\n  host: localhost\n  port: hello", ""},
		{"unknown field", "server:\n  host: localhost\n  port: 8080\n  prot: 8081", ""},
		{"invalid yaml", "server: [", ""},
		{"frontend zero port", "server:\n  host: localhost\n  port: 8080\nfrontend:\n  port: 0", ""},
		{"frontend large port", "server:\n  host: localhost\n  port: 8080\nfrontend:\n  port: 65536", ""},
		{"frontend invalid type", "server:\n  host: localhost\n  port: 8080\nfrontend:\n  port: hello", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tc.content), 0600); err != nil {
				t.Fatal(err)
			}
			config, err := loadStudioConfig(path)
			got := config.listenAddress()
			if tc.want == "" {
				if err == nil {
					t.Fatal("expected invalid configuration to fail")
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
			if config.Frontend.Port != 5173 {
				t.Fatalf("legacy frontend default = %d", config.Frontend.Port)
			}
		})
	}
	if _, err := loadStudioConfig(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatal("expected missing file to fail")
	}
	if _, err := loadStudioConfig("../config.yaml"); err != nil {
		t.Fatalf("repository configuration failed: %v", err)
	}
}

func TestConfiguredFrontendPort(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("server:\n  host: 127.0.0.1\n  port: 3010\nfrontend:\n  port: 3011\n"), 0600); err != nil {
		t.Fatal(err)
	}
	config, err := loadStudioConfig(path)
	if err != nil || config.Frontend.Port != 3011 {
		t.Fatalf("custom frontend configuration: %+v, %v", config, err)
	}
	s := testStudio(t)
	s.frontendPort = config.Frontend.Port
	router := s.router()
	for _, tc := range []struct {
		origin string
		status int
	}{
		{"http://127.0.0.1:3011", 404},
		{"http://localhost:3011", 404},
		{"http://127.0.0.1:5173", 403},
		{"http://evil.example:3011", 403},
		{"http://example.com", 404},
	} {
		req := httptest.NewRequest("POST", "/api/nonexistent", nil)
		req.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != tc.status {
			t.Errorf("origin %q: got %d, want %d", tc.origin, w.Code, tc.status)
		}
	}
}
