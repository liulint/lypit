package main

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
)

func TestToolInstallerCoalescesRequestsAndRechecksBothTools(t *testing.T) {
	s := testStudio(t)
	var ready atomic.Bool
	var runs atomic.Int32
	release := make(chan struct{})
	lookup := func(name string) (string, error) {
		if name == toolInstallPlan(runtime.GOOS).Manager || ready.Load() {
			return "/fake/" + name, nil
		}
		return "", errors.New("missing")
	}
	install := func(ctx context.Context, path string) error {
		runs.Add(1)
		if path != "/fake/"+toolInstallPlan(runtime.GOOS).Manager {
			t.Error("unexpected installer", path)
		}
		select {
		case <-release:
			ready.Store(true)
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	state, err := s.startToolInstall(lookup, install)
	if err != nil || state.Status != "running" {
		t.Fatal("installer did not start", state, err)
	}
	var clients sync.WaitGroup
	for i := 0; i < 8; i++ {
		clients.Add(1)
		go func() {
			defer clients.Done()
			state, err := s.startToolInstall(lookup, install)
			if err != nil || state.Status != "running" {
				t.Error("duplicate request changed installation", state, err)
			}
		}()
	}
	clients.Wait()
	close(release)
	s.wg.Wait()
	s.toolMu.Lock()
	state = s.toolInstall
	s.toolMu.Unlock()
	if state.Status != "succeeded" || runs.Load() != 1 {
		t.Fatal("installation not coalesced", state, runs.Load())
	}
	state, err = s.startToolInstall(lookup, install)
	if err != nil || state.Status != "succeeded" || runs.Load() != 1 {
		t.Fatal("ready tools reinstalled", err)
	}
}

func TestToolInstallerHandlesMissingManagerFailureAndCancellation(t *testing.T) {
	for _, name := range []string{"no-manager", "failed", "incomplete", "cancelled"} {
		t.Run(name, func(t *testing.T) {
			s := testStudio(t)
			ctx, cancel := context.WithCancel(s.ctx)
			defer cancel()
			s.ctx = ctx
			lookup := func(tool string) (string, error) {
				if tool == toolInstallPlan(runtime.GOOS).Manager && name != "no-manager" {
					return "/fake/" + toolInstallPlan(runtime.GOOS).Manager, nil
				}
				return "", errors.New("missing")
			}
			var runs atomic.Int32
			install := func(ctx context.Context, _ string) error {
				runs.Add(1)
				if name == "cancelled" {
					<-ctx.Done()
					return ctx.Err()
				}
				if name == "incomplete" {
					return nil
				}
				return errors.New("network or permissions")
			}
			_, err := s.startToolInstall(lookup, install)
			if name == "no-manager" {
				if err == nil || runs.Load() != 0 {
					t.Fatal("missing manager executed", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			cancel()
			s.wg.Wait()
			s.toolMu.Lock()
			state := s.toolInstall
			s.toolMu.Unlock()
			if state.Status != "failed" || runs.Load() != 1 {
				t.Fatal("failure not reported", state, runs.Load())
			}
		})
	}
}

func TestToolInstallerSkipsInstallWhenBothToolsPresent(t *testing.T) {
	s := testStudio(t)
	lookup := func(name string) (string, error) {
		if name == toolInstallPlan(runtime.GOOS).Manager {
			t.Fatal("unneeded package manager lookup")
		}
		return "/fake/" + name, nil
	}
	state, err := s.startToolInstall(lookup, func(context.Context, string) error { t.Fatal("installed ready tools"); return nil })
	if err != nil || state.Status != "succeeded" {
		t.Fatal("ready tools not detected", state, err)
	}
}
