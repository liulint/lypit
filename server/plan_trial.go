package main

import (
	"errors"
	"reflect"
	"strings"
)

// Trial work owns only one draft shot; active boards and whole-plan approval are unaffected.
func planTrialProject(p Project, id string) (Project, error) {
	start := 0
	for _, q := range p.PlannedShots {
		if q.ID != id {
			start += q.Duration
			continue
		}
		if q.ReviewStatus != "reviewed" {
			return p, errors.New("请先保存并审核通过当前镜头的文字规划")
		}
		if q.Duration < 1 || q.Duration > 12 || strings.TrimSpace(q.Title) == "" || strings.TrimSpace(q.FirstFrame) == "" || strings.TrimSpace(q.Description) == "" || strings.TrimSpace(q.SourceText) == "" || !strings.Contains(p.Script, q.SourceText) {
			return p, errors.New("当前镜头的时长、原文依据、首帧或视频动作无效，请修改后重新审核")
		}
		beats, err := scriptBeats(p)
		if err != nil {
			return p, err
		}
		if len(beats) > 0 {
			found := false
			for _, beat := range beats {
				found = found || (start >= beat.Start && start+q.Duration <= beat.End && q.SourceText == beat.Text)
			}
			if !found {
				return p, errors.New("当前镜头的时间或原文依据偏离总体剧本，请修改后重新审核")
			}
		}
		if len(q.References) == 0 {
			return p, errors.New("当前镜头须引用已确认素材")
		}
		seen := map[string]bool{}
		for _, ref := range q.References {
			found := false
			for _, a := range p.Assets {
				found = found || a.ID == ref
			}
			if !found || seen[ref] {
				return p, errors.New("当前镜头的引用素材无效或重复")
			}
			seen[ref] = true
		}
		if err := validateSpeech(p, q); err != nil {
			return p, err
		}
		if dialogueMode(p) {
			lines := scriptDialogue(p, fidelityNarration(q))
			if len(lines) > 0 && !strings.Contains("\n"+strings.Join(scriptDialogue(p, q.SourceText), "\n")+"\n", "\n"+strings.Join(lines, "\n")+"\n") {
				return p, errors.New("本镜对白须逐字保留原剧本台词及顺序，请修改后重新审核")
			}
		}
		out, err := decodeBody[Project](jsonBytes(p))
		if err != nil {
			return p, err
		}
		out.Shots, out.PlannedShots = out.PlannedShots, nil
		out.BoardsLocked, out.PlanApproved, out.PlanVersion = false, false, 2
		out.Output, out.Voice = "", ""
		return out, nil
	}
	return p, errors.New("待审核规划中没有当前镜头，请刷新后重新报价")
}

// Draft saves never trust client-supplied image/video/approval fields.
func planShotText(q Shot) Shot {
	return Shot{ID: q.ID, Title: q.Title, FirstFrame: q.FirstFrame, Description: q.Description, SourceText: q.SourceText, Narration: q.Narration, Duration: q.Duration, References: q.References}
}

func preserveTrialResults(old, next Shot) Shot {
	if reflect.DeepEqual(planShotText(old), planShotText(next)) && next.ReviewStatus == "reviewed" {
		old.ReviewStatus = next.ReviewStatus
		return old
	}
	return next
}
