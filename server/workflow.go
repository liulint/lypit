package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

var stageNames = map[string]string{"analyze": "故事分析", "masters": "角色主图", "details": "设定图与场景道具", "board_plan": "分镜规划", "shot_rewrite": "按秒数重写单镜", "boards": "分镜生图", "board_retry": "重新生成一版", "board_edit": "基于当前图修改", "shot_video": "单镜视频制作", "videos": "视频与声音", "render": "本地合成"}

func (s *Studio) quoteFor(p Project, cfg Settings, stage string, requests ...ShotRequest) (Quote, error) {
	q := Quote{PromptTemplatesVersion: promptTemplatesVersion, StoryDirectionVersion: storyDirectionVersion, VideoDurationPolicyVersion: 1, ID: uid(), ProjectID: p.ID, Revision: p.Revision, SettingsVersion: cfg.Version, Stage: stage, Mode: cfg.Mode, Unknown: cfg.Mode == "remote" && stage != "render", Expires: time.Now().Add(15 * time.Minute).UTC().Format(time.RFC3339Nano), Items: []QuoteItem{}, AspectRatio: projectAspect(p), Duration: projectDuration(p)}
	q.PlanTrial = firstShotRequest(requests).PlanTrial
	if q.PlanTrial {
		if stage != "board_retry" && stage != "shot_video" && stage != "shot_rewrite" {
			return q, errors.New("规划试做仅支持当前镜头的图片或视频报价")
		}
		var err error
		if stage != "shot_rewrite" {
			p, err = planTrialProject(p, firstShotRequest(requests).ShotID)
		}
		if err != nil {
			return q, err
		}
	}
	if len(requests) > 0 {
		q.ShotID, q.Feedback = requests[0].ShotID, strings.TrimSpace(requests[0].Feedback)
	}
	selectedIDs := firstShotRequest(requests).ReferenceIDs
	if stage != "board_retry" && stage != "board_edit" && selectedIDs != nil {
		return q, errors.New("仅单镜图片报价支持选择参考素材")
	}
	if stage != "board_edit" && firstShotRequest(requests).Preserve != "" {
		return q, errors.New("只有基于当前图修改支持必须保留内容")
	}
	if stage == "board_retry" {
		var err error
		p, err = selectedReferenceProject(p, q.ShotID, selectedIDs)
		if err != nil {
			return q, err
		}
	}
	if stage != "shot_video" && (firstShotRequest(requests).VideoMode != "" || firstShotRequest(requests).EndShotID != "") {
		return q, errors.New("请在单镜视频报价中选择生成方式")
	}
	if stage != "board_retry" && stage != "board_edit" && stage != "shot_video" && stage != "shot_rewrite" && (q.ShotID != "" || q.Feedback != "") {
		return q, errors.New("此阶段不接受单镜重生成参数")
	}
	if p.AudioMode != "" {
		if e := validateAudioMode(p.AudioMode); e != nil {
			return q, e
		}
	}
	if e := validateSpec(q.AspectRatio, q.Duration); e != nil {
		return q, e
	}
	add := func(label string, count int) {
		if count > 0 {
			q.Items = append(q.Items, QuoteItem{Label: label, Count: count})
		}
	}
	if p.Archived {
		return q, errors.New("已归档项目只读")
	}
	if stage != "analyze" && stage != "render" && p.Mode != cfg.Mode {
		return q, errors.New("此项目已有另一模式的素材，请切回原制作模式或新建作品，避免混用模拟与真实素材")
	}
	if stage == "render" {
		q.Mode = p.Mode
	}
	switch stage {
	case "analyze":
		if p.Analyzed {
			return q, errors.New("当前故事已分析；修改故事后可重新分析")
		}
		label := "文本分析（旁白剧本与资产清单）"
		if dialogueMode(p) {
			label = "文本分析（角色对白剧本与资产清单）"
		}
		add(label, 1)
	case "masters":
		if !p.Analyzed || p.AssetsLocked {
			return q, errors.New("请先分析故事，且素材应未锁定")
		}
		n := 0
		for _, a := range p.Assets {
			if a.Kind == "character" && a.Master == nil {
				n++
			}
		}
		add("角色正面主图", n)
	case "details":
		if !p.Analyzed || p.AssetsLocked {
			return q, errors.New("请先分析故事，且素材应未锁定")
		}
		n, m := 0, 0
		for _, a := range p.Assets {
			if a.Kind == "character" {
				if a.Master == nil || !a.Master.Approved {
					return q, errors.New("先人工确认全部角色主图")
				}
				if e := s.verifyImage(*a.Master); e != nil {
					return q, e
				}
				if a.Sheet == nil {
					n++
				}
			} else if a.Master == nil {
				m++
			}
		}
		add("角色2×2设定图（每张本地裁切四视角）", n)
		add("场景与道具图", m)
	case "shot_rewrite":
		q.TargetDuration = firstShotRequest(requests).TargetDuration
		if _, e := retimeShot(p, q.ShotID, q.TargetDuration, q.PlanTrial, cfg); e != nil {
			return q, e
		}
		if _, e := rewriteShot(p, q.ShotID, q.PlanTrial); e != nil {
			return q, e
		}
		add(fmt.Sprintf("重写当前镜头首帧、动作与台词（%d秒，仅文字，完成后等待确认）", q.TargetDuration), 1)
	case "board_plan":
		if err := validatePlanningDurations(p, cfg); err != nil {
			return q, err
		}
		if !p.AssetsLocked {
			return q, errors.New("请先人工确认并锁定全部素材")
		}
		if e := s.assetsReady(p); e != nil {
			return q, e
		}
		if _, e := scriptBeats(p); e != nil {
			return q, e
		}
		add("忠于总体剧本的分镜文字规划（不含生图，完成后等待审核）", 1)
	case "boards":
		if !p.AssetsLocked || !p.PlanApproved || p.PlanVersion != 2 {
			return q, errors.New("请先规划分镜并人工确认采用，再单独报价生图")
		}
		if e := s.assetsReady(p); e != nil {
			return q, e
		}
		if e := validatePlanVideoDurations(cfg, p.Shots); e != nil {
			return q, e
		}
		if e := validateFaithfulPlan(p, p.Shots); e != nil {
			return q, e
		}
		n := 0
		for _, shot := range p.Shots {
			if !shotHasImage(shot) {
				n++
			}
		}
		q.ReferencePolicyVersion = fourViewPolicyVersion
		q.ShotReferencePlans = map[string]ReferencePlan{}
		for _, shot := range p.Shots {
			if shotHasImage(shot) {
				continue
			}
			refs, err := s.fourViewBoardReferences(p, shot)
			if err != nil {
				return q, err
			}
			plan, raw, err := s.prepareFourViewReferences(refs, p, shot)
			if err != nil {
				return q, err
			}
			if err := s.saveReferencePreviews(p, plan, raw); err != nil {
				return q, err
			}
			q.ShotReferencePlans["shot-"+shot.ID+"-0"] = plan
		}
		q.BoardImagesPerShot = 1
		add(fmt.Sprintf("分镜图片（%d镜×1张）", n), n)
	case "board_edit":
		shot, spec, refs, err := s.imageEditScope(p, q.ShotID, q.Feedback, firstShotRequest(requests).Preserve, selectedIDs)
		if err != nil {
			return q, err
		}
		plan, _, err := s.prepareEditReferences(refs, p, "shot-"+shot.ID+"-edit", &spec)
		if err != nil {
			return q, err
		}
		q.ImageEdit = &spec
		q.ImageReferencePlan = &plan
		q.ReferencePolicyVersion = imageEditPlanVersion
		q.ReferenceIDs = append([]string{}, spec.AuxiliaryIDs...)
		add("基于当前图修改一张分镜图片："+shot.Title, 1)
	case "board_retry":
		shot, e := s.retryShot(p, q.ShotID, q.Feedback)
		if e != nil {
			return q, e
		}
		q.ReferenceIDs = append([]string(nil), shot.References...)
		refs, err := s.fourViewBoardReferences(p, shot)
		if err != nil {
			return q, err
		}
		if shotHasImage(shot) {
			refs = append(refs, shot.Candidates[shot.Selected])
		}
		plan, raw, err := s.prepareFourViewReferences(refs, p, shot)
		if err != nil {
			return q, err
		}
		if err := s.saveReferencePreviews(p, plan, raw); err != nil {
			return q, err
		}
		q.ImageReferencePlan = &plan
		q.ReferencePolicyVersion = fourViewPolicyVersion

		if shotHasImage(shot) {
			add("重新生成一张分镜图片："+shot.Title, 1)
		} else {
			add("补生成一张分镜图片："+shot.Title, 1)
		}
	case "shot_video":
		shot, e := s.videoShot(p, q.ShotID, q.Feedback, q.PlanTrial)
		if e != nil {
			return q, e
		}
		if e := validateVideoDuration(cfg, shot); e != nil {
			return q, e
		}
		label := "生成本镜视频"
		if shot.Video != "" {
			label = "重新生成本镜视频"
		}
		sound := "不含统一旁白"
		if dialogueMode(p) {
			sound = "含角色对白，模型需支持同步音频"
		}
		input, e := s.videoInput(p, shot, cfg, firstShotRequest(requests).VideoMode, firstShotRequest(requests).EndShotID)
		if e != nil {
			return q, e
		}
		q.VideoInputs = []VideoInput{input}
		label += " · " + videoModeNames[input.Mode]
		q.Duration = shot.Duration
		add(fmt.Sprintf("%s：%s（%d秒，%s）", label, shot.Title, shot.Duration, sound), 1)
	case "videos":
		if !p.BoardsLocked {
			return q, errors.New("请先确认并锁定分镜")
		}
		if e := validateShotPlan(p); e != nil {
			return q, e
		}
		if e := s.assetsReady(p); e != nil {
			return q, e
		}
		n := 0
		for index, shot := range p.Shots {
			if shot.Video != "" && !videoDependencyCurrent(p, index) {
				return q, errors.New("视频参考或续接来源已变化，请先逐镜重新生成后续视频")
			}
			if shot.Selected < 0 || shot.Selected >= len(shot.Candidates) || !shot.Approved {
				return q, errors.New("分镜选版无效")
			}
			if e := s.verifyImage(shot.Candidates[shot.Selected]); e != nil {
				return q, e
			}
			if e := validateSpeech(p, shot); e != nil {
				return q, e
			}
			if shot.Video == "" {
				if e := validateVideoDuration(cfg, shot); e != nil {
					return q, e
				}
				input, e := s.videoInput(p, shot, cfg, "first_frame", "")
				if e != nil {
					return q, e
				}
				q.VideoInputs = append(q.VideoInputs, input)
				n++
			}
		}
		label := "视频生成"
		if dialogueMode(p) {
			label = "有声视频与角色对白（模型需支持同步音频）"
		}
		add(fmt.Sprintf("%s（%d镜合计%d秒）", label, len(p.Shots), projectDuration(p)), n)
		if !dialogueMode(p) && p.Voice == "" {
			add("统一旁白（模拟模式为静音）", 1)
		}
	case "render":
		if !p.BoardsLocked {
			return q, errors.New("分镜尚未锁定")
		}
		if e := validateShotPlan(p); e != nil {
			return q, e
		}
		for index, shot := range p.Shots {
			if !videoDependencyCurrent(p, index) {
				return q, errors.New("视频参考或续接来源已变化，请先重新生成后续镜头")
			}
			if shot.Video == "" {
				return q, errors.New("请先完成所有镜头视频")
			}
		}
		if !dialogueMode(p) && p.Voice == "" {
			return q, errors.New("请先完成旁白")
		}
		if p.Output != "" {
			return q, errors.New("已有成片，可直接下载")
		}
		q.Unknown = false
		add("FFmpeg 本地合成、导出与成片检查", 1)
	default:
		return q, errors.New("未知制作阶段")
	}
	if len(q.Items) == 0 {
		return q, errors.New("本阶段没有缺少的素材，无需再次生成")
	}
	if cfg.Mode == "remote" {
		caps := []string{}
		switch stage {
		case "analyze", "board_plan", "shot_rewrite":
			caps = []string{"text"}
		case "masters", "details", "board_retry", "board_edit":
			caps = []string{"image"}
		case "boards":
			caps = []string{"image"}
		case "shot_video":
			caps = []string{"video"}
		case "videos":
			caps = []string{"video"}
			if !dialogueMode(p) && p.Voice == "" {
				caps = append(caps, "voice")
			}
		}
		if e := s.validateCatalogBindings(cfg, caps...); e != nil {
			return q, e
		}
		for _, cap := range caps {
			ep, e := resolveEndpoint(cfg, cap)
			if e != nil {
				return q, e
			}
			if ep.Transport == "cli" {
				if _, e = cliExecutable(ep); e != nil {
					return q, e
				}
				continue
			}
			if e = validEndpoint(ep); e != nil {
				return q, e
			}
			_, credentialErr := s.credential(ep)
			if ep.Model == "" || credentialErr != nil {
				return q, fmt.Errorf("请为%s选择厂商与模型，并设置该厂商的API Key", map[string]string{"text": "文本规划", "image": "图片生成", "video": "视频生成", "voice": "旁白配音"}[cap])
			}
		}
	}
	applyVideoEstimate(&q, p, cfg)
	return q, nil
}
func (s *Studio) quoteHandler(c *gin.Context) {
	var v struct {
		TargetDuration int      `json:"target_duration"`
		Preserve       string   `json:"preserve"`
		PlanTrial      bool     `json:"plan_trial"`
		ReferenceIDs   []string `json:"reference_ids"`
		ReplacesTaskID string   `json:"replaces_task_id"`
		Stage          string   `json:"stage"`
		Revision       int      `json:"revision"`
		ShotID         string   `json:"shot_id"`
		Feedback       string   `json:"feedback"`
		VideoMode      string   `json:"video_mode"`
		EndShotID      string   `json:"end_shot_id"`
	}
	if !bind(c, &v) {
		return
	}
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	p, e := s.store.project(c.Param("id"))
	if e != nil {
		result(c, nil, e)
		return
	}
	if p.Revision != v.Revision {
		result(c, nil, errConflict)
		return
	}
	replacementFingerprint := ""
	if v.PlanTrial && v.ReplacesTaskID != "" {
		result(c, nil, errors.New("试做任务请恢复原任务或放弃后重新报价"))
		return
	}
	if v.ReplacesTaskID != "" {
		_, replacementFingerprint, e = s.boardReplacement(p, v.Stage, v.ShotID, v.ReplacesTaskID)
		if e != nil {
			result(c, nil, e)
			return
		}
	}
	if v.ReplacesTaskID == "" && s.store.generationBusy(p.ID, v.Stage, v.ShotID, v.PlanTrial) {
		result(c, nil, errors.New("请先完成或放弃当前任务"))
		return
	}
	cfg, e := s.store.settings()
	if e != nil {
		result(c, nil, e)
		return
	}
	q, e := s.quoteFor(p, cfg, v.Stage, ShotRequest{TargetDuration: v.TargetDuration, PlanTrial: v.PlanTrial, Preserve: v.Preserve, ReferenceIDs: v.ReferenceIDs, ShotID: v.ShotID, Feedback: v.Feedback, VideoMode: v.VideoMode, EndShotID: v.EndShotID})
	if e == nil {
		q.ReplacesTaskID, q.ReplacementFingerprint = v.ReplacesTaskID, replacementFingerprint
		_, e = s.store.db.Exec("INSERT INTO quotes VALUES(?,?,?)", q.ID, q.ProjectID, jsonBytes(q))
	}
	result(c, q, e)
}
func (s *Studio) confirmHandler(c *gin.Context) {
	var v struct {
		AllowReplacement bool `json:"allow_replacement"`
		AllowUnknown     bool `json:"allow_unknown"`
	}
	if !bind(c, &v) {
		return
	}
	s.store.mu.Lock()
	q, e := s.store.quote(c.Param("id"))
	if e != nil {
		s.store.mu.Unlock()
		result(c, nil, e)
		return
	}
	if q.TaskID != "" {
		t, e := s.store.task(q.TaskID)
		s.store.mu.Unlock()
		result(c, t, e)
		return
	}
	p, e := s.store.project(q.ProjectID)
	if e != nil {
		s.store.mu.Unlock()
		result(c, nil, e)
		return
	}
	cfg, e := s.store.settings()
	if e != nil {
		s.store.mu.Unlock()
		result(c, nil, e)
		return
	}
	if expired(q) || q.Revision != p.Revision || q.SettingsVersion != cfg.Version {
		s.store.mu.Unlock()
		result(c, nil, errors.New("报价已过期或项目、模型或提示词配置已变化，请重新报价"))
		return
	}
	if q.Unknown && !v.AllowUnknown {
		s.store.mu.Unlock()
		result(c, nil, errors.New("请明确接受按实际用量收费，金额无可靠上界"))
		return
	}
	var replaced Task
	if q.ReplacesTaskID != "" {
		if !v.AllowReplacement {
			s.store.mu.Unlock()
			result(c, nil, errors.New("请确认只补生成当前缺图一张；原请求可能已计费，新生成将另行计费"))
			return
		}
		var fingerprint string
		replaced, fingerprint, e = s.boardReplacement(p, q.Stage, q.ShotID, q.ReplacesTaskID)
		if e == nil && fingerprint != q.ReplacementFingerprint {
			e = errors.New("原任务或回执已变化，请重新报价")
		}
		if e != nil {
			s.store.mu.Unlock()
			result(c, nil, e)
			return
		}
	}
	if q.ReplacesTaskID == "" && s.store.generationBusy(p.ID, q.Stage, q.ShotID, q.PlanTrial) {
		s.store.mu.Unlock()
		result(c, nil, errors.New("已有任务尚未处理"))
		return
	}
	req := ShotRequest{TargetDuration: q.TargetDuration, PlanTrial: q.PlanTrial, ReferenceIDs: q.ReferenceIDs, ShotID: q.ShotID, Feedback: q.Feedback}
	if q.Stage == "board_edit" {
		if q.ImageEdit == nil {
			s.store.mu.Unlock()
			result(c, nil, errors.New("编辑报价缺少独立底图，请重新报价"))
			return
		}
		req.Preserve = q.ImageEdit.Preserve
		req.ReferenceIDs = q.ImageEdit.AuxiliaryIDs
	}
	if q.Stage == "shot_video" && len(q.VideoInputs) == 1 {
		req.VideoMode = q.VideoInputs[0].Mode
		req.EndShotID = q.VideoInputs[0].EndShotID
	}
	fresh, checkErr := s.quoteFor(p, cfg, q.Stage, req)
	if checkErr == nil && q.PromptTemplatesVersion > 0 && fresh.PromptTemplatesVersion != q.PromptTemplatesVersion {
		checkErr = errors.New("提示词方案已变化，请重新报价")
	}
	if checkErr == nil && (string(jsonBytes(fresh.ShotReferencePlans)) != string(jsonBytes(q.ShotReferencePlans)) || string(jsonBytes(fresh.ImageReferencePlan)) != string(jsonBytes(q.ImageReferencePlan)) || fresh.ReferencePolicyVersion != q.ReferencePolicyVersion || string(jsonBytes(fresh.ImageEdit)) != string(jsonBytes(q.ImageEdit))) {
		checkErr = errors.New("参考板方案或原始素材已变化，请重新报价；未调用模型")
	}
	if checkErr == nil && len(q.VideoInputs) > 0 && string(jsonBytes(fresh.VideoInputs)) != string(jsonBytes(q.VideoInputs)) {
		checkErr = errors.New("视频参考或续接来源已变化，请重新报价")
	}
	if e = checkErr; e != nil {
		s.store.mu.Unlock()
		result(c, nil, e)
		return
	}
	if q.PlanTrial && q.Stage != "shot_rewrite" {
		p, e = planTrialProject(p, q.ShotID)
		if e != nil {
			s.store.mu.Unlock()
			result(c, nil, e)
			return
		}
	}
	shotInputHash := ""
	if q.Stage == "board_retry" || q.Stage == "board_edit" || q.Stage == "shot_video" {
		for _, shot := range p.Shots {
			if shot.ID == q.ShotID {
				shotInputHash = digest(jsonBytes(shot))
			}
		}
	}
	if q.Stage == "board_retry" && q.ReferenceIDs != nil {
		p, e = selectedReferenceProject(p, q.ShotID, q.ReferenceIDs)
		if e != nil {
			s.store.mu.Unlock()
			result(c, nil, e)
			return
		}
	}
	// Keep CLI selection stable across PATH changes when resuming this confirmed task.
	for i := range cfg.Vendors {
		vendor := &cfg.Vendors[i]
		if vendor.Type == "cli" {
			if path, e := cliExecutable(Endpoint{CLI: vendor.CLI, Executable: vendor.Executable}); e == nil {
				vendor.Executable = path
			}
		}
	}
	if q.PromptTemplatesVersion > 0 {
		cfg = snapshotPrompts(cfg)
	}
	t := Task{PromptTemplatesVersion: q.PromptTemplatesVersion, StoryDirectionVersion: q.StoryDirectionVersion, TargetDuration: q.TargetDuration, VideoDurationPolicyVersion: q.VideoDurationPolicyVersion, ShotInputHash: shotInputHash, ShotReferencePlans: q.ShotReferencePlans, ImageEdit: q.ImageEdit, ImageReferencePlan: q.ImageReferencePlan, ReferencePolicyVersion: q.ReferencePolicyVersion, VideoInputs: q.VideoInputs, BoardImagesPerShot: q.BoardImagesPerShot, ShotID: q.ShotID, Feedback: q.Feedback, ID: uid(), ProjectID: p.ID, QuoteID: q.ID, Stage: q.Stage, Status: "queued", Message: "等待执行", Mode: q.Mode, Created: now(), Updated: now(), Input: p, Config: cfg}
	q.TaskID = t.ID
	t.PlanTrial = q.PlanTrial
	e = s.store.transaction(func(tx *sql.Tx) error {
		if q.ReplacesTaskID != "" {
			replaced.Status = "superseded"
			replaced.Message = "已确认单镜新报价；原任务停止恢复，已有图片及提交记录保留。新任务：" + t.ID
			replaced.Updated = now()
			if _, err := tx.Exec("UPDATE tasks SET status=?,body=? WHERE id=?", replaced.Status, jsonBytes(replaced), replaced.ID); err != nil {
				return err
			}
		}
		if _, e := tx.Exec("INSERT INTO tasks VALUES(?,?,?,?,?,?)", t.ID, t.ProjectID, t.Status, jsonBytes(t), jsonBytes(p), jsonBytes(cfg)); e != nil {
			return e
		}
		_, e := tx.Exec("UPDATE quotes SET body=? WHERE id=?", jsonBytes(q), q.ID)
		return e
	})
	s.store.mu.Unlock()
	if e == nil {
		s.launch(t.ID)
	}
	result(c, t, e)
}
func (s *Studio) launch(id string) {
	s.mu.Lock()
	if _, ok := s.running[id]; ok {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, 45*time.Minute)
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
		t.Status = "running"
		t.Message = "正在执行"
		if e = s.store.saveTask(t); e != nil {
			return
		}
		e = s.execute(ctx, &t)
		s.store.mu.Lock()
		defer s.store.mu.Unlock()
		if e == nil && ctx.Err() != nil {
			e = ctx.Err()
		}
		if e == nil {
			e = s.commitTask(t)
		}
		if e != nil {
			t.Status = "failed"
			if ctx.Err() != nil {
				t.Status = "cancelled"
			}
			t.Message = e.Error()
			_ = s.store.saveTask(t)
		}
	}()
}
func (s *Studio) taskAction(c *gin.Context) {
	if t, e := s.store.task(c.Param("id")); e == nil && strings.HasPrefix(t.Stage, "drama_") {
		result(c, nil, errors.New("请从新工作流恢复或取消任务"))
		return
	}
	id := c.Param("id")
	action := c.Param("action")
	s.mu.Lock()
	cancel, running := s.running[id]
	s.mu.Unlock()
	if action == "cancel" {
		if running {
			cancel()
		}
		result(c, gin.H{"message": "已请求取消；供应商已接收的任务可能继续执行并计费"}, nil)
		return
	}
	if running {
		result(c, nil, errors.New("任务仍在执行，请等待停止"))
		return
	}
	s.store.mu.Lock()
	t, e := s.store.task(id)
	if e != nil {
		s.store.mu.Unlock()
		result(c, nil, e)
		return
	}
	if t.Status != "failed" && t.Status != "interrupted" && t.Status != "cancelled" {
		s.store.mu.Unlock()
		result(c, nil, errors.New("当前任务不可恢复或放弃"))
		return
	}
	switch action {
	case "resume":
		if e = s.checkBoardRecovery(t); e != nil {
			s.store.mu.Unlock()
			result(c, nil, e)
			return
		}
		p, e := s.store.project(t.ProjectID)
		if e == nil && p.Revision != t.Input.Revision {
			if t.ShotInputHash != "" {
				_, e = mergeSingleShotTask(p, t)
			} else if !t.PlanTrial && (t.Stage == "boards" || t.Stage == "shot_video") {
				_, e = mergeProgressiveTask(p, t)
			} else {
				e = errConflict
			}
		}
		if e != nil {
			s.store.mu.Unlock()
			result(c, nil, errConflict)
			return
		}
		if t.Stage == "boards" {
			if _, e = s.store.db.Exec("UPDATE attempts SET status='unknown' WHERE task_id=? AND status='submitting'", t.ID); e != nil {
				s.store.mu.Unlock()
				result(c, nil, e)
				return
			}
		}
		t.Status = "queued"
		t.Message = "恢复原有任务，不重提结果未知的请求"
		e = s.store.saveTask(t)
		s.store.mu.Unlock()
		if e == nil {
			s.launch(t.ID)
		}
		result(c, t, e)
	case "abandon":
		t.Status = "abandoned"
		t.Message = "已放弃本地任务，远端已提交任务可能仍计费；历史记录保留"
		e = s.store.saveTask(t)
		s.store.mu.Unlock()
		result(c, t, e)
	default:
		s.store.mu.Unlock()
		result(c, nil, errors.New("未知任务操作"))
	}
}
func (s *Studio) progress(t *Task, n int, msg string) error {
	t.Progress = n
	t.Message = msg
	if err := s.saveTaskInput(*t); err != nil {
		return err
	}
	if t.Stage == "boards" && len(t.Input.Shots) > 0 {
		return s.publishBoards(*t)
	}
	return nil
}
func (s *Studio) execute(ctx context.Context, t *Task) error {
	p := &t.Input
	p.Mode = t.Mode
	switch t.Stage {
	case "analyze":
		if e := s.analyze(ctx, t); e != nil {
			return e
		}
	case "masters", "details":
		for i := range p.Assets {
			a := &p.Assets[i]
			if e := ctx.Err(); e != nil {
				return e
			}
			if t.Stage == "masters" && a.Kind == "character" && a.Master == nil {
				im, e := s.makeTemplateImage(ctx, t, "character_master", map[string]string{"style": p.Style, "asset_description": a.Description}, "master-"+a.ID, p.Style+"。角色正面全身主图，纯净背景。"+a.Description, nil, false)
				if e != nil {
					return e
				}
				a.Master = &im
			}
			if t.Stage == "details" {
				if a.Kind == "character" && a.Sheet == nil {
					im, e := s.makeTemplateImage(ctx, t, "character_sheet", map[string]string{"style": p.Style, "asset_description": a.Description}, "sheet-"+a.ID, p.Style+"。以参考图人物为唯一身份依据，固定同一服装。生成精确2×2四格设定图，四格等大无留白：左上正面全身、右上侧面全身、左下背面全身、右下面部近景。"+a.Description, []Image{*a.Master}, true)
					if e != nil {
						return e
					}
					views, e := s.cropSheet(im)
					if e != nil {
						return e
					}
					a.Sheet = &im
					a.Views = views
				} else if a.Kind != "character" && a.Master == nil {
					im, e := s.makeTemplateImage(ctx, t, "asset_reference", map[string]string{"style": p.Style, "asset_description": a.Description, "asset_kind": a.Kind}, "asset-"+a.ID, p.Style+"。完整独立的"+a.Kind+"参考图。"+a.Description, nil, false)
					if e != nil {
						return e
					}
					a.Master = &im
				}
			}
			if e := s.progress(t, (i+1)*90/len(p.Assets), "准备参考素材："+a.Name); e != nil {
				return e
			}
		}
	case "shot_video":
		return s.generateShotVideo(ctx, t, s.makeVideo)
	case "board_edit":
		return s.editBoard(ctx, t, s.makeImage)
	case "board_retry":
		return s.regenerateBoard(ctx, t, s.makeImage)
	case "shot_rewrite":
		return s.rewriteShotTask(ctx, t)
	case "board_plan":
		return s.planFaithfulShots(ctx, t)
	case "boards":
		if p.PlanVersion == 2 && !p.PlanApproved {
			return errors.New("分镜规划尚未人工确认")
		}
		if len(p.Shots) == 0 {
			if e := s.planLegacyShots(ctx, t); e != nil {
				return e
			}
			if e := s.progress(t, 5, "分镜规划完成"); e != nil {
				return e
			}
		}
		if e := s.fillBoardImages(ctx, t, s.makeImage); e != nil {
			return e
		}
	case "videos":
		if e := s.fillVideos(ctx, t, s.makeVideo); e != nil {
			return e
		}
		if !dialogueMode(*p) && p.Voice == "" {
			file, e := s.makeVoice(ctx, t)
			if e != nil {
				return e
			}
			p.Voice = file
		}
	case "render":
		file, e := s.render(ctx, t)
		if e != nil {
			return e
		}
		p.Output = file
	default:
		return errors.New("未知任务阶段")
	}
	return ctx.Err()
}
func (s *Studio) analyze(ctx context.Context, t *Task) error {
	p := &t.Input
	if t.Mode == "mock" {
		p.Script = p.Idea + "\n\n【本地模拟结构】\n起：主人公在熟悉的地方发现一个改变生活的线索。\n承：循着线索，过去与当下逐渐交叠。\n转：在犹豫与尝试之间，主人公决定迈出一步。\n合：回到最初的地方，带着新的理解继续生活。\n\n这是用于验证流程的模板，并非AI编剧结果。"
		p.Assets = []Asset{{ID: uid(), Kind: "character", Name: "故事主人公", Description: "青年，深色短发，浅色衬衫、深色长裤；各镜保持服装与面部特征一致", Views: []Image{}}, {ID: uid(), Kind: "scene", Name: "故事主场景", Description: "温暖的旧街区，窗边自然光，木质陈设，低饱和配色", Views: []Image{}}, {ID: uid(), Kind: "prop", Name: "关键线索", Description: "一本有岁月痕迹的深绿色旧书，封面无文字", Views: []Image{}}}
	} else {
		var v struct {
			Conflict string  `json:"conflict"`
			Script   string  `json:"script"`
			Assets   []Asset `json:"assets"`
		}
		prompt := fmt.Sprintf("你是短片导演。分析用户故事，先写%d秒、%s的剧本与参考资产清单，不生成分镜。", projectDuration(*p), aspectLabel(*p)) + "返回JSON {\"script\":\"完整中文剧本\",\"assets\":[{\"kind\":\"character|scene|prop\",\"name\":\"名称\",\"description\":\"具体外观、服装、固定特征或环境描述\"}]}。角色1～3、场景1～3、道具0～2。风格：" + p.Style + "。故事：" + p.Idea + "。" + audioDirection(*p)
		if t.StoryDirectionVersion >= storyDirectionVersion {
			prompt = scriptDirectionPrompt(*p, t.Config)
		}
		policy := videoDurationPolicy(t.Config)
		prompt, err := t.renderPrompt("analyze", prompt, map[string]string{
			"duration": fmt.Sprint(projectDuration(*p)), "aspect": aspectLabel(*p), "min_duration": fmt.Sprint(max(2, policy.Min)), "max_duration": fmt.Sprint(policy.Max),
			"style": p.Style, "idea": p.Idea, "audio_direction": audioDirection(*p), "output_contract": promptOutputContract("analyze"),
		})
		if err != nil {
			return err
		}
		if e := s.textJSON(ctx, t, "analyze", prompt, &v); e != nil {
			return e
		}
		if v.Conflict != "" {
			return fmt.Errorf("剧本规划冲突：%s；原响应已保存，不自动重试", v.Conflict)
		}
		counts := map[string]int{}
		if strings.TrimSpace(v.Script) == "" || len(v.Script) > 50000 {
			return errors.New("模型返回剧本为空或过长")
		}
		for i := range v.Assets {
			a := &v.Assets[i]
			counts[a.Kind]++
			if a.Kind != "character" && a.Kind != "scene" && a.Kind != "prop" {
				return errors.New("模型返回未知素材类型")
			}
			if a.Name == "" || a.Description == "" {
				return errors.New("模型返回素材描述不完整")
			}
			*a = Asset{ID: uid(), Kind: a.Kind, Name: a.Name, Description: a.Description, Views: []Image{}}
		}
		if counts["character"] < 1 || counts["character"] > 3 || counts["scene"] < 1 || counts["scene"] > 3 || counts["prop"] > 2 {
			return errors.New("模型返回资产数量超出授权范围")
		}
		if t.StoryDirectionVersion >= storyDirectionVersion {
			candidate := *p
			candidate.Script, candidate.Assets = v.Script, v.Assets
			beats, err := scriptBeats(candidate)
			if err != nil {
				return err
			}
			if len(beats) == 0 {
				return errors.New("新剧本缺少连续时间段；原响应已保存，请调整后重新报价，不自动重试")
			}
			if err := validatePlanningDurations(candidate, t.Config); err != nil {
				return err
			}
		}
		p.Script = v.Script
		p.Assets = v.Assets
	}
	if t.Mode == "mock" {
		p.Script += "\n\n" + audioDirection(*p)
	}
	p.Analyzed = true
	return s.progress(t, 95, "故事与资产清单已准备，等待编辑确认")
}
func (s *Studio) planLegacyShots(ctx context.Context, t *Task) error {
	p := &t.Input
	durations := shotDurations(*p)
	if t.Mode == "mock" {
		titles := []string{"故事的入口", "发现线索", "靠近与观察", "记忆浮现", "短暂的犹豫", "做出选择", "变化发生", "回到新的日常"}
		for i, duration := range durations {
			title := titles[i*(len(titles)-1)/(len(durations)-1)]
			refs := []string{}
			for _, a := range p.Assets {
				refs = append(refs, a.ID)
			}
			speech := title + "，生活在细微处悄悄改变。"
			if dialogueMode(*p) {
				speech = ""
				for _, a := range p.Assets {
					if a.Kind == "character" {
						speech = a.Name + "：我们一起去看看吧。"
						break
					}
				}
			}
			p.Shots = append(p.Shots, Shot{ID: uid(), Title: title, Description: title + "。主角在既定场景中完成动作，镜头缓慢推进，画面留有呼吸空间。", Narration: speech, Duration: duration, References: refs, Candidates: []Image{}, Selected: 0})
		}
		return nil
	}
	var v struct {
		Shots []Shot `json:"shots"`
	}
	maxRefs := 6
	if t.ReferencePolicyVersion == referencePlanVersion {
		maxRefs = len(p.Assets)
	}
	prompt := fmt.Sprintf("作为导演，将剧本拆成严格%d镜，总时长%d秒，画幅%s；各镜秒数按顺序为%v，台词须适合各镜时长。", len(durations), projectDuration(*p), aspectLabel(*p), durations) + fmt.Sprintf("只使用清单中的资产ID，每镜1～%d个references，不得新增资产。", maxRefs) + "返回JSON {\"shots\":[{\"title\":\"镜头名\",\"description\":\"画面动作和运镜\",\"narration\":\"本镜台词\",\"references\":[\"资产ID\"]}]}。剧本：" + p.Script + "；清单：" + string(jsonBytes(p.Assets))
	prompt += "。" + audioDirection(*p)
	if dialogueMode(*p) {
		prompt += "narration字段存本镜角色对白（不是旁白），每行角色名称：台词，无人说话则空字符串。台词中的说话者必须来自角色清单，并在本镜references中引用。description必须写清谁在说话及嘴部动作。"
	}
	if e := s.textJSON(ctx, t, "boards-plan", prompt, &v); e != nil {
		return e
	}
	if len(v.Shots) != len(durations) {
		return fmt.Errorf("导演未返回%d个镜头，请检查原任务响应", len(durations))
	}
	allowed := map[string]bool{}
	for _, a := range p.Assets {
		allowed[a.ID] = true
	}
	for i := range v.Shots {
		q := &v.Shots[i]
		if q.Title == "" || q.Description == "" || len(q.References) == 0 || len(q.References) > maxRefs {
			return errors.New("导演返回镜头内容不完整")
		}
		seen := map[string]bool{}
		for _, id := range q.References {
			if !allowed[id] || seen[id] {
				return errors.New("导演需要新增资产或返回重复引用，请放弃此任务后修改资产设定")
			}
			seen[id] = true
		}
		if e := validateSpeech(*p, *q); e != nil {
			return e
		}
		*q = Shot{ID: uid(), Title: q.Title, Description: q.Description, Narration: q.Narration, References: q.References, Duration: durations[i], Candidates: []Image{}, Selected: 0}
	}
	p.Shots = v.Shots
	return nil
}
