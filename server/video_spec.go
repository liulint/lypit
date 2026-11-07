package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const defaultDuration = 60
const defaultAspect = "9:16"

// Missing fields in existing projects, history and task snapshots retain the original spec.
func (p *Project) UnmarshalJSON(b []byte) error {
	type plain Project
	v := plain{AspectRatio: defaultAspect, Duration: defaultDuration}
	if e := json.Unmarshal(b, &v); e != nil {
		return e
	}
	*p = Project(v)
	return nil
}
func projectAspect(p Project) string {
	if p.AspectRatio == "" {
		return defaultAspect
	}
	return p.AspectRatio
}
func projectDuration(p Project) int {
	if p.Duration == 0 {
		return defaultDuration
	}
	return p.Duration
}
func validateSpec(aspect string, duration int) error {
	if aspect != "9:16" && aspect != "16:9" {
		return errors.New("画幅请选择9:16竖屏或16:9横屏")
	}
	if duration < 10 || duration > 180 {
		return errors.New("时长须为10～180之间的整数秒")
	}
	return nil
}
func aspectLabel(p Project) string {
	if projectAspect(p) == "16:9" {
		return "16:9 横屏"
	}
	return "9:16 竖屏"
}

// Identity masters stay portrait; composition-dependent scenes and boards follow the project.
func landscapeImage(p Project, key string) bool {
	return projectAspect(p) == "16:9" && (strings.HasPrefix(key, "shot-") || strings.HasPrefix(key, "asset-"))
}
func frameSize(p Project) (int, int) {
	if projectAspect(p) == "16:9" {
		return 640, 360
	}
	return 360, 640
}
func scaleFilter(p Project) string {
	w, h := frameSize(p)
	return fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=increase,crop=%d:%d,setsar=1", w, h, w, h)
}

// Balance integer-second clips between 5 and 8 seconds, preserving the 60s 8/7s plan.
func shotDurations(p Project) []int {
	duration := projectDuration(p)
	count := (duration + 7) / 8
	out := make([]int, count)
	last := 0
	for i := range out {
		end := ((i+1)*duration + count/2) / count
		out[i] = end - last
		last = end
	}
	return out
}
func shotCount(p Project) int { return len(shotDurations(p)) }
func validateShotPlan(p Project) error {
	if e := validateSpec(projectAspect(p), projectDuration(p)); e != nil {
		return e
	}
	if p.PlanVersion == 2 {
		if len(p.Shots) == 0 {
			return errors.New("分镜规划为空")
		}
		total := 0
		for _, q := range p.Shots {
			if q.Duration < 1 || q.Duration > 12 {
				return errors.New("单镜时长须为1～12秒")
			}
			total += q.Duration
		}
		if total != projectDuration(p) {
			return errors.New("镜头总时长与目标时长不符")
		}
		return nil
	}
	if len(p.Shots) != shotCount(p) {
		return errors.New("镜头数量与当前制作规格不符，请重新制作分镜")
	}
	total := 0
	for _, q := range p.Shots {
		if q.Duration < 5 || q.Duration > 8 {
			return errors.New("镜头时长不符合当前制作规格")
		}
		total += q.Duration
	}
	if total != projectDuration(p) {
		return errors.New("镜头总时长与目标时长不符")
	}
	return nil
}

// Absent mode is the historical narrator pipeline, including immutable task snapshots.
func dialogueMode(p Project) bool { return p.AudioMode == "dialogue" }
func audioMode(p Project) string {
	if dialogueMode(p) {
		return "dialogue"
	}
	return "narration"
}
func validateAudioMode(mode string) error {
	if mode != "dialogue" && mode != "narration" {
		return errors.New("声音方式请选择角色对白或旁白")
	}
	return nil
}
func audioDirection(p Project) string {
	if dialogueMode(p) {
		return "声音方式：角色对白。让角色（包括动物）在画面内直接说中文，以角色名称：台词的格式逐行标明说话者；禁止旁白、解说和画外音。台词简短，留出动作时间，不要求每镜说话，同一角色音色保持一致。"
	}
	return "声音方式：统一中文旁白。不安排角色开口说话，旁白以第三人称叙事，简短适合镜头时长。"
}
func videoPrompt(p Project, q Shot) string {
	prompt := p.Style + "。" + q.Description
	if p.PlanVersion == 2 {
		header := p.Script
		if loc := beatHeader.FindStringIndex(header); loc != nil {
			header = header[:loc[0]]
		} else {
			header = ""
		}
		for _, a := range p.Assets {
			if a.Kind == "character" {
				for _, id := range q.References {
					if id == a.ID {
						header += "。" + a.Name + "：" + a.Description
					}
				}
			}
		}
		prompt = "总体风格与声音约束：" + header + "。静态首帧：" + q.FirstFrame + "。从该首帧开始演出，仅发生本镜动作：" + prompt
	}
	if dialogueMode(p) {
		return prompt + "\n" + audioDirection(p) + "按照以下已确认台词逐句发声，动物保持原有形象但能说话，嘴部动作与台词同步。仅说标明的台词，不朗读角色名。无台词时只有环境声。\n本镜台词：" + q.Narration
	}
	return prompt + "。无角色对白，无旁白，配音由后期统一加入。"
}

// Keep the existing narration JSON field compatible with old projects and receipts.
// In dialogue mode it contains one explicit character-name: line per utterance.
func validateSpeech(p Project, q Shot) error {
	if !dialogueMode(p) {
		return nil
	}
	names := map[string]bool{}
	for _, a := range p.Assets {
		if a.Kind == "character" {
			for _, id := range q.References {
				if id == a.ID {
					names[a.Name] = true
				}
			}
		}
	}
	for _, line := range strings.Split(q.Narration, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		speaker, text, ok := strings.Cut(strings.ReplaceAll(line, ":", "："), "：")
		if !ok || !names[strings.TrimSpace(speaker)] || strings.TrimSpace(text) == "" {
			return errors.New("角色对白须逐行填写“角色名：台词”，说话者须是本镜引用的角色；无人说话可留空")
		}
	}
	return nil
}
