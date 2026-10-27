package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
)

type AssetApproval struct {
	Hash   string `json:"hash"`
	Reason string `json:"reason"`
	At     string `json:"at"`
}
type AssetSet struct {
	Version    int                      `json:"version"`
	WorkQuote  string                   `json:"work_quote"`
	Visual     VisualPlan               `json:"visual"`
	Story      StoryPlan                `json:"story"`
	Characters []Character              `json:"characters"`
	Scenes     []SceneAsset             `json:"scenes"`
	Props      []SceneAsset             `json:"props"`
	Approvals  map[string]AssetApproval `json:"approvals"`
	Intents    map[string]string        `json:"intents"`
	Locked     bool                     `json:"locked"`
}

func assets(p Project) *AssetSet {
	if len(p.AssetSets) == 0 {
		return nil
	}
	a := clone(p.AssetSets[len(p.AssetSets)-1])
	return &a
}

var characterViews = []string{"front", "side", "back", "close"}

func assetBoard(a AssetSet) Board {
	return Board{Schema: 3, Visual: a.Visual, Story: a.Story, Characters: clone(a.Characters), Scenes: clone(a.Scenes), Props: clone(a.Props)}
}
func requiredViews(b Board) []string {
	if b.Schema >= 3 {
		return characterViews
	}
	return []string{"front", "side", "close"}
}
func assetDefinitions(a AssetSet) any {
	chars := []any{}
	for _, c := range a.Characters {
		chars = append(chars, []any{c.ID, c.Name, c.Description, c.FixedFeatures, c.Sources})
	}
	scenes := []any{}
	props := []any{}
	for _, v := range a.Scenes {
		scenes = append(scenes, []any{v.ID, v.Name, v.Description, v.Sources})
	}
	for _, v := range a.Props {
		props = append(props, []any{v.ID, v.Name, v.Description, v.Sources})
	}
	return []any{a.Visual, a.Story, chars, scenes, props}
}
func validateAssetDefinitions(a AssetSet, p Project) error {
	if len(a.Characters) < 1 || len(a.Characters) > 3 || len(a.Scenes) < 1 || len(a.Scenes) > 3 || len(a.Props) > 2 {
		return errors.New("需要1～3个角色、1～3个场景和最多2个关键道具")
	}
	if strings.TrimSpace(a.Visual.Style) == "" || strings.TrimSpace(a.Story.Synopsis) == "" {
		return errors.New("请填写故事概要和视觉风格")
	}
	ids := map[string]bool{}
	check := func(id, name, desc string, sources []string) error {
		if !nameRE.MatchString(id) || ids[id] || strings.TrimSpace(name) == "" || strings.TrimSpace(desc) == "" || len(desc) > 10000 {
			return errors.New("资产标识、名称或描述无效或重复")
		}
		ids[id] = true
		seen := map[string]bool{}
		for _, f := range sources {
			if !slices.Contains(p.References, f) || seen[f] {
				return errors.New("仅可关联本项目已上传的参考图，不能重复")
			}
			seen[f] = true
		}
		return nil
	}
	for _, c := range a.Characters {
		if e := check(c.ID, c.Name, c.Description, c.Sources); e != nil {
			return e
		}
	}
	for _, v := range append(append([]SceneAsset{}, a.Scenes...), a.Props...) {
		if e := check(v.ID, v.Name, v.Description, v.Sources); e != nil {
			return e
		}
	}
	return nil
}
func cleanAssetSet(input AssetSet, old *AssetSet, p Project) (AssetSet, []string, error) {
	a := AssetSet{Characters: []Character{}, Scenes: []SceneAsset{}, Props: []SceneAsset{}, Visual: visualDefaults(input.Visual, p.Idea), Story: input.Story, Version: 1, Approvals: map[string]AssetApproval{}, Intents: map[string]string{}}
	if old != nil {
		a.Version = old.Version + 1
	}
	for _, v := range input.Characters {
		a.Characters = append(a.Characters, Character{ID: v.ID, Name: v.Name, Description: v.Description, FixedFeatures: v.FixedFeatures, Sources: slices.Clone(v.Sources)})
	}
	for _, group := range []struct {
		in  []SceneAsset
		out *[]SceneAsset
	}{{input.Scenes, &a.Scenes}, {input.Props, &a.Props}} {
		for _, v := range group.in {
			*group.out = append(*group.out, SceneAsset{ID: v.ID, Name: v.Name, Description: v.Description, Sources: slices.Clone(v.Sources)})
		}
	}
	if e := validateAssetDefinitions(a, p); e != nil {
		return a, nil, e
	}
	changed := []string{}
	sameStyle := old != nil && hash(a.Visual) == hash(old.Visual)
	for i := range a.Characters {
		c := &a.Characters[i]
		stable := false
		if sameStyle {
			for _, o := range old.Characters {
				if hash([]any{c.ID, c.Name, c.Description, c.FixedFeatures, c.Sources}) == hash([]any{o.ID, o.Name, o.Description, o.FixedFeatures, o.Sources}) {
					*c = clone(o)
					stable = true
				}
			}
		}
		if !stable {
			changed = append(changed, c.ID)
		}
		for _, kind := range characterViews {
			found := false
			for _, v := range c.Views {
				found = found || v.Kind == kind
			}
			if !found {
				c.Views = append(c.Views, ReferenceView{ID: c.ID + "-" + kind, Kind: kind})
			}
		}
	}
	for _, group := range []struct {
		current  *[]SceneAsset
		previous []SceneAsset
	}{{&a.Scenes, func() []SceneAsset {
		if old != nil {
			return old.Scenes
		}
		return nil
	}()}, {&a.Props, func() []SceneAsset {
		if old != nil {
			return old.Props
		}
		return nil
	}()}} {
		for i := range *group.current {
			v := &(*group.current)[i]
			stable := false
			if sameStyle {
				for _, o := range group.previous {
					if hash([]any{v.ID, v.Name, v.Description, v.Sources}) == hash([]any{o.ID, o.Name, o.Description, o.Sources}) {
						*v = clone(o)
						stable = true
					}
				}
			}
			if !stable {
				changed = append(changed, v.ID)
			}
		}
	}
	if old != nil {
		a.Intents = clone(old.Intents)
		if a.Intents == nil {
			a.Intents = map[string]string{}
		}
		previous := map[string]string{}
		for _, i := range assetItems(old) {
			previous[i.ID] = i.fingerprint(old.Visual)
		}
		for _, item := range assetItems(&a) {
			fingerprint := item.fingerprint(a.Visual)
			if approval, ok := old.Approvals[item.ID]; ok && approval.Hash == fingerprint {
				a.Approvals[item.ID] = approval
			}
			if previous[item.ID] != fingerprint {
				delete(a.Intents, item.ID)
			}
		}
		oldIDs := []string{}
		for _, c := range old.Characters {
			oldIDs = append(oldIDs, c.ID)
		}
		for _, v := range append(append([]SceneAsset{}, old.Scenes...), old.Props...) {
			oldIDs = append(oldIDs, v.ID)
		}
		for _, id := range oldIDs {
			found := false
			for _, c := range a.Characters {
				found = found || c.ID == id
			}
			for _, v := range append(append([]SceneAsset{}, a.Scenes...), a.Props...) {
				found = found || v.ID == id
			}
			if !found {
				changed = append(changed, id)
			}
		}
	}
	return a, changed, nil
}

