package main

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

type scriptBeat struct {
	Start, End int
	Text       string
}

var beatHeader = regexp.MustCompile(`(?m)^【\s*(\d+)\s*[—–－-]\s*(\d+)\s*秒[^\n]*】`)

// Timed script sections are authorial boundaries, never a clip-count heuristic.
func scriptBeats(p Project) ([]scriptBeat, error) {
	matches := beatHeader.FindAllStringSubmatchIndex(p.Script, -1)
	beats := []scriptBeat{}
	end := 0
	for i, m := range matches {
		start, _ := strconv.Atoi(p.Script[m[2]:m[3]])
		stop, _ := strconv.Atoi(p.Script[m[4]:m[5]])
		if start != end || stop <= start {
			return nil, errors.New("剧本时间段须从0秒连续递增，请先修正剧本")
		}
		next := len(p.Script)
		if i+1 < len(matches) {
			next = matches[i+1][0]
		}
		beats = append(beats, scriptBeat{start, stop, strings.TrimSpace(p.Script[m[0]:next])})
		end = stop
	}
	if len(beats) > 0 && end != projectDuration(p) {
		return nil, errors.New("剧本时间段与项目总时长不一致，请先修正剧本")
	}
	return beats, nil
}

func scriptDialogue(p Project, text string) []string {
	lines := []string{}
	for _, line := range strings.Split(text, "\n") {
		speaker, words, ok := strings.Cut(strings.ReplaceAll(strings.TrimSpace(line), ":", "："), "：")
		if !ok {
			continue
		}
		for _, a := range p.Assets {
			if a.Kind == "character" && a.Name == strings.TrimSpace(speaker) {
				lines = append(lines, a.Name+"："+strings.TrimSpace(words))
				break
			}
		}
	}
	return lines
}

func validateFaithfulPlan(p Project, shots []Shot) error {
	beats, err := scriptBeats(p)
	if err != nil {
		return err
	}
	if len(shots) == 0 || len(shots) > 180 {
		return errors.New("分镜规划为空或镜头过多")
	}
	allowed := map[string]bool{}
	for _, a := range p.Assets {
		allowed[a.ID] = true
	}
	total := 0
	dialogue := []string{}
	sourcePosition := 0
	for _, q := range shots {
		if q.Duration < 1 || q.Duration > 12 {
			return errors.New("单镜须为1～12秒；超长段落请在原剧情内拆镜，不得压缩或挪动剧情")
		}
		if strings.TrimSpace(q.Title) == "" || strings.TrimSpace(q.Description) == "" || strings.TrimSpace(q.FirstFrame) == "" || strings.TrimSpace(q.SourceText) == "" {
			return errors.New("分镜须包含原文依据、独立首帧和视频动作")
		}
		if !strings.Contains(p.Script, q.SourceText) {
			return errors.New("分镜原文依据必须逐字引用总体剧本")
		}
		if len(beats) == 0 {
			at := strings.Index(p.Script[sourcePosition:], q.SourceText)
			if at < 0 {
				return errors.New("分镜原文依据顺序与总体剧本不一致")
			}
			sourcePosition += at
		}
		if len(beats) > 0 {
			matched := false
			for _, b := range beats {
				if total >= b.Start && total+q.Duration <= b.End && q.SourceText == b.Text {
					matched = true
					break
				}
			}
			if !matched {
				return errors.New("分镜跨越或偏离剧本时间段；不得合并不同场景或挪动剧情")
			}
		}
		if len(q.References) == 0 {
			return errors.New("每镜须引用已确认素材")
		}
		seen := map[string]bool{}
		for _, id := range q.References {
			if !allowed[id] || seen[id] {
				return errors.New("分镜素材引用无效或重复")
			}
			seen[id] = true
		}
		if q.ContentOverride != nil && !contentOverridden(q) {
			return errors.New("已确认的单镜覆盖内容已变化，请通过内容编辑重新确认")
		}
		if e := validateSpeech(p, q); e != nil {
			return e
		}
		if dialogueMode(p) {
			dialogue = append(dialogue, scriptDialogue(p, fidelityNarration(q))...)
		}
		total += q.Duration
	}
	if total != projectDuration(p) {
		return errors.New("分镜总时长必须与总体剧本一致，不自动均分或修正")
	}
	if dialogueMode(p) && strings.Join(dialogue, "\n") != strings.Join(scriptDialogue(p, p.Script), "\n") {
		return errors.New("分镜台词与总体剧本不一致：不得删改、遗漏、重复或调换顺序")
	}
	// A line must remain in its original timed scene, even if the global order matches.
	if dialogueMode(p) && len(beats) > 0 {
		start := 0
		for _, b := range beats {
			lines := []string{}
			for _, q := range shots {
				if start >= b.Start && start < b.End {
					lines = append(lines, scriptDialogue(p, fidelityNarration(q))...)
				}
				start += q.Duration
			}
			if strings.Join(lines, "\n") != strings.Join(scriptDialogue(p, b.Text), "\n") {
				return errors.New("台词必须保留在原剧本时间段内")
			}
			start = 0
		}
	}
	return nil
}

