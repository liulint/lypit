package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func nativeImageFixture(t *testing.T, body string) (*Studio, Provider, string, string) {
	t.Helper()
	s := testStudio(t)
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	image := filepath.Join(t.TempDir(), "fixture.png")
	fixturePNG(t, image, 8, 8)
	bin := filepath.Join(t.TempDir(), "codex")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > args.txt\ncat > prompt.txt\n" + strings.ReplaceAll(body, "@IMAGE@", "'"+strings.ReplaceAll(image, "'", "'\\''")+"'")
	if e := os.WriteFile(bin, []byte(script), 0700); e != nil {
		t.Fatal(e)
	}
	p := cliTestProvider("codex", bin)
	p.CLI.ImageMode = "codex"
	p.CLI.ImageInstructions = ""
	p.Models[0].OutputFormats = []string{"png"}
	return s, p, home, image
}

const nativeSuccess = `mkdir -p "$CODEX_HOME/generated_images/thread-test"
cp @IMAGE@ "$CODEX_HOME/generated_images/thread-test/ig_one.png"
printf '%s\n' '{"type":"thread.started","thread_id":"thread-test"}' '{"type":"turn.completed"}'
`

func TestNativeCodexImageReferencesAndRecovery(t *testing.T) {
	s, p, home, _ := nativeImageFixture(t, nativeSuccess)
	dir := filepath.Join(t.TempDir(), "task with space")
	ref := "uploaded.png"
	if e := os.MkdirAll(filepath.Dir(s.abs(ref)), 0700); e != nil {
		t.Fatal(e)
	}
	fixturePNG(t, s.abs(ref), 8, 8)
	raw, e := s.executeCLI(context.Background(), p, p.Models[0], "image", "a cat", []string{ref}, dir, false)
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = cliImageOutput(dir, raw, p.Models[0]); e != nil {
		t.Fatal(e)
	}
	args, _ := os.ReadFile(filepath.Join(dir, "args.txt"))
	prompt, _ := os.ReadFile(filepath.Join(dir, "prompt.txt"))
	root, _ := filepath.EvalSymlinks(filepath.Join(home, "generated_images"))
	if !strings.Contains(string(args), "--add-dir\n"+root) || !strings.Contains(string(args), "-i\n"+filepath.Join(dir, "inputs", "ref-0.png")) || !strings.HasPrefix(string(prompt), "$imagegen\n") {
		t.Fatal(string(args), string(prompt))
	}
	if _, e = os.Stat(filepath.Join(home, "generated_images", "thread-test", "ig_one.png")); e != nil {
		t.Fatal("source image removed")
	}
	if _, e = s.executeCLI(context.Background(), p, p.Models[0], "image", "a cat", nil, dir, false); e == nil {
		t.Fatal("second invocation allowed")
	}
	// Recovery uses the saved task image, even after the external output vanishes.
	if e = os.Rename(filepath.Join(home, "generated_images", "thread-test"), filepath.Join(home, "generated_images", "kept-history")); e != nil {
		t.Fatal(e)
	}
	recovered, e := s.executeCLI(context.Background(), p, p.Models[0], "image", "", nil, dir, true)
	if e != nil || string(recovered) != string(raw) {
		t.Fatal(string(recovered), e)
	}
}

func TestNativeCodexImageThreadValidation(t *testing.T) {
	for _, m := range []Model{{Sizes: []string{"auto"}}, {Sizes: []string{"99999x99999"}}, {Sizes: []string{"8x8"}, ReferenceImages: true, MaxReferences: 10}} {
		if validateNativeImageModel(m) == nil {
			t.Fatal("invalid native image specification accepted")
		}
	}
	for _, raw := range []string{`{"type":"thread.started","thread_id":"../escape"}`, `{"type":"thread.started","thread_id":"a"}` + "\n" + `{"type":"thread.started","thread_id":"b"}`, `{"file":"old.png"}`, `preview only`} {
		if _, e := codexImageThread([]byte(raw)); e == nil {
			t.Fatal("bad thread accepted", raw)
		}
	}
}

func TestNativeCodexImageRejectsUnrelatedOrUnsafeFiles(t *testing.T) {
	for _, kind := range []string{"preview", "other-thread", "symlink-thread", "symlink-file", "old-image", "multiple", "corrupt"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			dir := t.TempDir()
			intent := codexImageIntent{Root: root, Started: time.Now().UTC()}
			thread := filepath.Join(root, "thread-test")
			if e := os.Mkdir(thread, 0700); e != nil {
				t.Fatal(e)
			}
			source := filepath.Join(t.TempDir(), "source.png")
			fixturePNG(t, source, 8, 8)
			image := filepath.Join(thread, "ig_one.png")
			switch kind {
			case "preview":
				os.WriteFile(filepath.Join(thread, "preview.txt"), []byte("image preview only"), 0600)
			case "other-thread":
				other := filepath.Join(root, "other")
				os.Mkdir(other, 0700)
				fixturePNG(t, filepath.Join(other, "ig_one.png"), 8, 8)
			case "symlink-thread":
				os.Rename(thread, thread+"-real")
				os.Symlink(thread+"-real", thread)
			case "symlink-file":
				os.Symlink(source, image)
			case "old-image":
				fixturePNG(t, image, 8, 8)
				old := time.Now().Add(-time.Hour)
				os.Chtimes(image, old, old)
			case "multiple":
				fixturePNG(t, image, 8, 8)
				fixturePNG(t, filepath.Join(thread, "ig_two.png"), 8, 8)
			case "corrupt":
				os.WriteFile(image, []byte("not an image"), 0600)
			}
			raw, e := collectCodexImage(dir, intent, "thread-test")
			if e == nil {
				_, _, e = cliImageOutput(dir, raw, Model{Sizes: []string{"8x8"}})
			}
			if e == nil {
				t.Fatal("unsafe output accepted")
			}
		})
	}
}

