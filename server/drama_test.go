package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func dramaTestWork(t *testing.T, s *Studio) DramaWork {
	t.Helper()
	var w DramaWork
	if e := json.Unmarshal(request(t, s, "POST", "/api/drama/works", map[string]any{"title": "第一堂课", "idea": "两只猫互相让出暖处", "genre": "温情", "form": "动物剧情", "duration": 5}, 200), &w); e != nil {
		t.Fatal(e)
	}
	return w
}
func dramaTestSave(t *testing.T, s *Studio, w DramaWork, kind, text string) DramaDoc {
	t.Helper()
	d, e := s.dramaDoc(w.ID, kind)
	if e != nil {
		t.Fatal(e)
	}
	var out DramaDoc
	b := request(t, s, "PUT", "/api/drama/works/"+w.ID+"/documents/"+kind, map[string]any{"base": d.Hash, "text": text, "accept": true}, 200)
	if e = json.Unmarshal(b, &out); e != nil {
		t.Fatal(e)
	}
	return out
}
func TestDramaDocumentAdoptionAndExternalChange(t *testing.T) {
	s := testStudio(t)
	w := dramaTestWork(t, s)
	script := dramaTestSave(t, s, w, "script", dramaMock("script", 5))
	assets := dramaTestSave(t, s, w, "assets", dramaMock("assets", 5))
	if assets.State != "accepted" {
		t.Fatal(assets)
	}
	request(t, s, "PUT", "/api/drama/works/"+w.ID+"/documents/script", map[string]any{"base": "obsolete", "text": "oops"}, 409)
	dramaTestSave(t, s, w, "script", script.Text+"\n小白看向灯影。\n")
	d, e := s.dramaDoc(w.ID, "assets")
	if e != nil || d.State != "stale" {
		t.Fatalf("dependent not stale: %+v %v", d, e)
	}
	path, _ := s.dramaFile(w.ID, dramaPaths["script"])
	if e = os.WriteFile(path, []byte(script.Text+"\n外部修改"), 0600); e != nil {
		t.Fatal(e)
	}
	d, e = s.dramaDoc(w.ID, "script")
	if e != nil || d.State != "stale" {
		t.Fatalf("external change accepted: %+v %v", d, e)
	}
}
func TestDramaCandidateSnapshotAndIdempotency(t *testing.T) {
	s := testStudio(t)
	w := dramaTestWork(t, s)
	id := uid()
	v := map[string]any{"kind": "script", "base": "", "operation_id": id}
	request(t, s, "POST", "/api/drama/works/"+w.ID+"/create", v, 200)
	waitTask(t, s, id)
	request(t, s, "POST", "/api/drama/works/"+w.ID+"/create", v, 200)
	tasks, _ := s.store.tasks(w.ID)
	if len(tasks) != 1 {
		t.Fatal("duplicate generation")
	}
	j, e := s.readDramaJob(id)
	if e != nil || j.Prompt == "" || j.Candidate == "" {
		t.Fatalf("snapshot missing: %v", e)
	}
	doc, e := s.dramaDoc(w.ID, "script")
	if e != nil || doc.Text != "" {
		t.Fatal("candidate overwrote source")
	}
	dramaTestSave(t, s, w, "script", dramaMock("script", 5)+"\n人工修改。")
	request(t, s, "POST", "/api/drama/jobs/"+id+"/adopt", map[string]any{}, 409)
}
func TestDramaStructureAndCrossDocumentChecks(t *testing.T) {
	s := testStudio(t)
	w := dramaTestWork(t, s)
	dramaTestSave(t, s, w, "script", dramaMock("script", 5))
	dramaTestSave(t, s, w, "assets", dramaMock("assets", 5))
	dramaTestSave(t, s, w, "storyboard", dramaMock("storyboard", 5))
	for _, text := range []string{strings.Replace(dramaMock("motion", 5), "时长：5s", "时长：8s", 1), strings.Replace(dramaMock("motion", 5), "两只猫在纸板上相邻伏下。", "两只猫跑远。", 1)} {
		_, e := s.saveDramaDoc(w.ID, "motion", "", text, true, nil)
		if e == nil {
			t.Fatal("inconsistent motion adopted")
		}
	}
	_, issues := dramaParse("script", dramaMock("script", 5)+"\n## EP001-SC001 重复\n正文")
	if len(issues) == 0 {
		t.Fatal("duplicate ID not detected")
	}
}
func TestDramaQuoteBindingAndConfigurationInvalidation(t *testing.T) {
	s := testStudio(t)
	w := dramaTestWork(t, s)
	dramaTestSave(t, s, w, "script", dramaMock("script", 5))
	dramaTestSave(t, s, w, "assets", dramaMock("assets", 5))
	dramaTestSave(t, s, w, "images", dramaMock("images", 5))
	var q DramaQuote
	b := request(t, s, "POST", "/api/drama/works/"+w.ID+"/quotes", map[string]any{"kind": "image", "document": "images", "entry": "IMG-AHUI", "prompt": "injected"}, 200)
	json.Unmarshal(b, &q)
	if strings.Contains(q.Spec.Prompt, "injected") {
		t.Fatal("client prompt trusted")
	}
	cfg, _ := s.store.settings()
	cfg.Version++
	s.store.db.Exec("UPDATE settings SET body=?", jsonBytes(cfg))
	request(t, s, "POST", "/api/drama/quotes/"+q.ID+"/confirm", map[string]any{}, 409)
}
func TestDramaUnknownRecoveryNeverSubmits(t *testing.T) {
	s := testStudio(t)
	w := dramaTestWork(t, s)
	cfg, _ := s.store.settings()
	cfg.Mode = "remote"
	j := DramaJob{ID: uid(), WorkID: w.ID, Kind: "script", Prompt: "test", SkillVersion: dramaVersion}
	task, e := s.newDramaTask(w, cfg, j)
	if e != nil {
		t.Fatal(e)
	}
	s.store.db.Exec("INSERT INTO attempts(id,task_id,status) VALUES(?,?,'submitting')", task.ID+":creative", task.ID)
	s.launchDrama(task.ID)
	deadline := time.Now().Add(time.Second * 3)
	for time.Now().Before(deadline) {
		task, _ = s.store.task(task.ID)
		if task.Status == "failed" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if task.Status != "failed" {
		t.Fatal(task)
	}
	var count int
	s.store.db.QueryRow("SELECT count(*) FROM attempts WHERE task_id=?", task.ID).Scan(&count)
	if count != 1 {
		t.Fatal("new attempt created")
	}
}
func TestDramaReferencesRequireRealApprovedFiles(t *testing.T) {
	s := testStudio(t)
	w := dramaTestWork(t, s)
	_, e := s.dramaBindings(w.ID, "PLAN-HERO（顺序：1）· IMG-AHUI《阿灰》（用途：身份；控制：毛色；不得控制：动作）")
	if e == nil {
		t.Fatal("PLAN submitted")
	}
	file, e := s.writeMedia(w.ID, "ref.png", mockPNG("ref", false, false))
	if e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(s.media(file))
	m := DramaMedia{ID: uid(), WorkID: w.ID, Kind: "image", File: file, Path: "输入/阿灰.png", Hash: digest(b)}
	s.saveDramaMedia(m)
	decl := "REF-AHUI（顺序：1）· 输入/阿灰.png《阿灰》（用途：身份；控制：毛色；不得控制：动作）"
	if _, e = s.dramaBindings(w.ID, decl); e == nil {
		t.Fatal("unapproved reference accepted")
	}
	m.Approved = true
	s.saveDramaMedia(m)
	refs, e := s.dramaBindings(w.ID, decl)
	if e != nil || len(refs) != 1 {
		t.Fatal(e)
	}
	os.WriteFile(s.media(file), []byte("tamper"), 0600)
	if _, e = s.dramaBindings(w.ID, decl); e == nil {
		t.Fatal("changed reference accepted")
	}
}
func TestDramaSnapshotDoesNotUseLegacyTemplates(t *testing.T) {
	s := testStudio(t)
	w := dramaTestWork(t, s)
	p, refs, e := s.dramaPrompt(w, "script", "少对白", map[string]string{})
	if e != nil || len(refs) < 3 {
		t.Fatal(e)
	}
	if !strings.Contains(p, "短剧写作") || strings.Contains(p, "{{idea}}") {
		t.Fatal("wrong prompt source")
	}
	cfg, _ := s.store.settings()
	cfg.PromptTemplates = map[string]string{"analyze": "legacy-custom"}
	j := DramaJob{ID: uid(), WorkID: w.ID, Kind: "script", Prompt: p}
	task, e := s.newDramaTask(w, cfg, j)
	if e != nil {
		t.Fatal(e)
	}
	loaded, _ := s.store.task(task.ID)
	if len(loaded.Config.PromptTemplates) > 0 {
		t.Fatal("legacy templates leaked")
	}
}
func TestMockFilmIntegrationDrama(t *testing.T)       { runMockDramaFilm(t, false) }
func TestMockFilmIntegrationDramaSeries(t *testing.T) { runMockDramaFilm(t, true) }
func TestMockFilmIntegrationDramaGuide(t *testing.T)  { runMockDramaFilm(t, true, true) }
func runMockDramaFilm(t *testing.T, series bool, guided ...bool) {
	if os.Getenv("LYPIT_INTEGRATION") != "1" {
		t.Skip("set LYPIT_INTEGRATION=1")
	}
	s := testStudio(t)
	var w DramaWork
	if series {
		v := seriesTestCreate(t, s)
		w = seriesTestEpisode(t, s, v, 2, nil)
	} else {
		w = dramaTestWork(t, s)
	}
	mock := func(kind string) string { return strings.ReplaceAll(dramaMock(kind, 5), "EP001", dramaEpisodeCode(w)) }
	dramaTestSave(t, s, w, "script", mock("script"))
	dramaTestSave(t, s, w, "assets", mock("assets"))
	dramaTestSave(t, s, w, "images", mock("images"))
	var quote DramaQuote
	json.Unmarshal(request(t, s, "POST", "/api/drama/works/"+w.ID+"/quotes", map[string]any{"kind": "image", "document": "images", "entry": "IMG-AHUI"}, 200), &quote)
	var task Task
	json.Unmarshal(request(t, s, "POST", "/api/drama/quotes/"+quote.ID+"/confirm", map[string]any{}, 200), &task)
	waitTask(t, s, task.ID)
	var again Task
	json.Unmarshal(request(t, s, "POST", "/api/drama/quotes/"+quote.ID+"/confirm", map[string]any{}, 200), &again)
	if again.ID != task.ID {
		t.Fatal("duplicate confirmed task")
	}
	medias, _ := s.dramaMedias(w.ID)
	im := medias[0]
	request(t, s, "POST", "/api/drama/works/"+w.ID+"/media/"+im.ID+"/approve", map[string]any{}, 200)
	decl := "REF-START（顺序：1）· " + im.Path + "《本镜起始帧》（用途：起始帧；控制：构图；不得控制：终态）"
	board := strings.Replace(mock("storyboard"), "无（待补参考图：本镜起始帧）", decl, 1)
	motion := strings.Replace(mock("motion"), "无（待补参考图：本镜起始帧）", decl, 1)
	dramaTestSave(t, s, w, "storyboard", board)
	if len(guided) > 0 && guided[0] {
		shotID := "SHOT-" + dramaEpisodeCode(w) + "-001"
		json.Unmarshal(request(t, s, "POST", "/api/drama/works/"+w.ID+"/quotes", map[string]any{"kind": "image", "document": "storyboard", "entry": shotID}, 200), &quote)
		json.Unmarshal(request(t, s, "POST", "/api/drama/quotes/"+quote.ID+"/confirm", map[string]any{}, 200), &task)
		waitTask(t, s, task.ID)
		media, _ := s.dramaMedias(w.ID)
		frame := media[0]
		request(t, s, "POST", "/api/drama/works/"+w.ID+"/media/"+frame.ID+"/approve", map[string]any{}, 200)
		current, _ := s.dramaDoc(w.ID, "storyboard")
		var bound DramaDoc
		json.Unmarshal(request(t, s, "POST", "/api/drama/works/"+w.ID+"/bind-frame", map[string]any{"base": current.Hash, "entry": shotID, "media_id": frame.ID}, 200), &bound)
		adopted := dramaTestSave(t, s, w, "storyboard", bound.Text)
		for _, entry := range adopted.Entries {
			if entry.ID == shotID {
				motion = strings.ReplaceAll(motion, decl, entry.Fields["输入参考图"])
			}
		}
	}
	dramaTestSave(t, s, w, "motion", motion)
	json.Unmarshal(request(t, s, "POST", "/api/drama/works/"+w.ID+"/quotes", map[string]any{"kind": "video", "document": "motion", "entry": "MOTION-" + dramaEpisodeCode(w) + "-001"}, 200), &quote)
	json.Unmarshal(request(t, s, "POST", "/api/drama/quotes/"+quote.ID+"/confirm", map[string]any{}, 200), &task)
	waitTask(t, s, task.ID)
	medias, _ = s.dramaMedias(w.ID)
	video := medias[0]
	request(t, s, "POST", "/api/drama/works/"+w.ID+"/media/"+video.ID+"/approve", map[string]any{}, 200)
	edit := "# 剪辑单\n\n## CUT-EP001-001 · 让出暖处\n- 来源：MOTION-EP001-001 · " + video.Path + "\n- 入点：0.00\n- 出点：5.00\n- 时长：5.00\n- 取舍：入点=动作前；出点=落定后\n- 声音：保留原声\n- 字幕：无\n"
	dramaTestSave(t, s, w, "edit", strings.ReplaceAll(edit, "EP001", dramaEpisodeCode(w)))
	json.Unmarshal(request(t, s, "POST", "/api/drama/works/"+w.ID+"/render", map[string]any{}, 200), &task)
	waitTask(t, s, task.ID)
	medias, _ = s.dramaMedias(w.ID)
	film := medias[0]
	if !strings.Contains(film.Path, "剧集/"+dramaEpisodeCode(w)+"/") {
		t.Fatal("wrong episode path", film.Path)
	}
	if film.Kind != "film" || absFloat(film.Duration-5) > 0.2 {
		t.Fatal(film)
	}
	if _, e := os.Stat(filepath.Join(s.data, "media", filepath.FromSlash(film.File))); e != nil {
		t.Fatal(e)
	}
	if e := s.requireAudio(context.Background(), film.File); e != nil {
		t.Fatal(e)
	}
}

func TestDramaUpstreamScreenplayParity(t *testing.T) {
	python, e := exec.LookPath("python3")
	if e != nil {
		t.Skip("Python only needed for development parity")
	}
	raw, e := os.ReadFile("testdata/drama-screenplay.md")
	if e != nil {
		t.Fatal(e)
	}
	for _, duplicate := range []bool{false, true} {
		text := string(raw)
		if duplicate {
			text += "\n## EP001-SC001 重复场景\n正文\n"
		}
		_, issues := dramaParse("script", text)
		if (len(issues) > 0) != duplicate {
			t.Fatal("Go checker disagrees")
		}
		root := t.TempDir()
		ep := filepath.Join(root, "剧集", "EP001")
		os.MkdirAll(ep, 0700)
		os.WriteFile(filepath.Join(ep, "剧本.md"), []byte(text), 0600)
		cmd := exec.Command(python, "-B", "drama_bundle/skills/short-drama/scripts/creator_markdown_check.py", ep, "--project-root", root, "--partial")
		b, e := cmd.CombinedOutput()
		if (e != nil) != duplicate {
			t.Fatalf("upstream disagrees: %v %s", e, b)
		}
	}
}

func TestDramaBatchOnlyQuotesMissingEntries(t *testing.T) {
	s := testStudio(t)
	w := dramaTestWork(t, s)
	dramaTestSave(t, s, w, "script", dramaMock("script", 5))
	dramaTestSave(t, s, w, "assets", dramaMock("assets", 5))
	dramaTestSave(t, s, w, "images", dramaMock("images", 5))
	cfg, _ := s.store.settings()
	p, _, e := s.buildDramaProduction(w, DramaProduction{Kind: "image", Document: "images", Entry: "*"}, cfg)
	if e != nil || len(p.Items) != 1 {
		t.Fatalf("missing batch: %+v %v", p, e)
	}
	s.saveDramaMedia(DramaMedia{ID: uid(), WorkID: w.ID, Entry: "IMG-AHUI", Kind: "image", File: w.ID + "/existing.png"})
	_, _, e = s.buildDramaProduction(w, DramaProduction{Kind: "image", Document: "images", Entry: "*"}, cfg)
	if e == nil {
		t.Fatal("existing result requoted")
	}
}
func TestDramaReviewCanInspectDraftWithoutAdoptingIt(t *testing.T) {
	s := testStudio(t)
	w := dramaTestWork(t, s)
	_, e := s.saveDramaDoc(w.ID, "script", "", dramaMock("script", 5), false, nil)
	if e != nil {
		t.Fatal(e)
	}
	inputs, e := s.dramaInputs(w.ID, "review")
	if e != nil || inputs["script"] == "" {
		t.Fatal(e)
	}
	doc, e := s.saveDramaDoc(w.ID, "review", "", dramaMock("review", 5), true, inputs)
	if e != nil || doc.State != "accepted" {
		t.Fatal(doc, e)
	}
	script, _ := s.dramaDoc(w.ID, "script")
	if script.State != "draft" {
		t.Fatal("review adopted source")
	}
}
func TestDramaTimelineCaptionsAndCrossCutAudio(t *testing.T) {
	s := testStudio(t)
	m := DramaMedia{ID: uid(), WorkID: uid(), Path: "输入/音乐.wav", Kind: "audio", Approved: true, Current: true}
	a, e := s.parseDramaAudio("音乐", "0 输入/音乐.wav（起点：1.5；增益：-12）", []DramaMedia{m}, 5)
	if e != nil || !a.Loop || a.Offset != 1.5 || a.Gain != -12 {
		t.Fatal(a, e)
	}
	c, e := dramaParseCaption("字幕 1", "0.25-1.75 你也来。", "小白：你也来。", 2)
	if e != nil || c.Start != 0.25 {
		t.Fatal(c, e)
	}
	if _, e = dramaParseCaption("字幕 1", "0-3 改写台词", "小白：你也来。", 2); e == nil {
		t.Fatal("invented/out-of-range caption accepted")
	}
	ass := dramaCaptions([]DramaCut{{Duration: 2}, {Duration: 3, Captions: []DramaCaption{c}}}, 360, 640)
	if !strings.Contains(ass, "0:00:02.25,0:00:03.75") {
		t.Fatal("caption not placed on episode timeline")
	}
}

func TestDramaProfileInvalidatesStoryboardWithoutChangingScript(t *testing.T) {
	s := testStudio(t)
	w := dramaTestWork(t, s)
	dramaTestSave(t, s, w, "script", dramaMock("script", 5))
	dramaTestSave(t, s, w, "assets", dramaMock("assets", 5))
	dramaTestSave(t, s, w, "storyboard", dramaMock("storyboard", 5))
	cfg, _ := s.store.settings()
	cfg.Video.Model = "doubao-seedance-2-5-test"
	cfg.Version++
	s.store.db.Exec("UPDATE settings SET body=?", jsonBytes(cfg))
	request(t, s, "PUT", "/api/drama/works/"+w.ID+"/profile", map[string]any{"base": w.ProfileHash, "mode": "first_frame", "language": "en"}, 200)
	board, e := s.dramaDoc(w.ID, "storyboard")
	if e != nil || board.State != "stale" {
		t.Fatal(board, e)
	}
	script, e := s.dramaDoc(w.ID, "script")
	if e != nil || script.State != "accepted" {
		t.Fatal(script, e)
	}
	policy := dramaProfile(Settings{Mode: "remote", Video: Endpoint{Model: "doubao-seedance-2-5-pro"}}, "first_frame", "en")
	if policy.Min != 4 || policy.Max != 30 {
		t.Fatal("retired editorial limit leaked into new workflow", policy)
	}
}

func TestDramaBatchWorkersRespectLimitAndCoordinateUsage(t *testing.T) {
	s := testStudio(t)
	w := dramaTestWork(t, s)
	cfg, _ := s.store.settings()
	cfg.ImageConcurrency = 2
	j := DramaJob{ID: uid(), WorkID: w.ID, Kind: "produce", Production: &DramaProduction{Kind: "batch", Items: []DramaProduction{{Kind: "image", Entry: "IMG-A"}, {Kind: "image", Entry: "IMG-B"}, {Kind: "image", Entry: "IMG-C"}, {Kind: "image", Entry: "IMG-D"}}}}
	task, e := s.newDramaTask(w, cfg, j)
	if e != nil {
		t.Fatal(e)
	}
	var active, max atomic.Int32
	e = s.executeDramaBatch(context.Background(), &task, &j, func(ctx context.Context, worker *Task, child *DramaJob) error {
		n := active.Add(1)
		defer active.Add(-1)
		for {
			old := max.Load()
			if n <= old || max.CompareAndSwap(old, n) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		worker.Input.Title = child.Production.Entry
		return worker.usageRecorder(VideoUsage{ShotID: child.Production.Entry, RemoteID: child.Production.Entry})
	})
	if e != nil || max.Load() != 2 || len(task.VideoUsage) != 4 || task.Input.Title != w.Title {
		t.Fatalf("limit/snapshot/usage: max=%d usages=%d err=%v", max.Load(), len(task.VideoUsage), e)
	}
}
func TestDramaBatchRecoveryBarrierBeforeNewAdmission(t *testing.T) {
	s := testStudio(t)
	w := dramaTestWork(t, s)
	cfg, _ := s.store.settings()
	j := DramaJob{ID: uid(), WorkID: w.ID, Kind: "produce", Production: &DramaProduction{Kind: "batch", Items: []DramaProduction{{Kind: "image", Entry: "IMG-FRESH"}, {Kind: "image", Entry: "IMG-OLD"}}}}
	task, e := s.newDramaTask(w, cfg, j)
	if e != nil {
		t.Fatal(e)
	}
	key := "shot-drama-" + digest([]byte("IMG-OLD"))[:16]
	s.store.db.Exec("INSERT INTO attempts(id,task_id,status) VALUES(?,?,'submitting')", task.ID+":"+key, task.ID)
	calls := []string{}
	e = s.executeDramaBatch(context.Background(), &task, &j, func(ctx context.Context, worker *Task, child *DramaJob) error {
		calls = append(calls, child.Production.Entry)
		return errors.New("unknown result")
	})
	if e == nil || len(calls) != 1 || calls[0] != "IMG-OLD" {
		t.Fatal("new work admitted before recovery", calls, e)
	}
}

func TestMockFilmIntegrationDramaTimeline(t *testing.T) {
	if os.Getenv("LYPIT_INTEGRATION") != "1" {
		t.Skip("set LYPIT_INTEGRATION=1")
	}
	s := testStudio(t)
	w := dramaTestWork(t, s)
	dramaTestSave(t, s, w, "script", dramaMock("script", 5)+"\n[画面文字] 路灯下\n")
	dramaTestSave(t, s, w, "assets", dramaMock("assets", 5))
	dramaTestSave(t, s, w, "storyboard", dramaMock("storyboard", 5))
	dramaTestSave(t, s, w, "motion", dramaMock("motion", 5))
	os.MkdirAll(s.media(w.ID), 0700)
	video := w.ID + "/import-video.mp4"
	_, e := runCommand(s.ctx, s.root, "ffmpeg", "-v", "error", "-y", "-f", "lavfi", "-i", "color=c=blue:s=360x640:r=24:d=5", "-f", "lavfi", "-i", "anullsrc=r=48000:cl=stereo", "-t", "5", "-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac", s.media(video))
	if e != nil {
		t.Fatal(e)
	}
	audio := w.ID + "/music.wav"
	if _, e = runCommand(s.ctx, s.root, "ffmpeg", "-v", "error", "-y", "-f", "lavfi", "-i", "sine=frequency=330:duration=2", s.media(audio)); e != nil {
		t.Fatal(e)
	}
	add := func(entry, kind, file string) DramaMedia {
		b, _ := os.ReadFile(s.media(file))
		m := DramaMedia{ID: uid(), WorkID: w.ID, Entry: entry, Kind: kind, File: file, Path: "剧集/EP001/制作成果/" + filepath.Base(file), Hash: digest(b), Approved: true}
		if e := s.saveDramaMedia(m); e != nil {
			t.Fatal(e)
		}
		return m
	}
	v := add("MOTION-EP001-001", "video", video)
	m := add("导入音乐", "audio", audio)
	edit := fmt.Sprintf("# 剪辑单\n\n## CUT-EP001-001 · 前半\n- 来源：MOTION-EP001-001 · %s\n- 入点：0.00\n- 出点：2.50\n- 时长：2.50\n- 取舍：入点=等待；出点=接动作\n- 声音：保留原声\n- 字幕 1：0.25-1.75 你也来。\n- 画面文字 1：0.00-1.00 普通 路灯下\n- 音乐：0 %s（增益：-12）\n\n## CUT-EP001-002 · 后半\n- 来源：MOTION-EP001-001 · %s\n- 入点：2.50\n- 出点：5.00\n- 时长：2.50\n- 取舍：入点=接动作；出点=落定\n- 声音：保留原声，音乐跨切点\n- 字幕：无\n", v.Path, m.Path, v.Path)
	dramaTestSave(t, s, w, "edit", edit)
	var task Task
	json.Unmarshal(request(t, s, "POST", "/api/drama/works/"+w.ID+"/render", map[string]any{}, 200), &task)
	waitTask(t, s, task.ID)
	all, _ := s.dramaMedias(w.ID)
	film := all[0]
	if film.Kind != "film" || film.Verification == nil || film.Verification.Loudness == "-inf" || !film.Verification.FullDecode {
		t.Fatalf("mixed audio verification missing: %+v", film)
	}
}

func TestDramaFramePaddingKeepsOriginalAndNeedsApproval(t *testing.T) {
	s := testStudio(t)
	w := dramaTestWork(t, s)
	raw := mockPNG("original", true, false)
	file, e := s.writeMedia(w.ID, "square.png", raw)
	if e != nil {
		t.Fatal(e)
	}
	source := DramaMedia{ID: uid(), WorkID: w.ID, File: file, Hash: digest(raw), Kind: "image", Current: true, Approved: true}
	m, e := s.padDramaFrame(w, source)
	if e != nil {
		t.Fatal(e)
	}
	if m.Approved || m.Parent != source.ID || m.File == source.File {
		t.Fatal("derivative replaced/approved original")
	}
	b, _ := os.ReadFile(s.media(m.File))
	cfg, _, e := image.DecodeConfig(bytes.NewReader(b))
	if e != nil || cfg.Width*16 != cfg.Height*9 {
		t.Fatal("bad frame ratio", cfg, e)
	}
	original, _ := os.ReadFile(s.media(source.File))
	if digest(original) != source.Hash {
		t.Fatal("original mutated")
	}
}
func TestDramaChineseCaptionUsesRealAlpha(t *testing.T) {
	f, e := dramaFont()
	if e != nil {
		t.Skip("system Chinese font unavailable: " + e.Error())
	}
	im, e := drawDramaCaption(f, DramaCaption{Text: "你也来。"}, 360, 640)
	if e != nil {
		t.Fatal(e)
	}
	_, _, _, a := im.At(0, 0).RGBA()
	if a != 0 {
		t.Fatal("caption background is opaque")
	}
	painted := false
	for y := 580; y < 640; y++ {
		for x := 0; x < 360; x++ {
			_, _, _, a := im.At(x, y).RGBA()
			painted = painted || a > 0
		}
	}
	if !painted {
		t.Fatal("Chinese glyphs not rendered")
	}
}

func TestDramaImageRecoveryReadsReceiptBeforeMissingReferences(t *testing.T) {
	s := testStudio(t)
	w := dramaTestWork(t, s)
	cfg, _ := s.store.settings()
	cfg.Mode = "remote"
	p := DramaProduction{Kind: "image", Entry: "IMG-RECOVER", Document: "images", Prompt: "saved prompt", Bindings: []DramaBinding{{Media: DramaMedia{WorkID: w.ID, File: w.ID + "/missing.png", Hash: "missing"}}}}
	j := DramaJob{ID: uid(), WorkID: w.ID, Kind: "produce", Production: &p}
	task, e := s.newDramaTask(w, cfg, j)
	if e != nil {
		t.Fatal(e)
	}
	key := "shot-drama-" + digest([]byte(p.Entry))[:16]
	raw := mockPNG("receipt", false, false)
	receipt := jsonBytes(map[string]any{"data": []map[string]string{{"b64_json": base64.StdEncoding.EncodeToString(raw)}}})
	s.store.db.Exec("INSERT INTO attempts(id,task_id,status,response) VALUES(?,?,'received',?)", task.ID+":"+key, task.ID, receipt)
	if e = s.executeDramaProduction(context.Background(), &task, &j); e != nil {
		t.Fatal(e)
	}
	var n int
	s.store.db.QueryRow("SELECT count(*) FROM attempts WHERE task_id=?", task.ID).Scan(&n)
	if n != 1 {
		t.Fatal("recovery submitted again")
	}
}

func TestDramaEntryIDsCannotBecomeOutputPaths(t *testing.T) {
	_, issues := dramaParse("images", "# 图片\n\n## IMG-../../escape · bad\n- 用途：主图\n\n### 可复制提示词\n> cat\n")
	if len(issues) == 0 {
		t.Fatal("path ID accepted")
	}
	s := testStudio(t)
	if _, e := s.writeMedia("project", "../escape.png", []byte("x")); e == nil {
		t.Fatal("unsafe media filename accepted")
	}
}
