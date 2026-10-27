package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

func storyboardScope(b *Board) map[string]int {
	characters, assets, shots := 3, 5, 12
	if b != nil {
		characters = len(b.Characters)
		assets = len(b.Scenes) + len(b.Props)
		shots = len(b.Shots)
	}
	return map[string]int{"text": 2, "character_image": characters * 3, "storyboard_image": assets + shots*3,
		"vision": 1 + characters*3 + assets + shots*2, "_repair": shots, "_vision_conditional": shots}
}
func fillScope(b Board, references []string, shotID string) map[string]int {
	scope := map[string]int{"text": 0}
	if b.Schema < 3 {
		if len(references) > 0 && len(b.SourceMappings) == 0 {
			scope["vision"]++
		}
		for _, c := range b.Characters {
			for _, kind := range []string{"front", "side", "close"} {
				var view ReferenceView
				for _, v := range c.Views {
					if v.Kind == kind {
						view = v
					}
				}
				if view.Image == "" {
					scope["character_image"]++
				}
				if view.Image == "" || !reviewOK(view.Review) {
					scope["vision"]++
				}
			}
		}
		for _, a := range append(append([]SceneAsset{}, b.Scenes...), b.Props...) {
			if a.Image == "" {
				scope["storyboard_image"]++
			}
			if a.Image == "" || !reviewOK(a.Review) {
				scope["vision"]++
			}
		}
	}
	forced := dependentShots(b, []string{shotID})
	for _, q := range b.Shots {
		if q.Stale || shotID != "" && slices.Contains(forced, q.ID) {
			scope["storyboard_image"] += 3
			scope["_repair"]++
			scope["vision"] += 2
			scope["_vision_conditional"]++
			continue
		}
		if !shotNeedsGeneration(q) {
			continue
		}
		for i := 0; i < 2; i++ {
			if i >= len(q.Candidates) || q.Candidates[i].Image == "" {
				scope["storyboard_image"]++
			}
		}
		if len(q.Candidates) < 2 || q.Candidates[0].Review.State == "" || q.Candidates[1].Review.State == "" || q.Candidates[0].Review.State == "incomplete" || q.Candidates[1].Review.State == "incomplete" {
			scope["vision"]++
		}
		passed := false
		for _, c := range q.Candidates {
			passed = passed || reviewOK(c.Review)
		}
		if !passed {
			if len(q.Candidates) < 3 || q.Candidates[2].Image == "" {
				scope["storyboard_image"]++
				scope["_repair"]++
			}
			if len(q.Candidates) < 3 || q.Candidates[2].Review.State == "" || q.Candidates[2].Review.State == "incomplete" {
				scope["vision"]++
				scope["_vision_conditional"]++
			}
		}
	}
	return scope
}
func storyboardRequestSpecs(settings Settings, scope map[string]int, planner string) []RequestSpec {
	specs := []RequestSpec{}
	add := func(role, kind, prompt string, count int, conditional bool) {
		if count == 0 {
			return
		}
		config := imageSettings(settings, role)
		p, m, e := selectModel(config, kind)
		if e != nil {
			return
		}
		var estimate *float64
		if p.Adapter == "mock" {
			z := 0.
			estimate = &z
		} else if p.Adapter != "cli" && m.Price != nil && m.Unit == "request" {
			v := *m.Price * float64(count)
			estimate = &v
		}
		specs = append(specs, RequestSpec{Role: role, Kind: kind, Provider: p.Name + " (" + p.ID + ")", Model: m.ID, Prompt: prompt, Count: count, Conditional: conditional, References: []string{}, Estimate: estimate, Parameters: map[string]any{"cli_options": m.CLIOptions, "price": m.Price, "unit": m.Unit, "source": m.PriceSource, "verified": m.Verified, "size": m.Sizes, "max_references": m.MaxReferences, "reuse": "已有相同生成意图与内容的成功素材复用，不重复提交"}})
	}
	add("text", "text", storyDirector+"\n分镜导演："+planner+shotSchema, scope["text"], false)
	add("character_image", "image", "每角色三张独立视图：正面全身→引用主图生成侧面/四分之三视图→引用主图生成面部近景。继承用户明确关联的图片和风格。", scope["character_image"], false)
	shots := scope["_repair"]
	imageDescription := "历史场景、关键道具与分镜候选的局部补图，仅按清单缺少范围执行。"
	if scope["_asset_locked"] > 0 {
		imageDescription = "仅生成分镜候选，每镜两张，直接引用已人工确认的角色四视图、场景和道具，不重新制作参考资产。"
	}
	add("storyboard_image", "image", imageDescription, scope["storyboard_image"]-shots, false)
	add("storyboard_image", "image", "仅在该镜两张候选均未通过时，根据具体错误补一张修复图。每镜最多一次；没有触发则不提交。", shots, true)
	add("vision", "vision", "用户参考图分类、角色/场景/道具检查、两张候选比较与条件修复复核。分数、严重错误及修复建议必须结构化返回。", scope["vision"]-scope["_vision_conditional"], false)
	add("vision", "vision", "仅在两张候选均未通过时审核一次修复候选，不触发则不提交。", scope["_vision_conditional"], true)
	return specs
}
func preserveExtendedAssets(old *Board, b *Board) {
	sameStyle := hash(old.Visual) == hash(b.Visual)
	stableCharacters := map[string]bool{}
	for i := range b.Characters {
		c := &b.Characters[i]
		for _, o := range old.Characters {
			if sameStyle && c.ID == o.ID && c.Description == o.Description && c.FixedFeatures == o.FixedFeatures && c.Name == o.Name && (b.Schema < 3 || hash(c.Views) == hash(o.Views)) {
				c.Image = o.Image
				c.Views = clone(o.Views)
				c.Sources = slices.Clone(o.Sources)
				stableCharacters[c.ID] = true
			}
		}
	}
	stableAssets := map[string]bool{}
	for _, pair := range []struct {
		previous []SceneAsset
		current  *[]SceneAsset
	}{{old.Scenes, &b.Scenes}, {old.Props, &b.Props}} {
		for i := range *pair.current {
			a := &(*pair.current)[i]
			for _, o := range pair.previous {
				if sameStyle && a.ID == o.ID && a.Description == o.Description && a.Name == o.Name && (b.Schema < 3 || a.Hash == o.Hash) {
					a.Image = o.Image
					a.Hash = o.Hash
					a.Review = clone(o.Review)
					a.Sources = slices.Clone(o.Sources)
					stableAssets[a.ID] = true
				}
			}
		}
	}
	stableShots := map[string]bool{}
	for i := range b.Shots {
		q := &b.Shots[i]
		deps := sameStyle
		for _, id := range q.Characters {
			deps = deps && stableCharacters[id]
		}
		if b.Schema >= 2 {
			deps = deps && stableAssets[q.SceneID]
			if q.PropID != "" {
				deps = deps && stableAssets[q.PropID]
			}
		}
		for _, id := range q.References {
			deps = deps && stableShots[id]
		}
		for _, o := range old.Shots {
			if q.ID != o.ID {
				continue
			}
			if deps && q.ImagePrompt == o.ImagePrompt && hash(q.Camera) == hash(o.Camera) && hash(q.Continuity) == hash(o.Continuity) && q.SceneID == o.SceneID && q.PropID == o.PropID && slices.Equal(q.Characters, o.Characters) && slices.Equal(q.References, o.References) && !o.Stale {
				q.Image = o.Image
				q.Candidates = clone(o.Candidates)
				q.Selected = o.Selected
				q.Recommended = o.Recommended
				q.Warning = o.Warning
				stableShots[q.ID] = true
			}
			oldPrompt := o.BaseVideoPrompt
			if oldPrompt == "" {
				oldPrompt = o.VideoPrompt
			}
			newPrompt := q.BaseVideoPrompt
			if newPrompt == "" {
				newPrompt = q.VideoPrompt
			}
			if q.Image == o.Image && q.Image != "" && newPrompt == oldPrompt && q.Duration == o.Duration {
				q.Video = o.Video
				q.VideoGenerationID = o.VideoGenerationID
				q.VideoPrompt = o.VideoPrompt
				q.VideoReview = clone(o.VideoReview)
			}
			if q.Narration == o.Narration {
				q.Voice = o.Voice
			}
		}
	}
}
func shotNeedsGeneration(q Shot) bool {
	if q.Stale || len(q.Candidates) < 2 || q.Image == "" {
		return true
	}
	for _, c := range q.Candidates {
		if c.Image == "" || c.Review.State == "" || c.Review.State == "incomplete" {
			return true
		}
	}
	return false
}
func videoIntent(b Board, q Shot, kind string) string {
	if b.Schema < 2 {
		if kind == "review" {
			return "legacy-video-review:" + q.ID + ":" + hash([]any{q.Video, q.Image, q.VideoPrompt})
		}
		return ""
	}
	if kind == "tts" {
		return "tts:" + q.ID + ":" + hash([]any{q.Narration, q.Duration})
	}
	c := selectedCandidate(q)
	if c == nil {
		return ""
	}
	generation := ""
	if kind != "tts" {
		generation = q.VideoGenerationID
	}
	return "video:" + q.ID + ":" + hash([]any{kind, c.ID, c.Hash, q.VideoPrompt, q.Narration, q.Duration, generation})
}
func plannedVideoReviewRefs(b Board, q Shot) []string {
	refs := []string{q.Image}
	for _, id := range q.Characters {
		for _, c := range b.Characters {
			if c.ID == id {
				if view, ok := canonicalView(c, q.Camera.View); ok {
					refs = append(refs, view.Image)
				} else if c.Image != "" {
					refs = append(refs, c.Image)
				}
			}
		}
	}
	for i, shot := range b.Shots {
		if shot.ID == q.ID && i > 0 {
			refs = append(refs, b.Shots[i-1].Image)
			break
		}
	}
	return refs
}
func (s *Studio) extractReviewFrames(ctx context.Context, pid, file string) ([]string, error) {
	info, e := s.probe(ctx, s.abs(file))
	if e != nil {
		return nil, e
	}
	duration, e := strconv.ParseFloat(info.Format.Duration, 64)
	if e != nil || duration <= 0 {
		return nil, errors.New("视频时长无效")
	}
	digest, e := fileHash(s.abs(file))
	if e != nil {
		return nil, e
	}
	dir := filepath.Join(pid, "video-review", digest[:20])
	if e = os.MkdirAll(s.abs(dir), 0700); e != nil {
		return nil, e
	}
	files := []string{}
	for i, t := range []float64{0, duration / 2, max(0, duration-.12)} {
		rel := filepath.Join(dir, fmt.Sprintf("frame-%d.png", i))
		if _, e = os.Stat(s.abs(rel)); e != nil {
			if _, e = command(ctx, "", "ffmpeg", "-v", "error", "-y", "-ss", fmt.Sprintf("%.6f", t), "-i", s.abs(file), "-frames:v", "1", s.abs(rel)); e != nil {
				return nil, e
			}
		}
		files = append(files, rel)
	}
	return files, nil
}
func (s *Studio) reviewVideo(ctx context.Context, pid string, b Board, q Shot, file string, settings Settings) (Review, error) {
	if reviewOK(q.VideoReview) || q.VideoReview.State == "failed" {
		return q.VideoReview, nil
	}
	frames, e := s.extractReviewFrames(ctx, pid, file)
	if e != nil {
		return Review{State: "incomplete"}, e
	}
	refs := []ReferenceUse{}
	add := func(file, purpose string) error {
		digest, err := fileHash(s.abs(file))
		if err != nil {
			return err
		}
		refs = append(refs, ReferenceUse{File: file, Hash: digest, Purpose: purpose})
		return nil
	}
	for _, f := range frames[1:] {
		if e = add(f, "当前视频的中间或结束帧，按输入顺序检查动作和身份变化"); e != nil {
			return Review{}, e
		}
	}
	for i, f := range plannedVideoReviewRefs(b, q) {
		purpose := "已审核分镜或角色参考"
		if i == len(plannedVideoReviewRefs(b, q))-1 {
			for j, shot := range b.Shots {
				if shot.ID == q.ID && j > 0 && b.Shots[j-1].Video != "" {
					previous, err := s.extractReviewFrames(ctx, pid, b.Shots[j-1].Video)
					if err != nil {
						return Review{}, err
					}
					f = previous[len(previous)-1]
					purpose = "前一个镜头的真实结束帧，核对方向与动作衔接"
				}
			}
		}
		if e = add(f, purpose); e != nil {
			return Review{}, e
		}
	}
	result, e := s.reviewImages(ctx, pid, settings, videoIntent(b, q, "review"), "第一张是当前视频起始帧，随后为中间与结束帧、已审核参考及前镜结束帧。检查身份漂移、动作异常、道具状态、相邻镜头方向。"+q.VideoPrompt, []string{"video-" + q.ID}, frames[:1], refs)
	if e != nil {
		return Review{State: "incomplete", Issues: []string{e.Error()}}, e
	}
	return result["video-"+q.ID], nil
}

