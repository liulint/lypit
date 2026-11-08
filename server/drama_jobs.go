package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"io/fs"
	"path"
	"strings"
	"time"
)

type DramaJob struct {
	MediaReferences   []DramaMedia         `json:"media_references,omitempty"`
	RequestHash       string               `json:"request_hash,omitempty"`
	ID                string               `json:"id"`
	WorkID            string               `json:"work_id"`
	Kind              string               `json:"kind"`
	Base              string               `json:"base"`
	Inputs            map[string]string    `json:"inputs"`
	SkillVersion      string               `json:"skill_version"`
	References        []string             `json:"references"`
	Prompt            string               `json:"-"`
	Request           string               `json:"request"`
	Candidate         string               `json:"candidate"`
	CandidateOriginal string               `json:"candidate_original,omitempty"`
	CandidateCheck    *DramaCandidateCheck `json:"candidate_check,omitempty"`
	Applied           bool                 `json:"applied"`
	Production        *DramaProduction     `json:"production,omitempty"`
}

func (s *Studio) dramaJob(id string) (DramaJob, error) {
	var j DramaJob
	var b []byte
	e := s.store.db.QueryRow("SELECT body FROM drama_jobs WHERE id=?", id).Scan(&b)
	if e == nil {
		e = json.Unmarshal(b, &j)
	}
	return j, e
}

