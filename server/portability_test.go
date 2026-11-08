package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPackagedFrontendServesFromPortableRoot(t *testing.T) {
	s := testStudio(t)
	s.root = filepath.Join(t.TempDir(), "发行包 & 空格")
	dir := filepath.Join(s.root, "web", "dist", "assets")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(dir), "index.html"), []byte("<html>studio</html>"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.js"), []byte("console.log('studio')"), 0600); err != nil {
		t.Fatal(err)
	}
	for route, want := range map[string]string{"/": "<html>studio</html>", "/vendors": "<html>studio</html>", "/assets/app.js": "console.log('studio')"} {
		w := httptest.NewRecorder()
		s.router().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080"+route, nil))
		if w.Code != 200 || w.Body.String() != want {
			t.Fatal("portable frontend failed", route, w.Code, w.Body.String())
		}
	}
	for _, route := range []string{"/assets/ServiceSettings-old.js", "/assets/old.css"} {
		w := httptest.NewRecorder()
		s.router().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080"+route, nil))
		if w.Code != http.StatusNotFound || strings.Contains(w.Body.String(), "<html>") {
			t.Fatal("missing asset received SPA document", route, w.Code, w.Body.String())
		}
	}
	for _, path := range []string{"C:/private.png", "file.png:stream.jpg", "dir\\private.png"} {
		if safeMedia(path) {
			t.Fatal("Windows path escape accepted", path)
		}
	}
}

func TestSQLitePortablePathsPragmasAndExclusiveLock(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "项目数据 # & 空格")
	st, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if another, err := openStore(dir); err == nil {
		another.close()
		t.Fatal("second instance acquired data lock")
	}
	for pragma, want := range map[string]string{"journal_mode": "wal", "foreign_keys": "1", "busy_timeout": "5000", "synchronous": "2"} {
		var got string
		if err := st.db.QueryRow("PRAGMA " + pragma).Scan(&got); err != nil || got != want {
			t.Fatal("SQLite pragma not applied", pragma, got, err)
		}
	}
	p, err := st.create("旧数据兼容", "测试故事", "电影感", "9:16", 60)
	if err != nil {
		t.Fatal(err)
	}
	if err = st.close(); err != nil {
		t.Fatal(err)
	}
	st, err = openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.close()
	got, err := st.project(p.ID)
	if err != nil || got.Title != p.Title {
		t.Fatal("SQLite reopen lost data", err)
	}
}

func TestPureGoSQLiteReadsExistingStandardDatabase(t *testing.T) {
	// Fixture created by standard SQLite, with the schema used by the former CGO driver.
	dir := t.TempDir()
	if err := copyTestFile(filepath.Join("testdata", "legacy.sqlite"), filepath.Join(dir, "studio.db"), 0600); err != nil {
		t.Fatal(err)
	}
	st, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.close()
	p, err := st.project("legacy-fixture")
	if err != nil || p.Title != "旧 SQLite 文件" {
		t.Fatal("legacy database changed", p, err)
	}
	cfg, err := st.settings()
	if err != nil || cfg.Version != 1 || len(cfg.Vendors) != 4 {
		t.Fatal("legacy settings not preserved", cfg, err)
	}
}

func TestWindowsNpmCLIUsesNodeWithoutShell(t *testing.T) {
	for _, adapter := range []string{"codex", "claude"} {
		t.Run(adapter, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "npm 空格 & 数据")
			var entry string
			if adapter == "codex" {
				entry = filepath.Join(dir, "node_modules", "@openai", "codex", "bin", "codex.js")
			} else {
				entry = filepath.Join(dir, "node_modules", "@anthropic-ai", "claude-code", "cli.js")
			}
			if err := os.MkdirAll(filepath.Dir(entry), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(entry, []byte("// official npm entry fixture"), 0600); err != nil {
				t.Fatal(err)
			}
			node := filepath.Join(dir, "external-node.exe")
			program, args, err := cliProgram(filepath.Join(dir, adapter+".cmd"), adapter, "windows", func(name string) (string, error) {
				if name != "node" {
					t.Fatal(name)
				}
				return node, nil
			})
			if err != nil || program != node || !reflect.DeepEqual(args, []string{entry}) {
				t.Fatal("npm shim used shell or lost path", program, args, err)
			}
			if _, _, err = cliProgram(filepath.Join(dir, "other.cmd"), adapter, "windows", findLocalExecutable); err == nil {
				t.Fatal("arbitrary batch accepted")
			}
			if _, _, err = cliProgram(filepath.Join(dir, adapter+".cmd"), adapter, "windows", func(string) (string, error) { return "", errors.New("missing node") }); err == nil {
				t.Fatal("missing node ignored")
			}
		})
	}
}

func TestWinGetMediaDiscoveryAndInstallerArguments(t *testing.T) {
	root := filepath.Join(t.TempDir(), "WinGet 包")
	for _, name := range []string{"ffmpeg", "ffprobe"} {
		dest := filepath.Join(root, "Gyan.FFmpeg_test", "ffmpeg-8.0-full_build", "bin", name+".exe")
		if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dest, []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
		if found, err := findWinGetMedia(name, root); err != nil || found != dest {
			t.Fatal("installed media not found", found, err)
		}
	}
	plan := toolInstallPlan("windows")
	args := strings.Join(plan.Args, " ")
	if plan.Manager != "winget" || !strings.Contains(args, "--id Gyan.FFmpeg --exact --source winget --scope user") || strings.Contains(args, "ignore-security") {
		t.Fatal("unsafe Windows installer", plan)
	}
	mac := toolInstallPlan("darwin")
	if mac.Manager != "brew" || !reflect.DeepEqual(mac.Args, []string{"install", "ffmpeg"}) {
		t.Fatal("macOS installer regressed")
	}
}
