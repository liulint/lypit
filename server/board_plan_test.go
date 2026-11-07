package main

import (
	"context"
	"strings"
	"testing"
)

func timedPlanFixture() (Project, []Shot) {
	p := Project{Duration: 30, AudioMode: "dialogue", Assets: []Asset{{ID: "cat", Kind: "character", Name: "橘猫"}, {ID: "kitten", Kind: "character", Name: "小灰猫"}}, Script: "片名：老师，回家了。橘猫声音轻柔微哑。\n【0—5秒｜窗外】\n橘猫扒窗。\n橘猫：有一天，我也要当老师。\n\n【5—10秒｜猫窝】\n拖纸箱，修补漏雨一角。\n\n【10—17秒｜路灯】\n先练习，再依偎。\n橘猫：小朋友，冷了怎么办？\n小灰猫：靠近一点！\n橘猫：对，谁也别落下。\n\n【17—22秒｜成绩】\n看到合格，叼证明。\n\n【22—30秒｜原址】\n先空荡，后重逢。\n橘猫：老师考上了……你们呢？\n小灰猫：老师，我们等你好久了。"}
	beats, _ := scriptBeats(p)
	shots := []Shot{}
	for _, b := range beats {
		shots = append(shots, Shot{ID: uid(), Title: "原剧情", SourceText: b.Text, FirstFrame: "动作开始之前", Description: "动作结束后再拥抱", Duration: b.End - b.Start, Narration: strings.Join(scriptDialogue(p, b.Text), "\n"), References: []string{"cat", "kitten"}})
	}
	return p, shots
}

func TestFaithfulPlanKeepsScriptTimingAndDialogue(t *testing.T) {
	p, shots := timedPlanFixture()
	if err := validateFaithfulPlan(p, shots); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func([]Shot){
		func(q []Shot) { q[0].Duration = 8; q[1].Duration = 2 },
		func(q []Shot) { q[2].Narration = strings.ReplaceAll(q[2].Narration, "小朋友，", "") },
		func(q []Shot) { q[1].Narration = q[0].Narration; q[0].Narration = "" },
		func(q []Shot) { q[2].FirstFrame = "" },
		func(q []Shot) { q[1].SourceText = "不是剧本中的内容" },
	} {
		candidate, _ := decodeBody[[]Shot](jsonBytes(shots))
		change(candidate)
		if validateFaithfulPlan(p, candidate) == nil {
			t.Fatal("accepted drift")
		}
	}
	a, b := shots[1], shots[1]
	a.Duration = 2
	b.Duration = 3
	divided := append([]Shot{shots[0], a, b}, shots[2:]...)
	if err := validateFaithfulPlan(p, divided); err != nil {
		t.Fatal("natural subdivision rejected", err)
	}
	if frame := boardFrame(shots[2]); strings.Contains(frame, shots[2].Description) || !strings.Contains(frame, shots[2].FirstFrame) {
		t.Fatal("image prompt leaked video action", frame)
	}
	legacy := Shot{Description: "historical exact prompt"}
	if boardFrame(legacy) != legacy.Description {
		t.Fatal("legacy changed")
	}
}