// Prompt is private operational state, never a second editable creative document.
func (s *Studio) saveDramaJob(j DramaJob) error {
	type wire DramaJob
	b, e := json.Marshal(struct {
		wire
		PrivatePrompt string `json:"private_prompt"`
	}{wire(j), j.Prompt})
	if e != nil {
		return e
	}
	_, e = s.store.db.Exec("INSERT INTO drama_jobs VALUES(?,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body", j.ID, b)
	return e
}
func (s *Studio) readDramaJob(id string) (DramaJob, error) {
	j, e := s.dramaJob(id)
	if e != nil {
		return j, e
	}
	var b []byte
	var v struct {
		Prompt string `json:"private_prompt"`
	}
	e = s.store.db.QueryRow("SELECT body FROM drama_jobs WHERE id=?", id).Scan(&b)
	if e == nil {
		e = json.Unmarshal(b, &v)
	}
	j.Prompt = v.Prompt
	return j, e
}
func (s *Studio) dramaPrompt(w DramaWork, kind, request string, inputs map[string]string) (string, []string, error) {
	skill := dramaSkills[kind]
	if skill == "" {
		return "", nil, errors.New("未知创作阶段")
	}
	base := "drama_bundle/skills/" + skill + "/"
	refs := []string{base + "SKILL.md", base + "references/stage-contract.md", "drama_bundle/skills/short-drama/references/creator-documents.md"}
	// Signals select craft knowledge; never load an entire corpus for a short scene.
	topics := map[string][]string{"script": {"script-craft.md", "dialogue-craft.md", "screenplay-format.md"}, "storyboard": {"shot-craft.md", "blocking-playbooks.md"}, "motion": {"performance-action-timing.md", "camera-audio-continuity.md", "generability.md"}, "develop": {"story-craft.md"}, "review": {"rubric-story-script.md", "rubric-visual-motion.md", "anti-template-repair.md"}, "edit": {"cut-craft.md", "sound-and-subtitles.md", "delivery-verify.md"}}
	for _, name := range topics[kind] {
		refs = append(refs, base+"references/"+name)
	}
	cfg, e := s.store.settings()
	if e != nil {
		return "", nil, e
	}
	// Adapter metadata is additive; the upstream skill bundle stays immutable.
	startRules := ""
	if kind == "storyboard" {
		startRules = "\n映序分镜附加字段：每个SHOT填写「- 与上一镜关系：连续接拍」或「切换镜头」或「关系不明确」。只有同一时空、同一景别机位、没有剪接、动作紧接上一镜才可声明连续接拍；第一镜、换景别、正反打、换机位、跳时或换场使用切换镜头。不得凭相同角色或相同场景就判断连续，不声称已看过实际视频。\n"
	}
	signal := strings.ToLower(w.Genre + " " + w.Form + " " + request + " " + cfg.Video.Model)
	// Model-specific facts only apply to the selected model, not every provider.
	if kind == "motion" {
		for _, name := range []string{"seedance-2.0", "seedance-2.5", "minimax-h3"} {
			if strings.Contains(signal, name) || strings.Contains(signal, strings.ReplaceAll(name, ".", "-")) {
				refs = append(refs, base+"references/"+name+".md")
			}
		}
	}
	fs.WalkDir(dramaBundle, base+"references", func(p string, d fs.DirEntry, e error) error {
		if e == nil && !d.IsDir() && strings.Contains(p, "genre-cards/") && strings.Contains(signal, strings.TrimSuffix(path.Base(p), ".md")) {
			refs = append(refs, p)
		}
		return nil
	})
	var b strings.Builder
	used := []string{}
	seen := map[string]bool{}
	for _, ref := range refs {
		if seen[ref] {
			continue
		}
		seen[ref] = true
		raw, e := dramaBundle.ReadFile(ref)
		if e != nil {
			if strings.HasSuffix(ref, "stage-contract.md") || strings.HasSuffix(ref, "SKILL.md") {
				return "", nil, e
			}
			continue
		}
		used = append(used, strings.TrimPrefix(ref, "drama_bundle/"))
		fmt.Fprintf(&b, "\n--- %s ---\n%s\n", ref, raw)
	}
	b.WriteString(startRules)
	fmt.Fprintf(&b, "\n映序交付适配（与原版知识分开）：这是一次有边界的纯文字任务。禁止使用工具、读取本机文件或调用任何媒体服务。创作输入仅是以下数据。只输出JSON对象 {\"markdown\":\"本阶段完整Markdown正文\"}，正文遵循上方文档语法，不创建文件。时间线纯音乐使用独立MUSIC-*二级条目、时长字段和可复制提示词；不创作歌词。不修改其他阶段。缺失决定性事实时在正文明确列出待选择项，未读取实际媒体时不得宣称视听质量通过或媒体已经存在。审查须如实标明文字审查及证据范围。\n项目：%s\n用户要求：%s\n", jsonBytes(w), request)
	if w.SeriesID != "" {
		fmt.Fprintf(&b, "\n系列与前情只读快照：%s\n本集主题与要求：%s\n仅制作 %s 一集。场景标识使用 %s-SC001 等；原版示例的EP001须替换成本集集号。共用素材仅可引用下列本集副本路径，不引用其他集当前文件。前情是已确认事实；不要重复演完整前集，不擅自重置关系、伤势或悬念。\n", jsonBytes(w.Context), jsonBytes(w.Plan), dramaEpisodeCode(w), dramaEpisodeCode(w))
	}
	fmt.Fprintf(&b, "\nshort-drama.json（已确认项目档案的只读投影）：\n%s\n", jsonBytes(dramaProjectConfig(w)))

	for _, k := range []string{"develop", "script", "assets", "images", "storyboard", "motion", "edit"} {
		if _, ok := inputs[k]; ok {
			d, e := s.dramaDoc(w.ID, k)
			if e != nil {
				return "", nil, e
			}
			label := "已采用"
			if kind == "review" {
				label = "当前待审查版本"
			}
			fmt.Fprintf(&b, "\n%s %s：\n%s\n", label, d.Path, d.Text)
		}
	}
	if kind == "assets" || kind == "images" || kind == "storyboard" || kind == "motion" {
		fmt.Fprintf(&b, "\n映序语言选择：本次图片可复制正文、人物画面代称及视觉连续性锁的自然语言使用 %s；视频可复制正文使用 %s。创作者可读说明使用项目语言 %s。上游旧稿中的语言声明不覆盖本次选择；必要时只转换当前输出中的自然语言并保留含义，不改写上游文件，不改稳定ID、路径、REF标识或镜头编号。\n", w.PromptLanguage, w.Profile.Language, w.Language)
	}

	if kind == "motion" {
		b.WriteString("\n映序视频文档结构约束：二级标题仅用于 MOTION-* 或 MUSIC-* 条目；挂图清单、投产边界、待选择项和文字审查等说明使用三级标题。每个 MOTION 的分镜、时长、输入参考图、终点字段必须逐字复制对应已采用分镜的值，不同义改写；动作扩写放在状态链及可复制提示词中，不能改变分镜边界。\n")
	}

	selected, e := s.dramaReferencePrompt(w.ID, inputs)
	if e != nil {
		return "", nil, e
	}
	b.WriteString(selected)
	current, e := s.dramaDoc(w.ID, kind)
	if e != nil {
		return "", nil, e
	}
	if current.Text != "" {
		fmt.Fprintf(&b, "\n本阶段当前内容（只按要求定点修订，保留无关段落）：\n%s", current.Text)
	}
	return b.String(), used, nil
}
func dramaJobBytes(j DramaJob) []byte {
	type wire DramaJob
	return jsonBytes(struct {
		wire
		PrivatePrompt string `json:"private_prompt"`
	}{wire(j), j.Prompt})
}
func dramaQuoteBytes(q DramaQuote) []byte {
	type wire DramaQuote
	return jsonBytes(struct {
		wire
		PrivateConfig Settings `json:"private_config"`
	}{wire(q), q.Config})
}
func (s *Studio) newDramaTask(w DramaWork, cfg Settings, j DramaJob, quotes ...*DramaQuote) (Task, error) {
	cfg.PromptTemplates = nil
	t := Task{ID: j.ID, ProjectID: w.ID, Stage: "drama_" + j.Kind, Status: "queued", Created: now(), Updated: now(), Mode: cfg.Mode, Config: cfg, Input: Project{VideoResolution: dramaProductionResolution(j.Production), ID: w.ID, Title: w.Title, AspectRatio: w.Aspect, Duration: w.Duration, AudioMode: "dialogue"}}
	if len(quotes) > 0 {
		t.QuoteID = quotes[0].ID
	}
	e := s.store.transaction(func(tx *sql.Tx) error {
		if _, e := tx.Exec("INSERT INTO tasks(id,project_id,status,body,input,config) VALUES(?,?,?,?,?,?)", t.ID, t.ProjectID, t.Status, jsonBytes(t), jsonBytes(t.Input), jsonBytes(cfg)); e != nil {
			return e
		}
		if _, e := tx.Exec("INSERT INTO drama_jobs VALUES(?,?)", j.ID, dramaJobBytes(j)); e != nil {
			return e
		}
		if len(quotes) > 0 {
			q := *quotes[0]
			q.TaskID = j.ID
			_, e := tx.Exec("UPDATE drama_quotes SET body=? WHERE id=?", dramaQuoteBytes(q), q.ID)
			return e
		}
		return nil
	})
	return t, e
}
func (s *Studio) launchDrama(id string) {
	s.mu.Lock()
	if _, ok := s.running[id]; ok {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, 15*time.Minute)
	s.running[id] = cancel
	s.wg.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.wg.Done()
		defer cancel()
		defer func() { s.mu.Lock(); delete(s.running, id); s.mu.Unlock() }()
		t, e := s.store.task(id)
		if e != nil {
			return
		}
		j, e := s.readDramaJob(id)
		if e != nil {
			return
		}
		t.Status = "running"
		t.Message = "正在执行一个明确任务"
		if e = s.store.saveTask(t); e != nil {
			return
		}
		if j.Production != nil {
			e = s.executeDramaProduction(ctx, &t, &j)
		} else {
			var v struct {
				Markdown string `json:"markdown"`
			}
			if t.Mode == "mock" {
				v.Markdown = dramaMock(j.Kind, t.Input.Duration)
				w, err := s.dramaWork(t.ProjectID)
				e = err
				v.Markdown = strings.ReplaceAll(v.Markdown, "EP001", dramaEpisodeCode(w))
			} else {
				e = s.textJSON(ctx, &t, "creative", j.Prompt, &v)
			}
			if e == nil {
				if strings.TrimSpace(v.Markdown) == "" || len(v.Markdown) > 1<<20 {
					e = errors.New("模型没有返回有效Markdown候选；回执已保留，不自动重新调用")
				} else {
					w, err := s.dramaWork(j.WorkID)
					if err != nil {
						e = err
					} else {
						prepareDramaCandidate(w, &j, v.Markdown)
						s.checkDramaCandidateLinks(&j)
						e = s.saveDramaJob(j)
					}
				}
			}
		}
		if e != nil {
			t.Status = "failed"
			t.Message = e.Error()
			if ctx.Err() != nil {
				t.Status = "cancelled"
			}
		} else {
			t.Status = "completed"
			t.Progress = 100
			t.Message = "候选结果已保存，等待人工检查"
			if j.CandidateCheck != nil {
				if len(j.CandidateCheck.Issues) > 0 {
					t.Message = "候选已保存，格式问题待人工修改；不会重新调用模型"
				} else if len(j.CandidateCheck.Repairs) > 0 {
					t.Message = "候选格式已在本地修复，等待人工检查"
				}
			}
		}
		s.store.saveTask(t)
	}()
}
func (s *Studio) dramaJobRoutes(g *gin.RouterGroup) {
	g.GET("/works/:id/jobs", func(c *gin.Context) {
		tasks, e := s.store.tasks(c.Param("id"))
		out := []any{}
		for _, t := range tasks {
			if !strings.HasPrefix(t.Stage, "drama_") {
				continue
			}
			j, err := s.dramaJob(t.ID)
			if err != nil {
				result(c, nil, err)
				return
			}
			out = append(out, gin.H{"task": t, "job": j})
		}
		result(c, out, e)
	})
	g.POST("/works/:id/create", func(c *gin.Context) {
		var v struct {
			ReferenceMediaIDs []string `json:"reference_media_ids"`
			Kind              string   `json:"kind"`
			Base              string   `json:"base"`
			Request           string   `json:"request"`
			OperationID       string   `json:"operation_id"`
		}
		if !bind(c, &v) {
			return
		}
		s.dramaMu.Lock()
		defer s.dramaMu.Unlock()
		w, e := s.dramaWork(c.Param("id"))
		if e != nil {
			result(c, nil, e)
			return
		}
		d, e := s.dramaDoc(w.ID, v.Kind)
		if e != nil {
			result(c, nil, e)
			return
		}
		if d.Hash != v.Base {
			result(c, nil, errConflict)
			return
		}
		if !dramaID.MatchString(v.OperationID) || len(v.Request) > 20000 {
			result(c, nil, errors.New("请提供有效操作标识和修改要求"))
			return
		}
		requestHash := digest(jsonBytes(v))
		if old, e := s.store.task(v.OperationID); e == nil {
			prior, err := s.dramaJob(v.OperationID)
			if err != nil {
				result(c, nil, err)
				return
			}
			if (prior.RequestHash != "" && prior.RequestHash != requestHash) || (prior.RequestHash == "" && len(v.ReferenceMediaIDs) > 0) {
				result(c, nil, errConflict)
				return
			}
			if old.ProjectID != w.ID || old.Stage != "drama_"+v.Kind {
				result(c, nil, errConflict)
			} else {
				result(c, old, nil)
			}
			return
		}

		var unresolved int
		if e = s.store.db.QueryRow("SELECT count(*) FROM attempts a JOIN tasks t ON t.id=a.task_id WHERE t.project_id=? AND a.status='submitting'", w.ID).Scan(&unresolved); e != nil || unresolved > 0 {
			result(c, nil, errors.New("存在结果未知的原调用，请只恢复原任务"))
			return
		}
		existing, e := s.store.tasks(w.ID)
		if e != nil {
			result(c, nil, e)
			return
		}
		for _, t := range existing {
			if t.Stage == "drama_"+v.Kind && (t.Status == "running" || t.Status == "queued") {
				result(c, nil, errors.New("本阶段已有执行任务，请等待或停止原任务"))
				return
			}
		}
		inputs, e := s.dramaInputs(w.ID, v.Kind)
		if e != nil {
			result(c, nil, e)
			return
		}
		mediaRefs, e := s.selectDramaReferences(w.ID, v.ReferenceMediaIDs)
		if e != nil {
			result(c, nil, e)
			return
		}
		for _, m := range mediaRefs {
			inputs["@media:"+m.ID] = m.Hash
		}
		prompt, refs, e := s.dramaPrompt(w, v.Kind, v.Request, inputs)
		if e != nil {
			result(c, nil, e)
			return
		}
		cfg, e := s.store.settings()
		if e != nil {
			result(c, nil, e)
			return
		}
		if cfg.Mode != "mock" {
			if _, e = resolveEndpoint(cfg, "text"); e != nil {
				result(c, nil, e)
				return
			}
		}
		j := DramaJob{RequestHash: requestHash, MediaReferences: mediaRefs, ID: v.OperationID, WorkID: w.ID, Kind: v.Kind, Base: d.Hash, Inputs: inputs, SkillVersion: dramaVersion, References: refs, Prompt: prompt, Request: v.Request}
		t, e := s.newDramaTask(w, cfg, j)
		if e == nil {
			s.launchDrama(t.ID)
		}
		result(c, t, e)
	})
	g.POST("/jobs/:id/check-format", func(c *gin.Context) {
		s.dramaMu.Lock()
		defer s.dramaMu.Unlock()
		j, e := s.readDramaJob(c.Param("id"))
		if e != nil {
			result(c, nil, e)
			return
		}
		t, e := s.store.task(j.ID)
		if e != nil {
			result(c, nil, e)
			return
		}
		if j.Production != nil || j.Applied || t.Status != "completed" || j.Candidate == "" {
			result(c, nil, errors.New("仅能检查尚未采用的已完成文字候选；不会创建生成任务"))
			return
		}
		w, e := s.dramaWork(j.WorkID)
		if e == nil {
			prepareDramaCandidate(w, &j, j.Candidate)
			s.checkDramaCandidateLinks(&j)
			e = s.saveDramaJob(j)
		}
		result(c, j, e)
	})
	g.POST("/jobs/:id/adopt", func(c *gin.Context) {
		s.dramaMu.Lock()
		defer s.dramaMu.Unlock()
		j, e := s.readDramaJob(c.Param("id"))
		if e != nil {
			result(c, nil, e)
			return
		}
		if j.Applied {
			d, e := s.dramaDoc(j.WorkID, j.Kind)
			result(c, d, e)
			return
		}
		t, e := s.store.task(j.ID)
		if e != nil || t.Status != "completed" || j.Production != nil {
			result(c, nil, errors.New("文字候选尚未完成"))
			return
		}
		w, err := s.dramaWork(j.WorkID)
		if err != nil {
			result(c, nil, err)
			return
		}
		prepareDramaCandidate(w, &j, j.Candidate)
		s.checkDramaCandidateLinks(&j)
		if e = s.saveDramaJob(j); e != nil {
			result(c, nil, e)
			return
		}
		if len(j.CandidateCheck.Issues) > 0 {
			issue := j.CandidateCheck.Issues[0]
			result(c, nil, fmt.Errorf("候选格式待修改：第%d行 %s；原稿已保留，无需重新生成", issue.Line, issue.Message))
			return
		}
		for k, hash := range j.Inputs {
			if !s.dramaDependencyCurrent(j.WorkID, k, hash, j.Kind != "review") {
				result(c, nil, errConflict)
				return
			}
		}
		current, err := s.dramaDoc(j.WorkID, j.Kind)
		if err != nil {
			result(c, nil, err)
			return
		}
		if current.Hash == digest([]byte(j.Candidate)) && current.State == "accepted" && string(jsonBytes(current.Inputs)) == string(jsonBytes(j.Inputs)) {
			j.Applied = true
			err = s.saveDramaJob(j)
			result(c, current, err)
			return
		}

		d, e := s.saveDramaDoc(j.WorkID, j.Kind, j.Base, j.Candidate, true, j.Inputs)
		if e == nil {
			j.Applied = true
			e = s.saveDramaJob(j)
		}
		result(c, d, e)
	})
	g.POST("/jobs/:id/:action", func(c *gin.Context) {
		s.dramaMu.Lock()
		defer s.dramaMu.Unlock()
		j, e := s.readDramaJob(c.Param("id"))
		if e != nil {
			result(c, nil, e)
			return
		}
		t, e := s.store.task(j.ID)
		if e != nil {
			result(c, nil, e)
			return
		}
		switch c.Param("action") {
		case "cancel":
			s.mu.Lock()
			cancel := s.running[j.ID]
			if cancel != nil {
				cancel()
			}
			s.mu.Unlock()
			if cancel == nil {
				t.Status = "cancelled"
				e = s.store.saveTask(t)
			}
		case "recover":
			if t.Status == "running" || t.Status == "queued" {
				result(c, t, nil)
				return
			}
			var n int
			e = s.store.db.QueryRow("SELECT count(*) FROM attempts WHERE task_id=?", j.ID).Scan(&n)
			if e == nil && n == 0 && t.Mode != "mock" {
				e = errors.New("没有原提交回执，恢复不发起新调用；请检查原任务")
			}
			if e == nil {
				s.launchDrama(j.ID)
			}
		default:
			e = errors.New("未知任务操作")
		}
		result(c, t, e)
	})
}
func dramaMock(kind string, duration int) string {
	switch kind {
	case "develop":
		return "# 创作简报 · 本地模拟\n\n仅为流程验收的模板文本，不代表真实模型输出。\n\n目标：寒夜里，两只猫通过互相让出暖处建立信任。"
	case "script":
		return "# EP001 第一堂课 · 本地模拟模板\n\n## EP001-SC001 外 · 路灯下 · 寒夜\n\n阿灰站在纸板边缘，小白蜷在灯影外。阿灰退开一步，让出纸板上的暖处。小白走上纸板，停在阿灰身旁。\n\n小白：你也来。\n\n阿灰伏在小白旁边，两只猫肩侧相贴，风声渐轻。"
	case "assets":
		return "# 视觉设定 · 本地模拟\n\n## 人物 · 阿灰\n- 识别锚点：灰色短毛成年猫，四足行走。\n- 画面代称：Ahui\n\n## 人物 · 小白\n- 识别锚点：白色短毛幼猫，四足行走。\n- 画面代称：Xiaobai\n\n## 地点 · 路灯下\n- 识别锚点：路灯旁铺有旧纸板。"
	case "images":
		return "# 图片提示词 · 本地模拟\n\n## IMG-AHUI · 阿灰角色主图\n- 用途：角色主图\n- 参考：无外部参考\n\n### 可复制提示词\n> A grey short-haired adult cat standing naturally on four paws, plain neutral background.\n"
	case "storyboard":
		return fmt.Sprintf("# 分镜 · 本地模拟\n\n## 场景站位 · EP001-SC001\n- 固定锚点：路灯位于纸板左后方。\n- 人物：阿灰站在纸板边缘，小白在右侧。\n- 轴线：两只猫之间，摄影机位于纸板前侧。\n\n## SHOT-EP001-001 · 让出暖处\n- 来源：EP001-SC001\n- 时长：%ds\n- 目的：以让位表现接纳。\n- 景别/机位：中景，纸板前侧。\n- 运镜：固定；让猫的动作被看清。\n- 起点：阿灰站在纸板上，小白在右侧。\n- 唯一动作：阿灰退开一步，让小白走上纸板。\n- 终点：两只猫在纸板上相邻伏下。\n- 声音：风声，无对白；仅为模拟流程示例，需人工审查剧本覆盖。\n- 视觉依据：《视觉设定.md》·人物「阿灰」；人物「小白」；地点「路灯下」。\n- 输入参考图：无（待补参考图：本镜起始帧）\n\n### 冻结关键帧提示词\n> A grey adult cat stands on cardboard beneath a streetlight, a small white kitten waits beside it, vertical cinematic composition.\n", duration)
	case "motion":
		return fmt.Sprintf("# 视频提示词 · 本地模拟\n\n## MOTION-EP001-001 · 让出暖处\n- 分镜：SHOT-EP001-001\n- 时长：%ds\n- 输入参考图：无（待补参考图：本镜起始帧）\n- 终点：两只猫在纸板上相邻伏下。\n\n### 可复制提示词\n> The grey cat steps aside. The kitten walks onto the cardboard. Both cats settle beside each other. No non-diegetic subtitles.\n", duration)
	case "review":
		return "# EP001 审查\n\n- 结论：PROVISIONAL\n- 复核方式：本地模拟模板，未执行语义或视听审查\n\n## 待人工检查\n- 检查动作完成、对白覆盖与跨镜承接，不将技术成功当作内容通过。"
	case "edit":
		return "# 剪辑单 · 本地模拟\n\n请从已确认媒体选择素材并填写 CUT 条目；本地模拟不假称观看过素材。"
	}
	return ""
}
