package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDramaCandidateConservativeFormatRepair(t *testing.T) {
	source := dramaMock("storyboard", 5) + "\n## 交接、待选择项与文字审查\n- 自审结论：仅检查文字。\n"
	fixed, check := checkDramaCandidate("storyboard", "EP001", source)
	if len(check.Repairs) != 1 || len(check.Issues) != 0 || !strings.Contains(fixed, "### 交接、待选择项与文字审查") {
		t.Fatal(check)
	}
	entries, _ := dramaParse("storyboard", fixed)
	if len(entries) != 2 || entries[1].ID != "SHOT-EP001-001" || entries[1].Fields["时长"] != "5s" {
		t.Fatal(entries)
	}
	again, next := checkDramaCandidate("storyboard", "EP001", fixed)
	if again != fixed || len(next.Repairs) != 0 || len(next.Issues) != 0 {
		t.Fatal("repair not idempotent")
	}
	// A plausible damaged real shot must never be hidden as a note.
	damaged := dramaMock("storyboard", 5) + "\n## 交接、待选择项与文字审查\n- 时长：4s\n"
	out, issue := checkDramaCandidate("storyboard", "EP001", damaged)
	if out != damaged || len(issue.Repairs) != 0 || len(issue.Issues) == 0 {
		t.Fatal("damaged shot hidden", issue)
	}
	// Unknown headings, ID typos and missing fields remain visible errors.
	for _, bad := range []string{strings.Replace(source, "## SHOT-EP001-001", "## SH0T-EP001-001", 1), strings.Replace(source, "- 来源：EP001-SC001\n", "", 1)} {
		_, check := checkDramaCandidate("storyboard", "EP001", bad)
		if len(check.Issues) == 0 {
			t.Fatal("ambiguous content invented")
		}
	}
	wrapped := "```markdown\n" + dramaMock("images", 5) + "```\n"
	out, issue = checkDramaCandidate("images", "EP001", wrapped)
	if len(issue.Repairs) != 1 || strings.HasPrefix(out, "```") || len(issue.Issues) != 0 {
		t.Fatal(issue)
	}
	emptyFence := "```markdown\n```\n"
	out, issue = checkDramaCandidate("images", "EP001", emptyFence)
	if out != emptyFence || len(issue.Issues) == 0 {
		t.Fatal("empty wrapper lost its original content", issue)
	}
	// A notes-only response is not transformed into an apparently valid production document.
	_, issue = checkDramaCandidate("images", "EP001", "## 文字自审与交接\n说明")
	if len(issue.Repairs) != 0 || len(issue.Issues) == 0 {
		t.Fatal(issue)
	}
}
func TestDramaCandidateLocalCheckPreservesOriginalAndDoesNotGenerate(t *testing.T) {
	s := testStudio(t)
	w := dramaTestWork(t, s)
	cfg, _ := s.store.settings()
	original := dramaMock("images", 5) + "\n## 文字自审与交接\n- 结论：待核对。\n"
	j := DramaJob{ID: uid(), WorkID: w.ID, Kind: "images", Candidate: original, Prompt: "private prompt"}
	task, e := s.newDramaTask(w, cfg, j)
	if e != nil {
		t.Fatal(e)
	}
	task.Status = "completed"
	s.store.saveTask(task)
	path := "/api/drama/jobs/" + j.ID + "/check-format"
	var checked DramaJob
	json.Unmarshal(request(t, s, "POST", path, map[string]any{}, 200), &checked)
	if checked.CandidateOriginal != original || len(checked.CandidateCheck.Repairs) != 1 || len(checked.CandidateCheck.Issues) != 0 {
		t.Fatal(checked)
	}
	var second DramaJob
	json.Unmarshal(request(t, s, "POST", path, map[string]any{}, 200), &second)
	if string(jsonBytes(checked)) != string(jsonBytes(second)) {
		t.Fatal("local check changed on retry")
	}
	stored, _ := s.readDramaJob(j.ID)
	if stored.Prompt != "private prompt" {
		t.Fatal("lost private snapshot")
	}
	d, _ := s.dramaDoc(w.ID, "images")
	if d.Text != "" || checked.Applied {
		t.Fatal("rewrote/adopted document")
	}
	var count int
	s.store.db.QueryRow("SELECT count(*) FROM attempts").Scan(&count)
	if count != 0 {
		t.Fatal("model attempt created")
	}
	s.store.db.QueryRow("SELECT count(*) FROM tasks").Scan(&count)
	if count != 1 {
		t.Fatal("new task created")
	}
	stored.Applied = true
	s.saveDramaJob(stored)
	request(t, s, "POST", path, map[string]any{}, 400)
}
func TestDramaGenerationChecksCandidateBeforeConfirmation(t *testing.T) {
	s := testStudio(t)
	w := dramaTestWork(t, s)
	id := uid()
	request(t, s, "POST", "/api/drama/works/"+w.ID+"/create", map[string]any{"kind": "script", "base": "", "operation_id": id}, 200)
	waitTask(t, s, id)
	j, e := s.readDramaJob(id)
	if e != nil || j.CandidateCheck == nil || j.CandidateCheck.Hash != digest([]byte(j.Candidate)) || len(j.CandidateCheck.Issues) != 0 || j.Applied {
		t.Fatal(j, e)
	}
	d, _ := s.dramaDoc(w.ID, "script")
	if d.Text != "" {
		t.Fatal("automatic adoption")
	}
}

