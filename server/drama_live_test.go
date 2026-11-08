package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Explicit, bounded acceptance only. Normal test runs never call a model.
// Persistent operation IDs and receipts make re-reading results safe; failures
// are not automatically replaced by a new invocation.
func TestDramaLiveTextAcceptance(t *testing.T) {
	if os.Getenv("LYPIT_LIVE_TEXT_EVAL") != "1" {
		t.Skip("explicit live acceptance only")
	}
	root, e := filepath.Abs("..")
	if e != nil {
		t.Fatal(e)
	}
	dir := os.Getenv("LYPIT_LIVE_EVAL_DIR")
	if dir == "" {
		t.Fatal("explicit isolated output directory required")
	}
	dir, e = filepath.Abs(dir)
	if e != nil {
		t.Fatal(e)
	}
	if dir == filepath.Join(root, "data") {
		t.Fatal("daily data is not an evaluation workspace")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, e := newStudio(ctx, root, dir)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { cancel(); s.wg.Wait(); s.store.close() }()
	source := url.URL{Scheme: "file", Path: filepath.ToSlash(filepath.Join(root, "data", "studio.db"))}
	source.RawQuery = "mode=ro"
	db, e := sql.Open("sqlite", source.String())
	if e != nil {
		t.Fatal(e)
	}
	var raw []byte
	e = db.QueryRow("SELECT body FROM settings WHERE id=1").Scan(&raw)
	db.Close()
	if e != nil {
		t.Fatal(e)
	}
	var cfg Settings
	if e = json.Unmarshal(raw, &cfg); e != nil {
		t.Fatal(e)
	}
	cfg = upgradeCurrentRegistry(withVendorRegistry(cfg))
	ep, e := resolveEndpoint(cfg, "text")
	if e != nil || ep.Transport != "cli" || cfg.Mode != "remote" {
		t.Fatal("live fixture is explicitly limited to the existing local CLI text binding")
	}
	cfg.PromptTemplates = nil
	if _, e = s.store.db.Exec("UPDATE settings SET body=? WHERE id=1", jsonBytes(cfg)); e != nil {
		t.Fatal(e)
	}
	var corpus struct {
		Cases []struct {
			ID, Title, Idea string
			Duration        int
		}
	}
	raw, e = os.ReadFile("testdata/drama-quality-cases.json")
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(raw, &corpus); e != nil || len(corpus.Cases) != 5 {
		t.Fatal("fixed five-case corpus required", e)
	}
	type record struct {
		Case     string `json:"case"`
		TaskID   string `json:"task_id"`
		Status   string `json:"status"`
		Message  string `json:"message"`
		Markdown string `json:"markdown"`
	}
	records := []record{}
	for _, c := range corpus.Cases {
		id := digest([]byte("drama-live-text-0.11.1-20261108:" + c.ID))[:24]
		task, err := s.store.task(id)
		if err != nil {
			w := dramaTestWork(t, s)
			w.Title = c.Title
			w.Idea = c.Idea
			w.Duration = c.Duration
			w.Form = "剧情短片，忠实遵循题材与输入，不强套冲突模板"
			if c.ID == "warm-cats" {
				w.Form = "温暖而克制的动物剧情短片，细腻表演，少对白，避免口号和煽情配乐替代剧情"
			}
			s.store.db.Exec("UPDATE drama_works SET body=? WHERE id=?", jsonBytes(w), w.ID)
			prompt, refs, err := s.dramaPrompt(w, "script", "仅写这一集剧本。遵循目标时长，不生成分镜或其他文档。", map[string]string{})
			if err != nil {
				t.Fatal(err)
			}
			j := DramaJob{ID: id, WorkID: w.ID, Kind: "script", SkillVersion: dramaVersion, Inputs: map[string]string{}, Prompt: prompt, References: refs}
			task, err = s.newDramaTask(w, cfg, j)
			if err != nil {
				t.Fatal(err)
			}
			s.launchDrama(id)
		} else if task.Status == "interrupted" || task.Status == "failed" || task.Status == "cancelled" {
			var n int
			s.store.db.QueryRow("SELECT count(*) FROM attempts WHERE task_id=?", id).Scan(&n)
			if n > 0 {
				s.launchDrama(id)
			}
		}
		deadline := time.Now().Add(12 * time.Minute)
		for time.Now().Before(deadline) {
			task, e = s.store.task(id)
			if e != nil {
				t.Fatal(e)
			}
			if task.Status == "completed" || task.Status == "failed" || task.Status == "cancelled" {
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
		job, e := s.dramaJob(id)
		if e != nil {
			t.Fatal(e)
		}
		records = append(records, record{Case: c.ID, TaskID: id, Status: task.Status, Message: task.Message, Markdown: job.Candidate})
		if e = os.WriteFile(filepath.Join(dir, "text-acceptance-results.json"), jsonBytes(records), 0600); e != nil {
			t.Fatal(e)
		}
		t.Logf("%s: %s", c.ID, task.Status)
	}
	var attempts int
	s.store.db.QueryRow("SELECT count(*) FROM attempts").Scan(&attempts)
	if attempts != 5 {
		t.Fatal("expected exactly five durable CLI submissions")
	}
	for _, r := range records {
		if r.Status != "completed" {
			t.Errorf("%s: %s", r.Case, r.Message)
		}
	}
}
