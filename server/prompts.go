package main

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
)

func promptOutputContract(id string) string {
	switch id {
	case "analyze":
		return `{"script":"完整中文剧本","assets":[{"kind":"character|scene|prop","name":"名称","description":"固定外观或环境"}]}`
	case "board_plan":
		return `{"shots":[{"title":"镜头名","source_text":"原文依据","duration":5,"first_frame":"静态首帧","description":"视频动作","narration":"角色名：原台词","references":["资产ID"],"execution":{}}]}`
	case "shot_rewrite":
		return `{"first_frame":"静态首帧","description":"视频动作与运镜","narration":"台词或旁白，可为空","execution":{}}`
	}
	return ""
}

const promptTemplatesVersion = 3
const maxPromptBytes = 50000

type promptDefinition struct {
	ID, Name, Group, Description, Default string
}

var promptVariable = regexp.MustCompile(`\{\{([a-z_]+)\}\}`)

var promptVariableLabels = map[string]string{
	"duration": "作品总秒数", "aspect": "画幅", "min_duration": "单镜最短秒数", "max_duration": "单镜最长秒数",
	"style": "作品风格", "idea": "用户故事", "audio_direction": "当前声音方式要求", "script": "总体剧本", "assets": "可引用素材清单",
	"target_duration": "目标镜头秒数", "shot_id": "当前镜头ID", "context_shots": "当前与相邻镜头文字",
	"output_contract": "固定JSON输出结构", "directing_rules": "动作、时长与承接规则", "execution_schema": "动作时间预算输出结构",
	"asset_description": "素材描述", "asset_kind": "场景或道具类型", "image_contract": "单张完整图片约束", "sheet_contract": "2×2四视角布局约束",
	"first_frame": "已确认静态首帧", "current_reference": "当前图片参考说明", "feedback": "本次修改建议", "changes": "修改内容", "preserve": "必须保留内容",
	"edit_contract": "编辑底图与辅助素材约束", "continuity": "角色身份及剧本头部约束", "description": "已确认本镜动作", "audio_instruction": "已确认台词及音频要求",
}

const imageContract = "仅生成一张完整画面，不要拼图或方案对比，不添加文字。"
const sheetContract = "生成精确2×2四格设定图，四格等大无留白：左上正面全身、右上侧面全身、左下背面全身、右下面部近景。只输出一张完整设定图。"
const editContract = "第1张输入为唯一编辑底图，保持其镜头构图、画幅、主体布局及未要求修改的内容；其他输入仅作辅助校对，不可据此重新构图或重画整幅。只输出1张修改后的完整分镜，不输出拼图或对比图，不添加未要求的内容。"

var promptFixedVariables = map[string]string{
	"directing_rules": directingRules, "execution_schema": executionSchema,
	"image_contract": imageContract, "sheet_contract": sheetContract, "edit_contract": editContract,
}