func boardFrame(q Shot) string {
	if q.FirstFrame != "" {
		return "只画本镜开始时的一帧静态画面，不提前表现后续动作，不合并时间或场景，不画拼图。首帧：" + q.FirstFrame
	}
	return q.Description // Preserve already-confirmed historical inputs.
}

func (s *Studio) planFaithfulShots(ctx context.Context, t *Task) error {
	p := &t.Input
	policy := shotDurationPolicy{Min: 1, Max: 12}
	if t.VideoDurationPolicyVersion > 0 {
		policy = videoDurationPolicy(t.Config)
		if err := validatePlanningDurations(*p, t.Config); err != nil {
			return err
		}
	}
	beats, err := scriptBeats(*p)
	if err != nil {
		return err
	}
	var v struct {
		Shots []Shot `json:"shots"`
	}
	if t.Mode == "mock" {
		if len(beats) == 0 {
			// Explicit local schematic only; no attempt to invent a new screenplay.
			last := 0
			for _, d := range shotDurations(*p) {
				beats = append(beats, scriptBeat{last, last + d, p.Script})
				last += d
			}
		}
		refs := []string{}
		for _, a := range p.Assets {
			refs = append(refs, a.ID)
		}
		for i, b := range beats {
			if b.End-b.Start > 12 {
				return errors.New("模拟规划遇到超过12秒的段落，请手动拆分剧本时间段")
			}
			speech := ""
			if dialogueMode(*p) {
				speech = strings.Join(scriptDialogue(*p, b.Text), "\n")
			}
			if len(beatHeader.FindAllStringIndex(p.Script, -1)) == 0 && i > 0 {
				speech = ""
			}
			v.Shots = append(v.Shots, Shot{Title: fmt.Sprintf("模拟分镜 %d（%d—%d秒）", i+1, b.Start, b.End), SourceText: b.Text, FirstFrame: "【本地程序示意】本段动作尚未开始时的构图，需人工核对。", Description: "【本地模拟规划】按原文顺序演出本段动作：" + b.Text, Narration: speech, Duration: b.End - b.Start, References: refs})
		}
	} else {
		if t.StoryDirectionVersion >= storyDirectionVersion {
			policy.Min = max(2, policy.Min)
		}
		prompt := fmt.Sprintf(`你是执行分镜导演。总体剧本是唯一剧情依据，忠实拆解，不重新编剧。总时长%d秒，画幅%s。不得固定镜头数或均分时长。有明确时间段时，严格保留各段边界、场景、事件顺序和台词原文；只允许在同一段内按动作拆成更多镜头，不可跨段合并。无时间段时按剧情顺序自然拆镜。单镜%d～%d秒，不得拆出短于下限的镜头；无法满足时应报告冲突，禁止自动重试。超长段落在原段内部拆镜，不删减事件。不得新增、遗漏或挪动剧情和角色。每镜source_text逐字复制所属完整时间段（含标题），无时间段则逐字引用对应原文。first_frame仅描述动作开始之前同一时空的一幅静态构图：人物位置、姿态、道具、光线、景别；禁止提前画动作结尾或将多个场景揉成一张图。description只写从该首帧开始按顺序演出的动作、运镜和声音。narration逐字保留本镜对白，不改写，不朗读角色名，无台词留空。仅用素材清单ID引用，角色音色及固定特征遵从总体剧本。返回JSON {"shots":[{"title":"镜头名","source_text":"原文依据","duration":5,"first_frame":"静态首帧","description":"视频动作","narration":"角色名：原台词","references":["资产ID"]}]}。这是文字草稿，完成后等待人工审核，不生成图片。
总体剧本：%s
素材清单：%s
%s`, projectDuration(*p), aspectLabel(*p), policy.Min, policy.Max, p.Script, string(jsonBytes(p.Assets)), audioDirection(*p))
		if t.StoryDirectionVersion >= storyDirectionVersion {
			prompt += directingRules + "\nshots中每镜在原字段之外增加：" + executionSchema
			prompt, err = t.renderPrompt("board_plan", prompt, map[string]string{
				"duration": fmt.Sprint(projectDuration(*p)), "aspect": aspectLabel(*p), "min_duration": fmt.Sprint(policy.Min), "max_duration": fmt.Sprint(policy.Max),
				"script": p.Script, "assets": string(jsonBytes(p.Assets)), "audio_direction": audioDirection(*p), "output_contract": promptOutputContract("board_plan"),
			})
			if err != nil {
				return err
			}
			var result struct {
				Conflict string         `json:"conflict"`
				Shots    []directedShot `json:"shots"`
			}
			if err := s.textJSON(ctx, t, "boards-plan", prompt, &result); err != nil {
				return err
			}
			if result.Conflict != "" {
				return fmt.Errorf("分镜规划冲突：%s；原响应已保存，不自动重试", result.Conflict)
			}
			for i, item := range result.Shots {
				q, err := compileShotExecution(*p, item.Shot, item.Execution)
				if err != nil {
					return fmt.Errorf("第%d镜节奏规划无效：%w；请调整剧本或时长后重新报价，不自动重试", i+1, err)
				}
				v.Shots = append(v.Shots, q)
			}
		} else {
			if err := s.textJSON(ctx, t, "boards-plan", prompt, &v); err != nil {
				return err
			}
		}
	}
	if t.VideoDurationPolicyVersion > 0 {
		if err := validatePlanVideoDurations(t.Config, v.Shots); err != nil {
			return err
		}
	}
	if err := validateFaithfulPlan(*p, v.Shots); err != nil {
		return err
	}
	for i := range v.Shots {
		q := &v.Shots[i]
		*q = Shot{ID: uid(), Title: q.Title, SourceText: q.SourceText, FirstFrame: q.FirstFrame, Description: q.Description, Narration: q.Narration, Duration: q.Duration, References: q.References, Candidates: []Image{}}
	}
	for _, old := range p.PlannedShots {
		if len(old.Candidates) > 0 || old.Video != "" {
			p.PreviousShots = append(p.PreviousShots, old)
		}
	}
	p.PlannedShots = v.Shots
	return s.progress(t, 95, "文字分镜已完成，等待人工审核；尚未生图")
}

