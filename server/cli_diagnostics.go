package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const cliLogLimit = 128 << 10

// Keep the tail so a long CLI run does not hide its final error. Raw output
// stays in memory; only sanitized diagnostics may be written to disk.
type cliLogTail struct {
	data  []byte
	total int
}

func (w *cliLogTail) Write(p []byte) (int, error) {
	n := len(p)
	w.total += n
	if n >= cliLogLimit {
		w.data = append(w.data[:0], p[n-cliLogLimit:]...)
	} else {
		if len(w.data)+n > cliLogLimit {
			w.data = w.data[len(w.data)+n-cliLogLimit:]
		}
		w.data = append(w.data, p...)
	}
	return n, nil
}

var cliSensitiveName = regexp.MustCompile(`(?i)(key|token|secret|password|authorization|cookie|credential)`)
var cliSensitiveField = regexp.MustCompile(`(?im)((?:[a-z0-9_-]*(?:api[_-]?key|token|secret|password|authorization|cookie|credential)[a-z0-9_-]*)["']?\s*[:=]\s*)("[^"\r\n]*"|'[^'\r\n]*'|[^\r\n]+)`)
var cliBearer = regexp.MustCompile(`(?i)\bBearer\s+[^\s"'<>]+`)
var cliTokenShape = regexp.MustCompile(`\b(?:sk-|sess-|rk-)[A-Za-z0-9_-]{12,}|\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`)
var cliURLUser = regexp.MustCompile(`(https?://)[^/\s@]+@`)
var cliURLQuery = regexp.MustCompile(`(https?://[^\s?"']+)\?[^\s"']+`)

func (s *Studio) cliLogSecrets(cfg Settings) []string {
	values := map[string]bool{}
	for _, entry := range os.Environ() {
		name, value, ok := strings.Cut(entry, "=")
		if ok && value != "" && cliSensitiveName.MatchString(name) {
			values[value] = true
		}
	}
	endpoints := []Endpoint{cfg.Text, cfg.Image, cfg.Video, cfg.Voice}
	for _, v := range cfg.Vendors {
		endpoints = append(endpoints, Endpoint{VendorID: v.ID, KeyMode: v.KeyMode, KeyEnv: v.KeyEnv, CredentialID: v.CredentialID})
	}
	for _, ep := range endpoints {
		if secret, err := s.credential(ep); err == nil && secret != "" {
			values[secret] = true
		}
	}
	secrets := make([]string, 0, len(values))
	for value := range values {
		secrets = append(secrets, value)
	}
	sort.Slice(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
	return secrets
}

func sanitizeCLIOutput(b []byte, secrets []string, clipped bool) string {
	// Discard partial edge lines when the tail starts mid-record. They may
	// contain an unlabelled credential whose field name was already clipped.
	if clipped {
		if i := bytes.IndexByte(b, '\n'); i >= 0 {
			b = b[i+1:]
		} else {
			return "[output truncated]"
		}
	}
	text := strings.ToValidUTF8(string(b), "�")
	for _, secret := range secrets {
		text = strings.ReplaceAll(text, secret, "[REDACTED]")
		// Interrupted processes may end halfway through a known secret.
		for n := len(secret) - 1; n > 0; n-- {
			if strings.HasSuffix(text, secret[:n]) {
				text = text[:len(text)-n] + "[REDACTED]"
				break
			}
		}
	}
	text = cliBearer.ReplaceAllString(text, "Bearer [REDACTED]")
	text = cliTokenShape.ReplaceAllString(text, "[REDACTED]")
	text = cliSensitiveField.ReplaceAllString(text, "${1}[REDACTED]")
	text = cliURLUser.ReplaceAllString(text, "${1}[REDACTED]@")
	return cliURLQuery.ReplaceAllString(text, "${1}?[REDACTED]")
}

// One immutable diagnostic per actual CLI launch. Recovery never invokes this
// function or overwrites the original execution log. No args, env or stdin are logged.
func (s *Studio) runCLIWithDiagnostics(cmd *exec.Cmd, t *Task, dir string, stdout io.Writer) error {
	secrets := s.cliLogSecrets(t.Config)
	out, stderr := &cliLogTail{}, &cliLogTail{}
	usage := &cliUsageWriter{}
	cmd.Stdout = io.MultiWriter(out, usage)
	if stdout != nil {
		cmd.Stdout = io.MultiWriter(stdout, out, usage)
	}
	cmd.Stderr = stderr
	started := time.Now()
	runErr := cmd.Run()
	usage.finish()
	if usage.counts != nil {
		if err := os.WriteFile(filepath.Join(dir, "usage.json"), jsonBytes(usage.counts), 0600); err != nil {
			runErr = errors.Join(runErr, errors.New("CLI用量保存失败；禁止重新执行"))
		}
	}
	exitCode := -1
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}
	executionError := ""
	if runErr != nil {
		executionError = sanitizeCLIOutput([]byte(runErr.Error()), secrets, false)
	}
	diagnostic := struct {
		Started         string `json:"started"`
		DurationMS      int64  `json:"duration_ms"`
		ExitCode        int    `json:"exit_code"`
		ExecutionFailed bool   `json:"execution_failed"`
		ExecutionError  string `json:"execution_error,omitempty"`
		Stdout          string `json:"stdout"`
		Stderr          string `json:"stderr"`
		StdoutTruncated bool   `json:"stdout_truncated"`
		StderrTruncated bool   `json:"stderr_truncated"`
	}{
		Started: started.UTC().Format(time.RFC3339Nano), DurationMS: time.Since(started).Milliseconds(), ExitCode: exitCode,
		ExecutionFailed: runErr != nil, ExecutionError: executionError,
		Stdout: sanitizeCLIOutput(out.data, secrets, out.total > len(out.data)), Stderr: sanitizeCLIOutput(stderr.data, secrets, stderr.total > len(stderr.data)),
		StdoutTruncated: out.total > len(out.data), StderrTruncated: stderr.total > len(stderr.data),
	}
	f, err := os.OpenFile(filepath.Join(dir, "cli-execution.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err == nil {
		_, err = f.Write(jsonBytes(diagnostic))
		err = errors.Join(err, f.Close())
	}
	if err != nil {
		return errors.Join(runErr, errors.New("CLI诊断日志保存失败；已保留提交记录，禁止重新执行"))
	}
	return runErr
}