func promptKeys(text string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, m := range promptVariable.FindAllStringSubmatch(text, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}

func validatePrompt(d promptDefinition, text string) error {
	if strings.TrimSpace(text) == "" || len(text) > maxPromptBytes {
		return fmt.Errorf("%s不能为空且最多%d字节", d.Name, maxPromptBytes)
	}
	allowed := map[string]bool{}
	for _, k := range promptKeys(d.Default) {
		allowed[k] = true
	}
	used := map[string]bool{}
	for _, k := range promptKeys(text) {
		if !allowed[k] {
			return fmt.Errorf("%s包含未知变量{{%s}}", d.Name, k)
		}
		used[k] = true
	}
	for _, k := range promptKeys(d.Default) {
		if !used[k] {
			return fmt.Errorf("%s缺少必需变量{{%s}}", d.Name, k)
		}
	}
	rest := promptVariable.ReplaceAllString(text, "")
	if strings.Contains(rest, "{{") {
		return errors.New("变量格式须为{{变量名}}，请检查未闭合或带空格的变量")
	}
	return nil
}

// Always allocate a complete map when confirming a new task. Defaults and custom
// text are frozen together in the existing private config snapshot, never looked
// up from current settings by a worker or a recovered task.
func snapshotPrompts(cfg Settings) Settings {
	text := make(map[string]string, len(promptDefinitions))
	for _, d := range promptDefinitions {
		text[d.ID] = d.Default
		if v, ok := cfg.PromptTemplates[d.ID]; ok {
			text[d.ID] = v
		}
	}
	cfg.PromptTemplates = text
	return cfg
}

func (t Task) renderPrompt(id, historical string, vars map[string]string) (string, error) {
	if t.PromptTemplatesVersion == 0 {
		return historical, nil
	}
	if t.PromptTemplatesVersion < 1 || t.PromptTemplatesVersion > promptTemplatesVersion {
		return "", errors.New("任务提示词版本不受支持，禁止改用当前配置重提")
	}
	text, ok := t.Config.PromptTemplates[id]
	if !ok {
		return "", errors.New("任务缺少提示词快照，禁止使用当前模板替代")
	}
	var def *promptDefinition
	for i := range promptDefinitions {
		if promptDefinitions[i].ID == id {
			def = &promptDefinitions[i]
			break
		}
	}
	if def == nil {
		return "", errors.New("未知提示词类型")
	}
	if err := validatePrompt(*def, text); err != nil {
		return "", err
	}
	replacements := []string{}
	for _, k := range promptKeys(text) {
		v, ok := vars[k]
		if !ok {
			v, ok = promptFixedVariables[k]
		}
		if !ok {
			return "", fmt.Errorf("提示词缺少运行变量%s", k)
		}
		replacements = append(replacements, "{{"+k+"}}", v)
	}
	// Bound expansion before allocating: repeated script variables must not turn
	// a small editable template into an unbounded provider request.
	size := len(text)
	for _, match := range promptVariable.FindAllStringSubmatch(text, -1) {
		value, ok := vars[match[1]]
		if !ok {
			value = promptFixedVariables[match[1]]
		}
		size += len(value) - len(match[0])
		if size > 2<<20 {
			return "", errors.New("填入作品内容后的提示词超过2MB，请减少重复变量或精简内容；未调用模型")
		}
	}
	// One pass only: user stories or dialogue containing {{...}} remain literal.
	return strings.NewReplacer(replacements...).Replace(text), nil
}

func promptLibrary(cfg Settings) gin.H {
	items := []gin.H{}
	for _, d := range promptDefinitions {
		text := d.Default
		if v, ok := cfg.PromptTemplates[d.ID]; ok {
			text = v
		}
		variables := []gin.H{}
		for _, k := range promptKeys(d.Default) {
			fixed := promptFixedVariables[k]
			if k == "output_contract" {
				fixed = promptOutputContract(d.ID)
			}
			variables = append(variables, gin.H{"name": k, "label": promptVariableLabels[k], "fixed": fixed})
		}
		items = append(items, gin.H{"id": d.ID, "name": d.Name, "group": d.Group, "description": d.Description, "template": text, "default_template": d.Default, "variables": variables})
	}
	return gin.H{"version": cfg.Version, "items": items, "max_bytes": maxPromptBytes}
}

func (s *Studio) getPrompts(c *gin.Context) {
	cfg, err := s.store.settings()
	if err != nil {
		result(c, nil, err)
		return
	}
	c.JSON(200, promptLibrary(cfg))
}

func (s *Studio) putPrompts(c *gin.Context) {
	var input struct {
		Version   int               `json:"version"`
		Templates map[string]string `json:"templates"`
	}
	if !bind(c, &input) {
		return
	}
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	cfg, err := s.store.settings()
	if err != nil {
		result(c, nil, err)
		return
	}
	if input.Version != cfg.Version {
		result(c, nil, errConflict)
		return
	}
	if len(input.Templates) != len(promptDefinitions) {
		result(c, nil, errors.New("须提交完整的10类提示词，不能新增或遗漏类型"))
		return
	}
	for _, d := range promptDefinitions {
		if err := validatePrompt(d, input.Templates[d.ID]); err != nil {
			result(c, nil, err)
			return
		}
	}
	// Keep only overrides; restoring defaults removes the override without deleting history.
	custom := map[string]string{}
	for _, d := range promptDefinitions {
		if input.Templates[d.ID] != d.Default {
			custom[d.ID] = input.Templates[d.ID]
		}
	}
	// Stable comparison avoids needless version changes and quote invalidation.
	if string(jsonBytes(custom)) == string(jsonBytes(cfg.PromptTemplates)) || (len(custom) == 0 && len(cfg.PromptTemplates) == 0) {
		c.JSON(200, promptLibrary(cfg))
		return
	}
	cfg.PromptTemplates, cfg.Version = custom, cfg.Version+1
	_, err = s.store.db.Exec("UPDATE settings SET body=? WHERE id=1", jsonBytes(cfg))
	if err != nil {
		result(c, nil, err)
		return
	}
	c.JSON(200, promptLibrary(cfg))
}

func (s *Studio) makeTemplateImage(ctx context.Context, t *Task, id string, vars map[string]string, key, historical string, refs []Image, sheet bool) (Image, error) {
	prompt, err := t.renderPrompt(id, historical, vars)
	if err != nil {
		return Image{}, err
	}
	return s.makeImage(ctx, t, key, prompt, refs, sheet)
}

func taskVideoPrompt(t Task, q Shot) (string, error) {
	p := t.Input
	context := p.Script
	if loc := beatHeader.FindStringIndex(context); loc != nil {
		context = context[:loc[0]]
	} else {
		context = ""
	}
	for _, a := range p.Assets {
		if a.Kind != "character" {
			continue
		}
		for _, id := range q.References {
			if id == a.ID {
				context += "。" + a.Name + "：" + a.Description
			}
		}
	}
	audio := "无角色对白，无旁白，配音由后期统一加入。"
	if dialogueMode(p) {
		audio = audioDirection(p) + "按照以下已确认台词逐句发声，嘴部动作与台词同步，仅说标明的台词，不朗读角色名。无台词时只有环境声。\n本镜台词：" + q.Narration
	}
	return t.renderPrompt("video", videoPrompt(p, q), map[string]string{"continuity": context, "first_frame": q.FirstFrame, "style": p.Style, "description": q.Description, "audio_instruction": audio})
}