func (s *Studio) reviewBoardPlan(c *gin.Context) {
	var v struct {
		Revision int    `json:"revision"`
		Shots    []Shot `json:"shots"`
		Approve  bool   `json:"approve"`
	}
	if !bind(c, &v) {
		return
	}
	p, err := s.store.update(c.Param("id"), v.Revision, func(p *Project) error {
		if p.Archived {
			return errors.New("已归档项目只读")
		}
		if len(p.PlannedShots) == 0 {
			return errors.New("没有待审核的分镜规划")
		}
		if len(v.Shots) != len(p.PlannedShots) {
			return errors.New("分镜数量已变化，请刷新后重试")
		}
		// Never accept override authorization from a client draft.
		for i := range v.Shots {
			q := &v.Shots[i]
			q.ContentOverride = nil
			old := p.PlannedShots[i]
			if q.ID == old.ID && contentOverridden(old) && contentHash(*q) == contentHash(old) {
				q.ContentOverride = old.ContentOverride
			}
		}
		for i, q := range v.Shots {
			if q.ID != p.PlannedShots[i].ID {
				return errors.New("分镜顺序或身份已变化，请刷新后重试")
			}
			if q.ReviewStatus != "" && q.ReviewStatus != "pending" && q.ReviewStatus != "changes" && q.ReviewStatus != "reviewed" {
				return errors.New("未知逐镜审核状态")
			}
			if len(q.Title) > 1000 || len(q.FirstFrame) > 50000 || len(q.Description) > 50000 || len(q.Narration) > 3000 || len(q.SourceText) > 50000 || len(q.References) > len(p.Assets) {
				return errors.New("草稿内容过长或引用过多")
			}
			if v.Approve && q.ReviewStatus != "reviewed" {
				return fmt.Errorf("第%d镜尚未审核完成，请逐镜确认", i+1)
			}
		}
		if v.Approve {
			cfg, err := s.store.settings()
			if err != nil {
				return err
			}
			if err := validatePlanVideoDurations(cfg, v.Shots); err != nil {
				return err
			}
			if err := validateFaithfulPlan(*p, v.Shots); err != nil {
				return err
			}
		}
		oldOffset, newOffset := 0, 0
		for i := range v.Shots {
			q := &v.Shots[i]
			old := p.PlannedShots[i]
			*q = Shot{ContentOverride: q.ContentOverride, ID: q.ID, ReviewStatus: q.ReviewStatus, Title: q.Title, SourceText: q.SourceText, FirstFrame: q.FirstFrame, Description: q.Description, Narration: q.Narration, Duration: q.Duration, References: q.References, Candidates: []Image{}}
			if oldOffset == newOffset {
				*q = preserveTrialResults(old, *q)
			}
			if (len(old.Candidates) > 0 || old.Video != "") && len(q.Candidates) == 0 {
				p.PreviousShots = append(p.PreviousShots, old)
			}
			oldOffset += old.Duration
			newOffset += q.Duration
		}
		p.PlannedShots = v.Shots
		if v.Approve {
			p.PreviousShots = append(p.PreviousShots, p.Shots...)
			p.Shots = p.PlannedShots
			p.PlannedShots = nil
			p.PlanVersion = 2
			p.PlanApproved = true
			p.BoardsLocked = false
			p.Output = ""
			p.Voice = ""
		}
		return nil
	})
	result(c, p, err)
}