func (s *Studio) validateBoardMedia(b Board) error {
	for _, q := range b.Shots {
		if c := selectedCandidate(q); c != nil {
			digest, e := fileHash(s.abs(c.Image))
			if e != nil || c.Hash != "" && digest != c.Hash {
				return fmt.Errorf("镜头%s候选图片内容已变化或缺失", q.ID)
			}
			if e = s.hashReferences(c.References); e != nil {
				return e
			}
		}
	}
	return nil
}

func prepareFillTarget(b *Board, target string) error {
	if target == "" {
		return nil
	}
	if strings.HasPrefix(target, "asset:") {
		id := strings.TrimPrefix(target, "asset:")
		found := false
		for _, c := range b.Characters {
			found = found || c.ID == id
		}
		for _, a := range append(append([]SceneAsset{}, b.Scenes...), b.Props...) {
			found = found || a.ID == id
		}
		if !found {
			return errors.New("指定参考资产不存在")
		}
		invalidateAssets(b, []string{id}, false)
		return nil
	}
	found := false
	for _, q := range b.Shots {
		found = found || q.ID == target
	}
	if !found {
		return errors.New("指定镜头不存在")
	}
	affected := dependentShots(*b, []string{target})
	for i := range b.Shots {
		if slices.Contains(affected, b.Shots[i].ID) {
			b.Shots[i].Stale = true
		}
	}
	return nil
}