type assetItem struct {
	ID, Role, Prompt string
	Image, Hash      *string
	Review           *Review
	StoredPrompt     *string
	StoredRefs       *[]ReferenceUse
	Refs             []string
}

func (i assetItem) fingerprint(v VisualPlan) string {
	return hash([]any{i.ID, v, i.Prompt, i.Refs, *i.Image, *i.Hash, *i.StoredRefs})
}
func assetItems(a *AssetSet) []assetItem {
	items := []assetItem{}
	for ci := range a.Characters {
		c := &a.Characters[ci]
		for vi := range c.Views {
			v := &c.Views[vi]
			direction := map[string]string{"front": "正面全身，清晰呈现脸部与完整服装", "side": "侧面全身视角，保持主图身份与服装", "back": "背面全身视角，清晰呈现背部服装和轮廓，不回头", "close": "面部近景，保留可识别特征"}[v.Kind]
			refs := slices.Clone(c.Sources)
			if v.Kind != "front" {
				for _, front := range c.Views {
					if front.Kind == "front" && front.Image != "" {
						refs = append([]string{front.Image}, refs...)
					}
				}
			}
			items = append(items, assetItem{ID: c.ID + ":" + v.Kind, Role: "character_image", Prompt: characterPrompt(*c) + "\n风格：" + a.Visual.Style + "。固定特征：" + c.FixedFeatures + "。" + direction + "。输入参考决定身份，一张独立图片，不要拼贴。", Image: &v.Image, Hash: &v.Hash, Review: &v.Review, StoredPrompt: &v.Prompt, StoredRefs: &v.References, Refs: refs})
		}
	}
	for _, group := range []*[]SceneAsset{&a.Scenes, &a.Props} {
		for i := range *group {
			v := &(*group)[i]
			items = append(items, assetItem{ID: v.ID, Role: "storyboard_image", Prompt: "完整参考画面，" + a.Visual.Style + "。" + a.Visual.Photography + "。" + v.Name + "：" + v.Description + "。明确形态、空间与光线，不要字幕、水印或拼贴。", Image: &v.Image, Hash: &v.Hash, Review: &v.Review, StoredPrompt: &v.Prompt, StoredRefs: &v.References, Refs: slices.Clone(v.Sources)})
		}
	}
	return items
}
func assetApproved(a AssetSet, i assetItem) bool {
	return *i.Image != "" && reviewOK(*i.Review) && a.Approvals[i.ID].Hash == i.fingerprint(a.Visual)
}
func validateMasters(a AssetSet) error {
	for _, i := range assetItems(&a) {
		if strings.HasSuffix(i.ID, ":front") && !assetApproved(a, i) {
			return errors.New("请先人工确认全部角色正面主图")
		}
	}
	return nil
}
func (s *Studio) validateAssetFiles(a AssetSet, all bool) error {
	for _, i := range assetItems(&a) {
		if !all && !strings.HasSuffix(i.ID, ":front") {
			continue
		}
		if !assetApproved(a, i) {
			return fmt.Errorf("参考素材 %s 尚未人工确认", i.ID)
		}
		digest, e := fileHash(s.abs(*i.Image))
		if e != nil || digest != *i.Hash {
			return errors.New("参考素材内容已变化或缺失，请重新处理")
		}
		if e = s.hashReferences(*i.StoredRefs); e != nil {
			return e
		}
	}
	return nil
}
func (s *Studio) lockedAssets(p Project) (*AssetSet, error) {
	a := assets(p)
	if a == nil || !a.Locked {
		return nil, errors.New("请先确认角色主图、完整参考素材并锁定")
	}
	if e := s.validateAssetFiles(*a, true); e != nil {
		return nil, e
	}
	return a, nil
}
func resetAsset(a *AssetSet, target string) error {
	found := false
	for ci := range a.Characters {
		c := &a.Characters[ci]
		if target == c.ID+":front" {
			c.Image = ""
			for vi := range c.Views {
				v := &c.Views[vi]
				*v = ReferenceView{ID: v.ID, Kind: v.Kind}
				delete(a.Approvals, c.ID+":"+v.Kind)
				delete(a.Intents, c.ID+":"+v.Kind)
			}
			found = true
			break
		}
	}
	if !found {
		for _, i := range assetItems(a) {
			if i.ID == target {
				*i.Image = ""
				*i.Hash = ""
				*i.Review = Review{}
				*i.StoredRefs = nil
				*i.StoredPrompt = ""
				delete(a.Approvals, i.ID)
				delete(a.Intents, i.ID)
				found = true
			}
		}
	}
	a.Locked = false
	if !found {
		return errors.New("指定素材不存在")
	}
	return nil
}
func assetImpact(p Project, roots []string) []string {
	b := board(p)
	if b == nil {
		return []string{}
	}
	shots := []string{}
	for _, q := range b.Shots {
		hit := slices.Contains(roots, q.SceneID) || slices.Contains(roots, q.PropID) || slices.Contains(roots, "*")
		for _, id := range q.Characters {
			hit = hit || slices.Contains(roots, id)
		}
		if hit {
			shots = append(shots, q.ID)
		}
	}
	return dependentShots(*b, shots)
}
func invalidateAssetShots(p *Project, roots []string) {
	affected := assetImpact(*p, roots)
	if len(affected) == 0 {
		return
	}
	b := cloneBoardVersion(p)
	for i := range b.Shots {
		if slices.Contains(affected, b.Shots[i].ID) {
			q := &b.Shots[i]
			q.Stale = true
			q.Video = ""
			q.VideoReview = Review{}
			q.Warning = "参考素材已更新，请重新生成受影响镜头"
		}
	}
}
func (s *Studio) saveAssets(pid string, a AssetSet) error {
	return s.store.update(pid, func(p *Project) {
		for i := range p.AssetSets {
			if p.AssetSets[i].Version == a.Version {
				p.AssetSets[i] = clone(a)
				return
			}
		}
	})
}