func TestBoardPlanningRequiresReviewAndPreservesExistingMedia(t *testing.T) {
	s := testStudio(t)
	p := readyRetryProject(t, s)
	var err error
	p, err = s.store.update(p.ID, p.Revision, func(p *Project) error { p.Shots[0].Video = "prior-version.mp4"; return nil })
	if err != nil {
		t.Fatal(err)
	}
	old := string(jsonBytes(p.Shots))
	p = stage(t, s, p, "board_plan")
	if string(jsonBytes(p.Shots)) != old || len(p.PlannedShots) == 0 {
		t.Fatal("planning replaced media")
	}
	if _, err := s.quoteFor(Project{Duration: 30}, defaultSettings(), "boards"); err == nil {
		t.Fatal("unreviewed image quote accepted")
	}
	request(t, s, "PUT", "/api/projects/"+p.ID+"/board-plan", map[string]any{"revision": p.Revision, "shots": p.PlannedShots, "approve": false}, 200)
	p = getProject(t, s, p.ID)
	if string(jsonBytes(p.Shots)) != old {
		t.Fatal("saving draft replaced media")
	}
	request(t, s, "PUT", "/api/projects/"+p.ID+"/board-plan", map[string]any{"revision": p.Revision, "shots": p.PlannedShots, "approve": true}, 400)
	for i := range p.PlannedShots {
		p.PlannedShots[i].ReviewStatus = "reviewed"
	}
	request(t, s, "PUT", "/api/projects/"+p.ID+"/board-plan", map[string]any{"revision": p.Revision, "shots": p.PlannedShots, "approve": false}, 200)
	p = getProject(t, s, p.ID)
	for _, q := range p.PlannedShots {
		if q.ReviewStatus != "reviewed" {
			t.Fatal("review status lost")
		}
	}
	request(t, s, "PUT", "/api/projects/"+p.ID+"/board-plan", map[string]any{"revision": p.Revision, "shots": p.PlannedShots, "approve": true}, 200)
	p = getProject(t, s, p.ID)
	if !p.PlanApproved || p.PlanVersion != 2 || string(jsonBytes(p.PreviousShots)) != old {
		t.Fatal("approval lost historical media")
	}
	q, err := s.quoteFor(p, defaultSettings(), "boards")
	if err != nil || len(q.Items) != 1 || q.Items[0].Count != len(p.Shots) {
		t.Fatal("wrong image scope", q, err)
	}
	for _, shot := range p.Shots {
		if len(shot.Candidates) != 0 || shot.Video != "" {
			t.Fatal("approval generated media")
		}
	}
	request(t, s, "PUT", "/api/projects/"+p.ID+"/board-plan", map[string]any{"revision": p.Revision - 1, "shots": p.Shots, "approve": true}, 409)
}

func TestTimedMockPlanDoesNotBalanceOrGenerateImages(t *testing.T) {
	p, _ := timedPlanFixture()
	s := testStudio(t)
	job := Task{ID: uid(), Stage: "board_plan", Mode: "mock", Input: p}
	if err := s.planFaithfulShots(context.Background(), &job); err != nil {
		t.Fatal(err)
	}
	if len(job.Input.PlannedShots) != 5 {
		t.Fatal("script was forced to four shots")
	}
	for i, want := range []int{5, 5, 7, 5, 8} {
		if job.Input.PlannedShots[i].Duration != want {
			t.Fatal("timing drift")
		}
	}
}

func TestIncompleteDraftCanBeSavedButCannotBeAdopted(t *testing.T) {
	s := testStudio(t)
	p := readyRetryProject(t, s)
	old := string(jsonBytes(p.Shots))
	p = stage(t, s, p, "board_plan")
	id := p.PlannedShots[0].ID
	p.PlannedShots[0].FirstFrame = ""
	p.PlannedShots[0].ReviewStatus = "changes"
	request(t, s, "PUT", "/api/projects/"+p.ID+"/board-plan", map[string]any{"revision": p.Revision, "shots": p.PlannedShots, "approve": false}, 200)
	p = getProject(t, s, p.ID)
	if p.PlannedShots[0].ID != id || p.PlannedShots[0].ReviewStatus != "changes" || p.PlannedShots[0].FirstFrame != "" || string(jsonBytes(p.Shots)) != old {
		t.Fatal("draft save lost state or replaced active media")
	}
	for i := range p.PlannedShots {
		p.PlannedShots[i].ReviewStatus = "reviewed"
	}
	request(t, s, "PUT", "/api/projects/"+p.ID+"/board-plan", map[string]any{"revision": p.Revision, "shots": p.PlannedShots, "approve": true}, 400)
	if string(jsonBytes(getProject(t, s, p.ID).Shots)) != old {
		t.Fatal("invalid adoption replaced media")
	}
	p.PlannedShots[0].ReviewStatus = "unexpected"
	request(t, s, "PUT", "/api/projects/"+p.ID+"/board-plan", map[string]any{"revision": p.Revision, "shots": p.PlannedShots, "approve": false}, 400)
}