func TestNativeCodexImageCancellationSavesResponseWithoutResubmission(t *testing.T) {
	s, p, home, image := nativeImageFixture(t, `printf x >> invoked.txt
printf '%s\n' '{"type":"thread.started","thread_id":"thread-test"}'
sleep 30 &
wait
`)
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	completed := make(chan error, 1)
	go func() { _, e := s.executeCLI(ctx, p, p.Models[0], "image", "cat", nil, dir, false); completed <- e }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if marker, _ := os.ReadFile(filepath.Join(dir, "invoked.txt")); string(marker) == "x" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fake CLI did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Give the fake CLI's immediate thread event time to reach its stdout pipe.
	time.Sleep(30 * time.Millisecond)
	start := time.Now()
	cancel()
	if e := <-completed; e == nil || time.Since(start) > 3*time.Second {
		t.Fatal("cancel failed", e)
	}
	if _, e := os.Stat(filepath.Join(dir, "response.bin")); e != nil {
		t.Fatal(e)
	}
	if _, e := s.executeCLI(context.Background(), p, p.Models[0], "image", "cat", nil, dir, true); e == nil {
		t.Fatal("missing output recovered")
	}
	thread := filepath.Join(home, "generated_images", "thread-test")
	os.MkdirAll(thread, 0700)
	data, _ := os.ReadFile(image)
	os.WriteFile(filepath.Join(thread, "call_late.png"), data, 0600)
	if _, e := s.executeCLI(context.Background(), p, p.Models[0], "image", "cat", nil, dir, true); e != nil {
		t.Fatal(e)
	}
	count, _ := os.ReadFile(filepath.Join(dir, "invoked.txt"))
	if string(count) != "x" {
		t.Fatal("recovery resubmitted")
	}
}

func TestNativeCodexImageConcurrentIntent(t *testing.T) {
	s, p, _, _ := nativeImageFixture(t, "printf x >> invoked.txt\n"+nativeSuccess)
	dir := t.TempDir()
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := s.executeCLI(context.Background(), p, p.Models[0], "image", "cat", nil, dir, false)
			results <- e
		}()
	}
	wg.Wait()
	close(results)
	success := 0
	for e := range results {
		if e == nil {
			success++
		}
	}
	count, _ := os.ReadFile(filepath.Join(dir, "invoked.txt"))
	if success != 1 || string(count) != "x" {
		t.Fatal(success, string(count))
	}
}

func TestNativeCodexImageVerificationRequiresQuoteAndModeFingerprint(t *testing.T) {
	s, p, _, _ := nativeImageFixture(t, nativeSuccess)
	settings := s.store.settings()
	settings.Providers = append(settings.Providers, p)
	if w := modelAPI(t, s, "PUT", "/api/settings", settings); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	probe, e := s.quoteImageProbe(p.ID, "test")
	if e != nil {
		t.Fatal(e)
	}
	if probe.Quote.Estimate != nil || probe.State != "pending" {
		t.Fatal("unconfirmed quote executed or cost invented")
	}
	if _, e = os.Stat(s.probeDir(probe.ID)); !os.IsNotExist(e) {
		t.Fatal("quote launched generation")
	}
	changed := clone(settings)
	changed.Providers[len(changed.Providers)-1].CLI.ImageMode = "tool"
	changed.Providers[len(changed.Providers)-1].CLI.ImageInstructions = "other tool"
	if w := modelAPI(t, s, "PUT", "/api/settings", changed); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if _, e = s.startImageProbe(probe.ID); e == nil {
		t.Fatal("stale mode quote accepted")
	}
	if w := modelAPI(t, s, "PUT", "/api/settings", settings); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	probe, e = s.quoteImageProbe(p.ID, "test")
	if e != nil {
		t.Fatal(e)
	}
	active, e := s.startImageProbe(probe.ID)
	if e != nil {
		t.Fatal(e)
	}
	s.launchImageProbe(active, false)
	s.wg.Wait()
	var saved ImageProbe
	s.store.mu.Lock()
	saved = clone(s.store.state.Probes[probe.ID])
	s.store.mu.Unlock()
	if saved.State != "succeeded" {
		b, _ := json.Marshal(saved)
		t.Fatal(fmt.Sprintf("verification failed: %s", b))
	}
	settings = s.store.settings()
	actual, _ := providerByID(settings, p.ID)
	if !cliImageReady(actual, actual.Models[0]) {
		t.Fatal("successful verification not bound")
	}
}