const assetDirector = `你是短片前期美术策划，只输出严格JSON，不规划镜头、不生成图片。结构：{"story":{"synopsis":"故事概要","relationships":"人物关系","emotion":"情绪","beats":["开场","发展","转折","结尾"]},"visual":{"style":"沿用用户风格","photography":"摄影","palette":"色调"},"characters":[{"id":"c1","name":"角色","description":"具体外观服装","fixed_features":"固定识别特征","sources":[]}],"scenes":[{"id":"scene1","name":"场景","description":"空间布局、光线与固定物件","sources":[]}],"props":[{"id":"prop1","name":"关键道具","description":"形态和颜色","sources":[]}]}。1～3角色，1～3场景，0～2关键道具；所有id唯一且为字母开头的字母数字下划线。不要返回图片、路径、审核或批准字段。sources留空，用户随后手动关联已有图片。尊重故事，不新增无关角色或地点。`

func (s *Studio) planAssets(ctx context.Context, p Project, settings Settings) error {
	if a := assets(p); a != nil && a.WorkQuote == p.ActiveQuote {
		return nil
	}
	var input AssetSet
	pr, _, _ := selectModel(settings, "text")
	if pr.Adapter == "mock" {
		b := mockBoard(p, "", "")
		enrichMockBoard(&b, visualDefaults(p.Visual, p.Idea))
		input = AssetSet{Visual: b.Visual, Story: StoryPlan{Synopsis: p.Idea, Emotion: "温暖", Beats: []string{"开始", "结束"}}, Characters: b.Characters, Scenes: b.Scenes, Props: b.Props}
	} else {
		file, e := s.scopedRequest(ctx, p.ID, settings, "text", "", p.ActiveQuote+":asset-plan", assetPlanPrompt(p), nil, 0)
		if e != nil {
			return e
		}
		raw, e := os.ReadFile(s.abs(file))
		if e != nil {
			return e
		}
		if json.Unmarshal(raw, &input) != nil {
			return errors.New("资产规划JSON无效，响应已保存")
		}
	}
	a, _, e := cleanAssetSet(input, nil, p)
	if e != nil {
		return e
	}
	if old := assets(p); old != nil {
		a.Version = old.Version + 1
	}
	a.WorkQuote = p.ActiveQuote
	return s.store.update(p.ID, func(p *Project) {
		invalidateAssetShots(p, []string{"*"})
		p.ActiveQuote = a.WorkQuote
		p.Status = "running"
		p.AssetWorkflow = true
		p.AssetSets = append(p.AssetSets, clone(a))
		p.Visual = a.Visual
		p.Revision++
	})
}
func (s *Studio) runAssetStage(ctx context.Context, p Project, settings Settings, stage, target string) error {
	if stage == "asset_plan" {
		return s.step(ctx, p.ID, "planning", "分析故事，提取角色、场景与道具", func() error { return s.planAssets(ctx, p, settings) })
	}
	a := assets(p)
	if a == nil {
		return errors.New("请先分析故事")
	}
	if a.WorkQuote != p.ActiveQuote {
		next := clone(*a)
		next.Version++
		next.WorkQuote = p.ActiveQuote
		next.Locked = false
		if target != "" {
			if e := resetAsset(&next, target); e != nil {
				return e
			}
		}
		if e := s.store.update(p.ID, func(p *Project) {
			if target != "" {
				invalidateAssetShots(p, []string{strings.Split(target, ":")[0]})
			}
			p.ActiveQuote = next.WorkQuote
			p.Status = "running"
			p.AssetSets = append(p.AssetSets, clone(next))
			p.Revision++
		}); e != nil {
			return e
		}
		a = &next
	}
	jobs, e := assetJobs(*a, stage, target)
	if e != nil {
		return e
	}
	for _, id := range jobs {
		if e = ctx.Err(); e != nil {
			return e
		}
		var item assetItem
		for _, v := range assetItems(a) {
			if v.ID == id {
				item = v
				break
			}
		}
		if a.Intents == nil {
			a.Intents = map[string]string{}
		}
		if a.Intents[id] == "" {
			a.Intents[id] = p.ActiveQuote + ":" + id
		}
		if e = s.saveAssets(p.ID, *a); e != nil {
			return e
		}
		e = s.step(ctx, p.ID, "image", "制作参考素材："+id, func() error {
			if *item.Image == "" {
				refs := []ReferenceUse{}
				for _, f := range item.Refs {
					digest, err := fileHash(s.abs(f))
					if err != nil {
						return err
					}
					refs = append(refs, ReferenceUse{File: f, Hash: digest, Purpose: "实际素材参考"})
				}
				*item.StoredRefs = refs
				*item.StoredPrompt = item.Prompt
				file, err := s.scopedRequest(ctx, p.ID, settings, "image", item.Role, a.Intents[id], item.Prompt, item.Refs, 0)
				if err != nil {
					return err
				}
				*item.Image = file
				*item.Hash, err = fileHash(s.abs(file))
				if err != nil {
					return err
				}
				if strings.HasSuffix(id, ":front") {
					for ci := range a.Characters {
						if a.Characters[ci].ID == strings.Split(id, ":")[0] {
							a.Characters[ci].Image = file
						}
					}
				}
				if err = s.saveAssets(p.ID, *a); err != nil {
					return err
				}
			}
			reviewed, err := s.reviewImages(ctx, p.ID, settings, a.Intents[id]+":review", item.Prompt, []string{id}, []string{*item.Image}, *item.StoredRefs)
			if err != nil {
				*item.Review = Review{State: "incomplete", Reason: "审核未完成：" + err.Error()}
			} else {
				*item.Review = reviewed[id]
			}
			if saveErr := s.saveAssets(p.ID, *a); saveErr != nil {
				return saveErr
			}
			return err
		})
		if e != nil {
			return e
		}
	}
	return nil
}
func assetJobs(a AssetSet, stage, target string) ([]string, error) {
	if stage != "character_masters" && stage != "reference_assets" {
		return nil, errors.New("未知素材阶段")
	}
	if stage == "reference_assets" {
		if e := validateMasters(a); e != nil {
			return nil, e
		}
	}
	jobs := []string{}
	for _, i := range assetItems(&a) {
		front := strings.HasSuffix(i.ID, ":front")
		if front != (stage == "character_masters") {
			continue
		}
		if target != "" && target != i.ID {
			continue
		}
		if *i.Image == "" || i.Review.State == "" || i.Review.State == "incomplete" {
			jobs = append(jobs, i.ID)
		}
	}
	return jobs, nil
}

