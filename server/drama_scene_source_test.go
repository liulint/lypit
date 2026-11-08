package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDramaSceneSourceEvidence(t *testing.T) {
	for _, value := range []string{"EP001-SC001", " EP001-SC001 ", "EP001-SC001「原文动作」", "EP001-SC001「动作一」「动作二」。"} {
		if dramaSourceScene(value) != "EP001-SC001" {
			t.Fatal(value)
		}
	}
	for _, value := range []string{"EP001-SC001 / EP001-SC002", "EP001-SC001X", "EP001-SC001「未闭合", "EP001-SC001 来源不确定", "EP001-SC001「动作」EP001-SC002"} {
		if dramaSourceScene(value) != "" {
			t.Fatal("guessed ambiguous reference", value)
		}
	}
	s := testStudio(t)
	w := dramaTestWork(t, s)
	dramaTestSave(t, s, w, "script", dramaMock("script", 5))
	source := strings.Replace(dramaMock("storyboard", 5), "- 来源：EP001-SC001", "- 来源：EP001-SC001「阿灰退开一步。」「小白走上纸板。」", 1)
	if e := s.validateDramaLinks(w.ID, "storyboard", source); e != nil {
		t.Fatal(e)
	}
	legacy := strings.ReplaceAll(dramaMock("script", 5), "EP001-SC001", "EP001-SC0001")
	dramaTestSave(t, s, w, "script", legacy)
	if e := s.validateDramaLinks(w.ID, "storyboard", strings.ReplaceAll(dramaMock("storyboard", 5), "EP001-SC001", "EP001-SC0001")); e != nil {
		t.Fatal("legacy exact source regressed", e)
	}
	dramaTestSave(t, s, w, "script", dramaMock("script", 5))
	bad := strings.Replace(source, "- 来源：EP001-SC001", "- 来源：EP001-SC999", 1)
	if e := s.validateDramaLinks(w.ID, "storyboard", bad); e == nil {
		t.Fatal("missing scene accepted")
	}
}
func TestDramaCandidateReferencesCheckedLocallyBeforeAdoption(t *testing.T) {
	s := testStudio(t)
	w := dramaTestWork(t, s)
	cfg, _ := s.store.settings()
	script := dramaTestSave(t, s, w, "script", dramaMock("script", 5))
	source := strings.Replace(dramaMock("storyboard", 5), "- 来源：EP001-SC001", "- 来源：EP001-SC001「引用证据」", 1)
	j := DramaJob{ID: uid(), WorkID: w.ID, Kind: "storyboard", Candidate: source}
	task, e := s.newDramaTask(w, cfg, j)
	if e != nil {
		t.Fatal(e)
	}
	task.Status = "completed"
	s.store.saveTask(task)
	path := "/api/drama/jobs/" + j.ID + "/check-format"
	read := func() DramaJob {
		var out DramaJob
		json.Unmarshal(request(t, s, "POST", path, map[string]any{}, 200), &out)
		return out
	}
	checked := read()
	if !checked.CandidateCheck.LinksChecked || len(checked.CandidateCheck.Issues) != 0 || checked.Candidate != source {
		t.Fatal(checked)
	}
	// Upstream changes must invalidate the local reference result even with identical candidate bytes.
	renamed := dramaTestSave(t, s, w, "script", strings.ReplaceAll(script.Text, "EP001-SC001", "EP001-SC002"))
	checked = read()
	if len(checked.CandidateCheck.Issues) != 1 || checked.CandidateCheck.Issues[0].Rule != "LINK" || checked.CandidateCheck.Issues[0].Line <= 1 {
		t.Fatal(checked.CandidateCheck)
	}
	dramaTestSave(t, s, w, "script", strings.ReplaceAll(renamed.Text, "EP001-SC002", "EP001-SC001"))
	checked = read()
	if len(checked.CandidateCheck.Issues) != 0 {
		t.Fatal("reference result was not refreshed", checked.CandidateCheck)
	}
	var n int
	s.store.db.QueryRow("SELECT count(*) FROM attempts").Scan(&n)
	if n != 0 {
		t.Fatal("check created model attempt")
	}
}
