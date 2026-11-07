package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/gin-gonic/gin"
)

// Only server-confirmed changes can relax fidelity. Hashes bind the exception to
// exactly the accepted text; the original dialogue remains the fidelity baseline.
type ShotContentOverride struct {
	Version           int    `json:"version"`
	OriginalNarration string `json:"original_narration"`
	OriginalSource    string `json:"original_source"`
	Hash              string `json:"hash"`
}
type ShotContent struct {
	FirstFrame  string `json:"first_frame"`
	Description string `json:"description"`
	Narration   string `json:"narration"`
}
type ShotRewriteResult struct {
	Revision        int         `json:"revision"`
	SettingsVersion int         `json:"settings_version"`
	Before          ShotContent `json:"before"`
	After           ShotContent `json:"after"`
	OldDuration     int         `json:"old_duration"`
	Duration        int         `json:"duration"`
	AppliedRevision int         `json:"applied_revision,omitempty"`
}

func shotContent(q Shot) ShotContent { return ShotContent{q.FirstFrame, q.Description, q.Narration} }
func contentHash(q Shot) string      { return digest(jsonBytes(shotContent(q))) }
func contentOverridden(q Shot) bool {
	return q.ContentOverride != nil && q.ContentOverride.Version == 1 && q.ContentOverride.Hash == contentHash(q)
}
func fidelityNarration(q Shot) string {
	if contentOverridden(q) {
		return q.ContentOverride.OriginalNarration
	}
	return q.Narration
}
func rewriteShot(p Project, id string, trial bool) (Shot, error) {
	shots := p.Shots
	if trial {
		shots = p.PlannedShots
	}
	for _, q := range shots {
		if q.ID == id {
			return q, nil
		}
	}
	return Shot{}, errors.New("镜头不存在")
}
func validateShotContent(p Project, q Shot) error {
	if strings.TrimSpace(q.FirstFrame) == "" || strings.TrimSpace(q.Description) == "" || len(q.FirstFrame) > 50000 || len(q.Description) > 50000 || len(q.Narration) > 3000 {
		return errors.New("请补全首帧与动作，首帧和动作各最多50000字节，台词最多3000字节")
	}
	if err := validateSpeech(p, q); err != nil {
		return err
	}
	text := q.Narration
	if dialogueMode(p) {
		lines := []string{}
		for _, line := range strings.Split(text, "\n") {
			_, v, ok := strings.Cut(strings.ReplaceAll(line, ":", "："), "：")
			if ok {
				lines = append(lines, v)
			}
		}
		text = strings.Join(lines, "")
	}
	n := 0
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			n++
		}
	}
	if n > q.Duration*6 {
		return fmt.Errorf("台词过长：%d秒最多建议%d个字母或汉字，请精简或延长时长", q.Duration, q.Duration*6)
	}
	return nil
}
func changeShotContent(p Project, id string, trial bool, duration int, v ShotContent, cfg Settings) (Project, error) {
	old, err := rewriteShot(p, id, trial)
	if err != nil {
		return p, err
	}
	// Retime validates the original faithful plan and shifts only time markers.
	next, err := retimeShot(p, id, duration, trial, cfg)
	if err != nil {
		return p, err
	}
	next, err = decodeBody[Project](jsonBytes(next))
	if err != nil {
		return p, err
	}
	shots := next.Shots
	if trial {
		shots = next.PlannedShots
	}
	for i := range shots {
		if shots[i].ID != id {
			continue
		}
		q := &shots[i]
		q.FirstFrame, q.Description, q.Narration = v.FirstFrame, v.Description, v.Narration
		if err := validateShotContent(next, *q); err != nil {
			return p, err
		}
		if old.Duration == duration && shotContent(old) == v {
			return p, nil
		}
		original := old.Narration
		source := old.SourceText
		if contentOverridden(old) {
			original = old.ContentOverride.OriginalNarration
			source = old.ContentOverride.OriginalSource
		}
		q.ContentOverride = &ShotContentOverride{Version: 1, OriginalNarration: original, OriginalSource: source, Hash: contentHash(*q)}
		if old.Duration == duration && old.Video != "" {
			next.PreviousShots = append(next.PreviousShots, old)
		}
		q.Approved = false
		q.ReviewStatus = "pending"
		q.Video = ""
		q.VideoMode = ""
		q.VideoTaskID = ""
		q.VideoSourceFile = ""
		q.VideoEndShotID = ""
		q.VideoEndImageHash = ""
	}
	if trial {
		next.PlannedShots = shots
	} else {
		next.Shots = shots
	}
	next.BoardsLocked = false
	next.Voice = ""
	next.Output = ""
	if err := validateFaithfulPlan(next, shots); err != nil {
		return p, err
	}
	return next, nil
}
func (s *Studio) rewriteShotTask(ctx context.Context, t *Task) error {
	if t.Rewrite != nil {
		return nil
	}
	q, err := rewriteShot(t.Input, t.ShotID, t.PlanTrial)
	if err != nil {
		return err
	}
	out := ShotContent{}
	if t.Mode == "mock" {
		out = ShotContent{FirstFrame: "【本地模拟改写】保留人物与场景，动作开始前的静态构图。", Description: fmt.Sprintf("【本地模拟改写】在%d秒内完成一次明确动作，保持前后镜衔接；此为程序模板，需人工核对。", t.TargetDuration), Narration: ""}
	} else {
		shots := t.Input.Shots
		if t.PlanTrial {
			shots = t.Input.PlannedShots
		}
		contextShots := []Shot{}
		for i := range shots {
			if shots[i].ID == q.ID {
				if i > 0 {
					contextShots = append(contextShots, planShotText(shots[i-1]))
				}
				contextShots = append(contextShots, planShotText(q))
				if i+1 < len(shots) {
					contextShots = append(contextShots, planShotText(shots[i+1]))
				}
				break
			}
		}
		prompt := fmt.Sprintf("你是短片导演。只重写当前镜头，输出严格适合%d秒的内容。允许完全重写首帧、动作和台词，保持既定人物身份、场景道具和前后剧情衔接，不新增引用素材，不修改其他镜头。动作应少而明确，台词需自然说完（每秒不超过6个汉字或字母）。首帧是静态画面，动作另写。返回JSON {\"first_frame\":\"静态首帧\",\"description\":\"视频动作和运镜\",\"narration\":\"台词或旁白，可为空\"}。当前镜头ID：%s；前后镜头：%s；总体剧本：%s；素材：%s；%s", t.TargetDuration, q.ID, string(jsonBytes(contextShots)), t.Input.Script, string(jsonBytes(t.Input.Assets)), audioDirection(t.Input))
		if t.StoryDirectionVersion >= storyDirectionVersion {
			prompt += directingRules + "\n返回原first_frame、description、narration字段，并增加：" + executionSchema
			prompt, err = t.renderPrompt("shot_rewrite", prompt, map[string]string{
				"target_duration": fmt.Sprint(t.TargetDuration), "shot_id": q.ID, "context_shots": string(jsonBytes(contextShots)),
				"script": t.Input.Script, "assets": string(jsonBytes(t.Input.Assets)), "audio_direction": audioDirection(t.Input), "output_contract": promptOutputContract("shot_rewrite"),
			})
			if err != nil {
				return err
			}
			var result struct {
				ShotContent
				Execution shotExecution `json:"execution"`
				Conflict  string        `json:"conflict"`
			}
			if err := s.textJSON(ctx, t, "shot-rewrite", prompt, &result); err != nil {
				return err
			}
			if result.Conflict != "" {
				return fmt.Errorf("单镜改写冲突：%s；原响应已保存，不自动重试", result.Conflict)
			}
			candidate := q
			candidate.Duration = t.TargetDuration
			candidate.FirstFrame, candidate.Description, candidate.Narration = result.FirstFrame, result.Description, result.Narration
			candidate, err = compileShotExecution(t.Input, candidate, result.Execution)
			if err != nil {
				return err
			}
			out = shotContent(candidate)
		} else {
			if err := s.textJSON(ctx, t, "shot-rewrite", prompt, &out); err != nil {
				return err
			}
		}
	}
	candidate := q
	candidate.Duration = t.TargetDuration
	candidate.FirstFrame = out.FirstFrame
	candidate.Description = out.Description
	candidate.Narration = out.Narration
	if err := validateShotContent(t.Input, candidate); err != nil {
		return err
	}
	t.Rewrite = &ShotRewriteResult{Revision: t.Input.Revision, SettingsVersion: t.Config.Version, Before: shotContent(q), After: out, OldDuration: q.Duration, Duration: t.TargetDuration}
	return s.progress(t, 95, "改写建议已保存，等待对比确认")
}
func (s *Studio) getShotRewrite(c *gin.Context) {
	t, err := s.store.task(c.Param("id"))
	if err != nil {
		result(c, nil, err)
		return
	}
	if t.Stage != "shot_rewrite" || t.Status != "completed" || t.Rewrite == nil {
		result(c, nil, errors.New("改写建议尚未完成"))
		return
	}
	result(c, t.Rewrite, nil)
}