func assetPlanPrompt(p Project) string {
	payload, _ := json.Marshal(map[string]any{"idea": p.Idea, "visual": p.Visual})
	return assetDirector + "\n" + string(payload)
}
func assetShotPrompt(p Project, a AssetSet, instruction, shotID string) string {
	raw, _ := json.Marshal(map[string]any{"locked_assets": assetDefinitions(a), "instruction": instruction, "target_shot": shotID, "previous": board(p)})
	return director + strings.ReplaceAll(shotSchema, "camera.view限定front、side、close", "camera.view限定front、side、back、close") + "\n仅使用下列已人工确认的角色、场景、道具标识与设定。不要增删或改写资产；缺少必要资产时输出 {\"needs_assets\":\"缺少的素材及原因\"}。camera.view可用front、side、back、close。\n" + string(raw)
}
func (s *Studio) planAssetBoard(ctx context.Context, p Project, settings Settings, instruction, shotID string) (Board, error) {
	a, e := s.lockedAssets(p)
	if e != nil {
		return Board{}, e
	}
	var b Board
	pr, _, _ := selectModel(settings, "text")
	if pr.Adapter == "mock" {
		b = mockBoard(p, instruction, shotID)
		enrichMockBoard(&b, a.Visual)
		for i := range b.Shots {
			q := &b.Shots[i]
			q.Characters = []string{a.Characters[0].ID}
			q.SceneID = a.Scenes[i%len(a.Scenes)].ID
			q.PropID = ""
			if len(a.Props) > 0 {
				q.PropID = a.Props[0].ID
			}
			q.Camera.View = characterViews[i%len(characterViews)]
		}
	} else {
		file, err := s.scopedRequest(ctx, p.ID, settings, "text", "", p.ActiveQuote+":shots", assetShotPrompt(p, *a, instruction, shotID), nil, 0)
		if err != nil {
			return b, err
		}
		raw, err := os.ReadFile(s.abs(file))
		if err != nil {
			return b, err
		}
		var need struct {
			Needs string `json:"needs_assets"`
		}
		_ = json.Unmarshal(raw, &need)
		if need.Needs != "" {
			return b, errors.New("需要新增参考素材，请返回素材准备阶段：" + need.Needs)
		}
		if json.Unmarshal(raw, &b) != nil {
			return b, errors.New("分镜导演JSON无效，响应已保存")
		}
	}
	if shotID != "" {
		old := board(p)
		if old == nil {
			return b, errors.New("没有可修改的分镜")
		}
		next := clone(*old)
		found := false
		for _, q := range b.Shots {
			if q.ID == shotID {
				for i := range next.Shots {
					if next.Shots[i].ID == shotID {
						next.Shots[i] = q
						found = true
					}
				}
			}
		}
		if !found {
			return b, errors.New("指定镜头未返回")
		}
		b = next
	}
	applyAssetSnapshot(&b, *a)
	b.GenerationID = p.ActiveQuote
	b.Version = 1
	if old := board(p); old != nil {
		b.Version = old.Version + 1
	}
	for i := range b.Shots {
		q := &b.Shots[i]
		q.BaseVideoPrompt = q.VideoPrompt
		q.Image = ""
		q.Video = ""
		q.Voice = ""
		q.Candidates = nil
		q.Selected = ""
		q.Recommended = ""
		q.Stale = false
		q.VideoReview = Review{}
	}
	if e = normalizeBoard(&b); e != nil {
		return b, e
	}
	if e = validateExtendedBoard(b); e != nil {
		return b, e
	}
	if old := board(p); old != nil {
		preserveExtendedAssets(old, &b)
	}
	b.Approved = false
	return b, nil
}