func TestDramaMotionNotesRepairKeepsEndpointDifferencesVisible(t *testing.T) {
	s := testStudio(t)
	w := dramaTestWork(t, s)
	dramaTestSave(t, s, w, "script", dramaMock("script", 5))
	dramaTestSave(t, s, w, "assets", dramaMock("assets", 5))
	dramaTestSave(t, s, w, "storyboard", dramaMock("storyboard", 5))
	source := strings.Replace(dramaMock("motion", 5), "两只猫在纸板上相邻伏下。", "两只猫各自坐下。", 1) + "\n## 逐镜挂图清单与投产边界\n| 镜头 | 挂图定位 |\n| --- | --- |\n| SHOT-EP001-001 | 本镜冻结关键帧 |\n\n## 待选择项与文字审查\n- 文字自审结论：待核对。\n"
	j := DramaJob{WorkID: w.ID, Kind: "motion", Candidate: source, CandidateCheck: &DramaCandidateCheck{Version: 1, Hash: digest([]byte(source))}}
	prepareDramaCandidate(w, &j, source)
	s.checkDramaCandidateLinks(&j)
	if j.CandidateOriginal != source || len(j.CandidateCheck.Repairs) != 2 || len(j.CandidateCheck.Issues) != 1 || j.CandidateCheck.Issues[0].Rule != "LINK" || !strings.Contains(j.Candidate, "- 终点：两只猫各自坐下。") {
		t.Fatal(j.CandidateCheck)
	}
	prompt, _, err := s.dramaPrompt(w, "motion", "", map[string]string{"storyboard": "snapshot"})
	if err != nil || !strings.Contains(prompt, "终点字段必须逐字复制") || !strings.Contains(prompt, "说明使用三级标题") {
		t.Fatal("missing motion structure contract", err)
	}
	// Never hide a damaged production entry, even with a recognized note title.
	for _, field := range []string{"- 时长：5s", "* 分镜: SHOT-EP001-001"} {
		damaged := dramaMock("motion", 5) + "\n## 待选择项与文字审查\n" + field + "\n"
		out, check := checkDramaCandidate("motion", "EP001", damaged)
		if out != damaged || len(check.Repairs) != 0 || len(check.Issues) == 0 {
			t.Fatal(check)
		}
	}
	// Check version upgrade does not bypass the new repairs and remains idempotent.
	before := string(jsonBytes(j))
	prepareDramaCandidate(w, &j, j.Candidate)
	if string(jsonBytes(j)) != before {
		t.Fatal("repeat changed repair history")
	}
}
