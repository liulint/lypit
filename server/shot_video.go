package main

import (
	"context"
	"errors"
	"os"
	"strings"
)

type videoMaker func(context.Context, *Task, Shot) (string, error)

func (s *Studio) videoShot(p Project, id, feedback string, trial ...bool) (Shot, error) {
	if !p.AssetsLocked {
		return Shot{}, errors.New("请先锁定参考素材")
	}
	if e := s.assetsReady(p); e != nil {
		return Shot{}, e
	}
	if e := validateShotPlan(p); e != nil && !(len(trial) > 0 && trial[0]) {
		return Shot{}, e
	}
	if len([]rune(feedback)) > 1000 {
		return Shot{}, errors.New("修改建议最多1000字")
	}
	for _, q := range p.Shots {
		if q.ID != id {
			continue
		}
		if !q.Approved {
			return Shot{}, errors.New("请先确认当前镜头画面与台词，再生成本镜视频")
		}
		if q.Selected < 0 || q.Selected >= len(q.Candidates) {
			return Shot{}, errors.New("当前镜头图片不存在")
		}
		if e := s.verifyImage(q.Candidates[q.Selected]); e != nil {
			return Shot{}, e
		}
		if e := validateSpeech(p, q); e != nil {
			return Shot{}, e
		}
		if q.Video != "" && strings.TrimSpace(feedback) == "" {
			return Shot{}, errors.New("重新生成视频前请填写修改建议")
		}
		return q, nil
	}
	return Shot{}, errors.New("目标镜头不存在")
}

func (s *Studio) generateShotVideo(ctx context.Context, t *Task, makeVideo videoMaker) error {
	p := &t.Input
	expected := t.ProjectID + "/" + t.ID + "-clip-" + t.ShotID + ".mp4"
	for _, q := range p.Shots {
		if q.ID == t.ShotID && q.Video == expected {
			info, e := os.Stat(s.media(q.Video))
			if e != nil {
				return e
			}
			if !info.Mode().IsRegular() || info.Size() == 0 {
				return errors.New("本镜视频文件无效")
			}
			return nil
		}
	}
	q, e := s.videoShot(*p, t.ShotID, t.Feedback, t.PlanTrial)
	if e != nil {
		return e
	}
	if e = s.progress(t, 5, "制作本镜视频："+q.Title); e != nil {
		return e
	}
	file, e := makeVideo(ctx, t, q)
	if e != nil {
		return e
	} // A failed or unknown submission must not replace the previous clip.
	if file == "" {
		return errors.New("供应商未返回本镜视频")
	}
	for i := range p.Shots {
		if p.Shots[i].ID != q.ID {
			continue
		}
		p.Shots[i].Video = file
		recordVideoInput(t, i)
		p.Output = ""
		return s.progress(t, 95, "本镜视频已完成，请预览；不满意可填写建议重新生成")
	}
	return errors.New("目标镜头不存在")
}
