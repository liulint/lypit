package main

import (
	"context"
	"errors"
	"strings"
)

type ShotRequest struct {
	TargetDuration int
	PlanTrial      bool
	Preserve       string
	ReferenceIDs   []string
	ShotID         string
	Feedback       string
	VideoMode      string
	EndShotID      string
}

// The target and advice are frozen in the quote and task, alongside the project version.
func (s *Studio) retryShot(p Project, id, feedback string) (Shot, error) {
	if !p.AssetsLocked || p.BoardsLocked {
		return Shot{}, errors.New("请在素材锁定、分镜未锁定时重新生成图片")
	}
	if e := s.assetsReady(p); e != nil {
		return Shot{}, e
	}
	if len([]rune(feedback)) > 1000 {
		return Shot{}, errors.New("请填写修改建议，最多1000字")
	}
	for _, q := range p.Shots {
		if q.ID != id {
			continue
		}
		if shotHasImage(q) {
			if strings.TrimSpace(feedback) == "" {
				return Shot{}, errors.New("重新生成已有图片前请填写修改建议")
			}
			if e := s.verifyImage(q.Candidates[q.Selected]); e != nil {
				return Shot{}, e
			}
		}
		return q, nil
	}
	return Shot{}, errors.New("目标镜头不存在")
}

func (s *Studio) regenerateBoard(ctx context.Context, t *Task, makeImage imageMaker) error {
	p := &t.Input
	key := "shot-" + t.ShotID + "-retry"
	// If saving the completed input succeeded before an interruption, do not append again.
	for _, q := range p.Shots {
		if q.ID == t.ShotID && q.Selected >= 0 && q.Selected < len(q.Candidates) && q.Candidates[q.Selected].File == t.ProjectID+"/"+t.ID+"-"+key+".png" {
			return s.verifyImage(q.Candidates[q.Selected])
		}
	}
	q, err := s.retryShot(*p, t.ShotID, t.Feedback)
	if err != nil {
		return err
	}
	refs, err := s.taskBoardReferences(*t, q)
	if err != nil {
		return err
	}
	currentReference := ""
	if (t.ReferencePolicyVersion == fourViewPolicyVersion || len(refs) < 6) && shotHasImage(q) {
		refs = append(refs, q.Candidates[q.Selected])
		currentReference = "最后一份原始参考为当前画面。"
	}
	advice := strings.TrimSpace(t.Feedback)
	if advice == "" {
		advice = "按原分镜画面、动作与角色要求补生成缺失图片"
	}
	prompt := p.Style + "。" + aspectLabel(*p) + "电影分镜。保持参考角色身份、外观与场景，原分镜：" + boardFrame(q) + "。" + currentReference + "本次修改建议：" + advice + "。仅生成一张完整画面，不要拼图或方案对比，不要添加文字。"
	prompt, err = t.renderPrompt("board_retry", prompt, map[string]string{"style": p.Style, "aspect": aspectLabel(*p), "first_frame": boardFrame(q), "current_reference": currentReference, "feedback": advice})
	if err != nil {
		return err
	}
	if err = s.progress(t, 5, "按修改建议重新生成："+q.Title); err != nil {
		return err
	}
	im, err := makeImage(ctx, t, key, prompt, refs, false)
	if err != nil {
		return err
	} // Keep the old image visible on failure or unknown result.
	if im.File == "" {
		return errors.New("生图未返回图片")
	}
	for i := range p.Shots {
		if p.Shots[i].ID != q.ID {
			continue
		}
		shot := &p.Shots[i]
		if t.ReferencePolicyVersion == fourViewPolicyVersion {
			im.ReferencePlan = t.ImageReferencePlan
		}
		im.Operation = "regenerate"
		shot.Candidates = append(shot.Candidates, im) // Preserve prior files and historical selection.
		shot.Selected = len(shot.Candidates) - 1
		shot.Approved = false
		shot.Video = ""
		p.Output = ""
		return s.progress(t, 95, "新图片已生成，等待重新确认画面")
	}
	return errors.New("目标镜头不存在")
}

func firstShotRequest(v []ShotRequest) ShotRequest {
	if len(v) > 0 {
		return v[0]
	}
	return ShotRequest{}
}