type shotContentEdit struct {
	ShotContent
	Revision        int  `json:"revision"`
	PlanTrial       bool `json:"plan_trial"`
	Duration        int  `json:"duration"`
	SettingsVersion int  `json:"settings_version"`
	FinishRejected  bool `json:"finish_rejected"`
}

func (s *Studio) editShotContent(c *gin.Context) {
	var v shotContentEdit
	if !bind(c, &v) {
		return
	}
	s.saveShotContent(c, c.Param("id"), c.Param("shot"), v, nil)
}
func (s *Studio) applyShotRewrite(c *gin.Context) {
	var v shotContentEdit
	if !bind(c, &v) {
		return
	}
	t, err := s.store.task(c.Param("id"))
	if err != nil {
		result(c, nil, err)
		return
	}
	if t.Stage != "shot_rewrite" || t.Status != "completed" || t.Rewrite == nil || t.Rewrite.AppliedRevision != 0 {
		result(c, nil, errConflict)
		return
	}
	// The submitted fields may be manually refined, but the target timing and scope
	// must still be the scope that was quoted and generated.
	v.PlanTrial = t.PlanTrial
	v.Duration = t.TargetDuration
	s.saveShotContent(c, t.ProjectID, t.ShotID, v, &t)
}
func (s *Studio) saveShotContent(c *gin.Context, pid, id string, v shotContentEdit, task *Task) {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	p, err := s.store.project(pid)
	if err != nil {
		result(c, nil, err)
		return
	}
	if p.Revision != v.Revision {
		result(c, nil, errConflict)
		return
	}
	cfg, err := s.store.settings()
	if err != nil {
		result(c, nil, err)
		return
	}
	if cfg.Version != v.SettingsVersion {
		result(c, nil, errors.New("模型配置已变化，请重新核对"))
		return
	}
	if task != nil {
		fresh, e := s.store.task(task.ID)
		if e != nil {
			result(c, nil, e)
			return
		}
		task = &fresh
		if task.Rewrite == nil || task.Rewrite.AppliedRevision != 0 || task.Input.Revision != p.Revision || task.Config.Version != cfg.Version {
			result(c, nil, errConflict)
			return
		}
	}
	tasks, err := s.store.tasks(pid)
	if err != nil {
		result(c, nil, err)
		return
	}
	finish := []Task{}
	for _, t := range tasks {
		if taskUnresolved(t.Status) {
			if !v.FinishRejected || !s.store.durationRejectedTask(t, id, v.PlanTrial) {
				result(c, nil, errors.New("仍有执行中或结果待核对的任务，请先恢复或处理任务"))
				return
			}
			finish = append(finish, t)
		}
	}
	next, err := changeShotContent(p, id, v.PlanTrial, v.Duration, v.ShotContent, cfg)
	if err != nil {
		result(c, nil, err)
		return
	}
	next.Revision = p.Revision + 1
	next.Updated = now()
	err = s.store.transaction(func(tx *sql.Tx) error {
		for _, t := range finish {
			t.Status = "abandoned"
			t.Message = "确认修改后结束明确拒绝的任务，原回执保留"
			t.Updated = now()
			if _, e := tx.Exec("UPDATE tasks SET status=?,body=? WHERE id=?", t.Status, jsonBytes(t), t.ID); e != nil {
				return e
			}
		}
		if task != nil {
			task.Rewrite.AppliedRevision = next.Revision
			if _, e := tx.Exec("UPDATE tasks SET body=? WHERE id=?", jsonBytes(task), task.ID); e != nil {
				return e
			}
		}
		return s.store.saveProject(tx, next)
	})
	result(c, next, err)
}
