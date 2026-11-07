package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCLIDiagnosticsRetainsFailureAndNeverRerunsUnknownImage(t *testing.T) {
	for _, scenario := range []string{"fail", "empty-image"} {
		t.Run(scenario, func(t *testing.T) {
			s := testStudio(t)
			job := cliTask(t, s)
			t.Setenv("CODEX_HOME", t.TempDir())
			t.Setenv("LYPIT_TEST_SECRET_TOKEN", "private-token-not-for-logs")
			ep := Endpoint{CLI: "codex", Executable: fakeCLI(t, scenario)}
			if _, err := s.cliImage(context.Background(), &job, "shot-one", "private prompt", nil, false, ep); err == nil {
				t.Fatal("expected failed or empty receipt")
			}
			dir := filepath.Join(s.data, "cli", job.ID, "shot-one")
			file := filepath.Join(dir, "cli-execution.json")
			before, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			var log struct {
				ExitCode int    `json:"exit_code"`
				Stderr   string `json:"stderr"`
			}
			if err = json.Unmarshal(before, &log); err != nil {
				t.Fatal(err)
			}
			if scenario == "fail" && (log.ExitCode != 1 || !strings.Contains(log.Stderr, "inference failed")) {
				t.Fatal("lost CLI failure", log)
			}
			if scenario == "empty-image" && (log.ExitCode != 0 || !strings.Contains(log.Stderr, "image_gen unavailable")) {
				t.Fatal("lost empty-receipt diagnostic", log)
			}
			if strings.Contains(string(before), "private-token-not-for-logs") || strings.Contains(string(before), "private prompt") {
				t.Fatal("private input leaked")
			}
			if runtime.GOOS != "windows" {
				if info, _ := os.Stat(file); info.Mode().Perm() != 0600 {
					t.Fatal("diagnostic is not private")
				}
			}
			if _, err = s.cliImage(context.Background(), &job, "shot-one", "different prompt", nil, false, ep); err == nil {
				t.Fatal("unknown result accepted")
			}
			after, _ := os.ReadFile(file)
			runs, _ := os.ReadFile(filepath.Join(dir, "runs"))
			if string(runs) != "run" || string(before) != string(after) {
				t.Fatal("recovery relaunched CLI or overwrote execution log")
			}
		})
	}
}

func TestCLIDiagnosticRedactionAndBoundedTail(t *testing.T) {
	secret := "secret-value-with-split-writes"
	w := &cliLogTail{}
	w.Write([]byte(strings.Repeat("x", cliLogLimit+10) + "\n"))
	w.Write([]byte("token=" + secret[:8]))
	w.Write([]byte(secret[8:] + "\nhttps://name:password@example.test/task?api_key=unsafe&signature=hidden\nfinal error\n"))
	got := sanitizeCLIOutput(w.data, []string{secret}, w.total > len(w.data))
	for _, bad := range []string{secret, "password", "unsafe", "hidden"} {
		if strings.Contains(got, bad) {
			t.Fatal("credential leaked", bad)
		}
	}
	if !strings.Contains(got, "final error") || len(w.data) > cliLogLimit {
		t.Fatal("tail lost final error or exceeded limit")
	}
	if got = sanitizeCLIOutput([]byte("failure "+secret[:10]), []string{secret}, false); strings.Contains(got, secret[:10]) {
		t.Fatal("partial secret leaked")
	}
}

func TestTextCLIDiagnosticDoesNotBreakStructuredResponse(t *testing.T) {
	s := testStudio(t)
	job := cliTask(t, s)
	var result struct {
		OK bool `json:"ok"`
	}
	ep := Endpoint{CLI: "claude", Executable: fakeCLI(t, "claude")}
	if err := s.cliTextJSON(context.Background(), &job, "text-one", "prompt", ep, &result); err != nil || !result.OK {
		t.Fatal("structured stdout lost", err)
	}
	if _, err := os.Stat(filepath.Join(s.data, "cli", job.ID, "text-one", "cli-execution.json")); err != nil {
		t.Fatal(err)
	}
}
