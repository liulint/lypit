package main

import (
	"errors"
	"fmt"
)

func (s *Studio) quoteAssetStage(p Project, settings Settings, q *Quote) error {
	q.Schema = 3
	q.Scope = map[string]int{}
	q.RefHashes = map[string]string{}
	add := func(role, kind, prompt string, refs []string) error {
		config := imageSettings(settings, role)
		if e := s.ready(config, kind); e != nil {
			return e
		}
		pr, m, e := selectModel(config, kind)
		if e != nil {
			return e
		}
		if e = checkReferences(m, refs); e != nil {
			return e
		}
		if kind == "image" && len(refs) > 0 && m.ImageEdit != nil && !*m.ImageEdit {
			return errors.New("生图模型未声明图片编辑能力，不能使用参考图")
		}
		var price *float64
		if pr.Adapter == "mock" {
			z := 0.
			price = &z
		} else if pr.Adapter != "cli" && m.Price != nil && m.Unit == "request" {
			v := *m.Price
			price = &v
		}
		for _, f := range refs {
			digest, e := fileHash(s.abs(f))
			if e != nil {
				return errors.New("引用素材缺失")
			}
			q.RefHashes[f] = digest
		}
		q.Scope[requestRole(kind, role)]++
		q.Requests = append(q.Requests, RequestSpec{Role: requestRole(kind, role), Kind: kind, Provider: pr.Name + " (" + pr.ID + ")", Model: m.ID, Prompt: prompt, References: refs, Count: 1, Estimate: price, Parameters: map[string]any{"cli_options": m.CLIOptions, "sizes": m.Sizes, "max_references": m.MaxReferences, "price": m.Price, "unit": m.Unit}})
		return nil
	}
	if a := assets(p); a != nil {
		q.AssetHash = hash(a)
	}
	if q.Stage == "asset_plan" {
		if assets(p) != nil {
			return errors.New("已有素材清单，请编辑现有清单或导入历史素材，避免覆盖已确认图片")
		}
		q.Notice = "仅调用一次文本模型提取故事概要、角色、场景和关键道具，不生成图片或分镜。随后可编辑清单，再按准确数量确认生图费用。"
		return add("", "text", assetPlanPrompt(p), nil)
	}
	a := assets(p)
	if a == nil {
		return errors.New("请先分析故事，确认资产清单")
	}
	if e := validateAssetDefinitions(*a, p); e != nil {
		return e
	}
	if q.ShotID != "" {
		if e := resetAsset(a, q.ShotID); e != nil {
			return e
		}
	}
	if q.Stage == "reference_assets" {
		if e := s.validateAssetFiles(*a, false); e != nil {
			return e
		}
	}
	ids, e := assetJobs(*a, q.Stage, q.ShotID)
	if e != nil {
		return e
	}
	if len(ids) == 0 {
		return errors.New("没有待生成的素材，请人工确认已有图片；重做请指定单张素材")
	}
	for _, id := range ids {
		for _, i := range assetItems(a) {
			if i.ID != id {
				continue
			}
			if *i.Image == "" {
				if e = add(i.Role, "image", i.Prompt, i.Refs); e != nil {
					return e
				}
			}
			// The generated result is one additional image in the review request.
			_, visionModel, err := selectModel(settings, "vision")
			if err != nil {
				return err
			}
			if !visionModel.Structured || !visionModel.ReferenceImages || visionModel.MaxReferences < len(i.Refs)+1 {
				return errors.New("视觉审核模型无法接收素材及全部参考图片")
			}
			if e = add("", "vision", "审核素材 "+id+"："+i.Prompt, i.Refs); e != nil {
				return e
			}
		}
	}
	q.Notice = "本次仅制作以上参考素材及对应模型审核；每项最多一张，不自动重试或扩展任务。模型审核后仍需人工确认。不会生成分镜或视频。"
	if q.Stage == "character_masters" {
		q.Notice += "先确认正面主图，再生成侧面、背面和面部近景。"
	}
	if q.ShotID != "" {
		q.Notice += fmt.Sprintf(" 指定重做：%s；相关分镜将标记待更新：%v。历史图片保留。", q.ShotID, assetImpact(p, []string{assetRoot(q.ShotID)}))
	}
	return nil
}
func (s *Studio) checkQuoteReferences(q Quote) error {
	for file, digest := range q.RefHashes {
		actual, e := fileHash(s.abs(file))
		if e != nil || actual != digest {
			return errors.New("报价引用图片已变化或缺失，请重新报价")
		}
	}
	return nil
}
func applyAssetSnapshot(b *Board, a AssetSet) {
	b.Schema = 3
	b.AssetHash = hash(a)
	b.Characters = clone(a.Characters)
	b.Scenes = clone(a.Scenes)
	b.Props = clone(a.Props)
	b.Visual = a.Visual
	b.Story = a.Story
	b.ReferenceHash = hash(a)
	b.SourceMappings = nil
}
func assetPhase(stage string) bool {
	return stage == "asset_plan" || stage == "character_masters" || stage == "reference_assets"
}
func shotOnlyScope(b *Board) map[string]int {
	shots := 12
	if b != nil {
		shots = len(b.Shots)
	}
	return map[string]int{"text": 1, "storyboard_image": shots * 3, "vision": shots * 2, "_repair": shots, "_vision_conditional": shots, "_asset_locked": 1}
}
func (s *Studio) quoteLockedAssets(p Project, q *Quote) error {
	a, e := s.lockedAssets(p)
	if e != nil {
		return e
	}
	q.Schema = 3
	q.AssetHash = hash(a)
	q.RefHashes = map[string]string{}
	for _, i := range assetItems(a) {
		q.RefHashes[*i.Image] = *i.Hash
		for _, ref := range *i.StoredRefs {
			q.RefHashes[ref.File] = ref.Hash
		}
	}
	return nil
}
func (s *Studio) assertBoardAssetSnapshot(p Project, b Board) error {
	if b.Schema < 3 && assets(p) == nil {
		return nil
	}
	a, e := s.lockedAssets(p)
	if e != nil {
		return e
	}
	if b.AssetHash != hash(a) {
		return errors.New("参考素材版本与分镜不一致，请更新分镜后再生成视频")
	}
	return nil
}
