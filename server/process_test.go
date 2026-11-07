package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestCancellationStopsCLIProcessTree(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	path := fakeCLI(t, "tree")
	cmd, err := newCLICommand(ctx, path, "codex")
	if err != nil {
		t.Fatal(err)
	}
	cmd.Dir = t.TempDir()
	configureProcess(cmd)
	cmd.WaitDelay = time.Second
	done := make(chan error, 1)
	go func() { done <- cmd.Run() }()
	pid := 0
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(filepath.Join(cmd.Dir, "child.pid")); err == nil {
			pid, _ = strconv.Atoi(string(b))
			if pid > 0 {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if pid == 0 {
		cancel()
		<-done
		t.Fatal("CLI child never started")
	}
	t.Cleanup(func() {
		if testProcessRunning(pid) {
			if p, err := os.FindProcess(pid); err == nil {
				_ = p.Kill()
			}
		}
	})
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled CLI reported success")
		}
	case <-time.After(15 * time.Second):
		t.Fatal("CLI cancellation timed out")
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !testProcessRunning(pid) {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatal("CLI child survived cancellation")
}
