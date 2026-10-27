package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
)

func (s *Studio) classifySources(ctx context.Context, p Project, b *Board, settings Settings) error {
	if len(p.References) == 0 {
		return nil
	}
	mappings := clone(p.ReferenceMappings)
	if len(mappings) == 0 {
		pr, _, _ := selectModel(settings, "vision")
		if pr.Adapter == "mock" {
			for _, file := range p.References {
				mappings = append(mappings, SourceMapping{File: file, Kind: "character", Target: b.Characters[0].ID, Reason: "模拟分类，请人工核对用途"})
			}
		} else {
			raw, _ := json.Marshal(map[string]any{"files": p.References, "characters": b.Characters, "scenes": b.Scenes, "props": b.Props})
			prompt := "[LYPIT_CLASSIFY]\n" + string(raw) + "\n逐张判断用户参考图用途，输出JSON {\"mappings\":[{\"file\":\"原始文件值\",\"kind\":\"character或scene或prop或style\",\"target\":\"对应现有资产ID，style时留空\",\"reason\":\"关联理由\"}]}。所有文件必须各出现一次，勿编造新路径或ID。"
			file, e := s.scopedRequest(ctx, p.ID, settings, "vision", "", imageIntent(*b, "classify"), prompt, p.References, 0)
			if e != nil {
				return e
			}
			bytes, e := os.ReadFile(s.abs(file))
			if e != nil {
				return e
			}
			var result struct {
				Mappings []SourceMapping `json:"mappings"`
			}
			if json.Unmarshal(bytes, &result) != nil {
				return errors.New("参考图分类结果无效")
			}
			mappings = result.Mappings
		}
	}
	if e := validateMappings(p, *b, mappings); e != nil {
		return e
	}
	b.SourceMappings = mappings
	return nil
}
func validateMappings(p Project, b Board, mappings []SourceMapping) error {
	seen := map[string]bool{}
	for _, m := range mappings {
		if !slices.Contains(p.References, m.File) || seen[m.File] {
			return errors.New("参考图用途必须匹配已上传文件且不能重复")
		}
		valid := m.Kind == "style" && m.Target == ""
		if m.Kind == "character" {
			for _, c := range b.Characters {
				valid = valid || c.ID == m.Target
			}
		}
		if m.Kind == "scene" {
			for _, a := range b.Scenes {
				valid = valid || a.ID == m.Target
			}
		}
		if m.Kind == "prop" {
			for _, a := range b.Props {
				valid = valid || a.ID == m.Target
			}
		}
		if !valid {
			return errors.New("参考图关联的角色、场景或道具不存在")
		}
		seen[m.File] = true
	}
	if len(seen) != len(p.References) {
		return errors.New("仍有参考图用途未明确，不能静默忽略")
	}
	return nil
}
func sourceFiles(b Board, kind, id string) []string {
	out := []string{}
	for _, m := range b.SourceMappings {
		if m.Kind == "style" || m.Kind == kind && m.Target == id {
			out = append(out, m.File)
		}
	}
	return out
}
func (s *Studio) assetReview(ctx context.Context, pid string, b Board, settings Settings, id, prompt, image string, files []string) (Review, error) {
	refs := []ReferenceUse{}
	for _, file := range files {
		digest, e := fileHash(s.abs(file))
		if e != nil {
			return Review{}, e
		}
		refs = append(refs, ReferenceUse{AssetID: "source", File: file, Hash: digest, Purpose: "继承已建立资产的外观、空间或风格"})
	}
	reviews, e := s.reviewImages(ctx, pid, settings, imageIntent(b, "review:"+id), prompt, []string{id}, []string{image}, refs)
	if e != nil {
		return Review{State: "incomplete", Issues: []string{e.Error()}}, e
	}
	r := reviews[id]
	if !reviewOK(r) {
		return r, fmt.Errorf("参考资产%s未通过审核：%s", id, r.Reason)
	}
	return r, nil
}
func (s *Studio) generateAssets(ctx context.Context, pid string, b *Board, settings Settings) error {
	for i := range b.Characters {
		c := &b.Characters[i]
		c.Sources = sourceFiles(*b, "character", c.ID)
		for _, kind := range []string{"front", "side", "close"} {
			index := -1
			for j := range c.Views {
				if c.Views[j].Kind == kind {
					index = j
				}
			}
			if index < 0 {
				c.Views = append(c.Views, ReferenceView{ID: c.ID + "-" + kind, Kind: kind})
				index = len(c.Views) - 1
			}
			view := &c.Views[index]
			if view.Image != "" && reviewOK(view.Review) {
				if kind == "front" {
					c.Image = view.Image
				}
				continue
			}
			err := s.step(ctx, pid, "character", "建立角色视图："+c.Name+" / "+kind, func() error {
				refs := slices.Clone(c.Sources)
				direction := "正面全身，清晰呈现脸部与完整服装"
				if kind != "front" {
					front, ok := canonicalView(*c, "front")
					if !ok {
						return errors.New("角色主图尚未审核通过")
					}
					refs = append([]string{front.Image}, refs...)
					if kind == "side" {
						direction = "侧面或四分之三全身视角"
					} else {
						direction = "面部近景，保留可识别特征"
					}
				}
				prompt := characterPrompt(*c) + "\n风格：" + b.Visual.Style + "。固定特征：" + c.FixedFeatures + "。" + direction + "。输入参考图决定身份，简洁背景，一张完整图片，不要拼贴。"
				if view.Image == "" {
					file, e := s.scopedRequest(ctx, pid, settings, "image", "character_image", imageIntent(*b, view.ID), prompt, refs, 0)
					if e != nil {
						return e
					}
					view.Image = file
					view.Hash, e = fileHash(s.abs(file))
					if e != nil {
						return e
					}
					if kind == "front" {
						c.Image = file
					}
					if e = s.saveBoard(pid, *b); e != nil {
						return e
					}
				}
				r, e := s.assetReview(ctx, pid, *b, settings, view.ID, prompt, view.Image, refs)
				view.Review = r
				if saveErr := s.saveBoard(pid, *b); saveErr != nil {
					return saveErr
				}
				return e
			})
			if err != nil {
				return err
			}
		}
	}
	for _, group := range []struct {
		kind   string
		assets *[]SceneAsset
	}{{"scene", &b.Scenes}, {"prop", &b.Props}} {
		for i := range *group.assets {
			a := &(*group.assets)[i]
			if a.Image != "" && reviewOK(a.Review) {
				continue
			}
			a.Sources = sourceFiles(*b, group.kind, a.ID)
			err := s.step(ctx, pid, group.kind, "建立参考资产："+a.Name, func() error {
				prompt := "完整参考画面，" + b.Visual.Style + "。" + b.Visual.Photography + "。" + a.Description + "。场景要明确空间与光线，道具要明确形态，不要字幕、水印或拼贴。"
				if a.Image == "" {
					file, e := s.scopedRequest(ctx, pid, settings, "image", "storyboard_image", imageIntent(*b, a.ID), prompt, a.Sources, 0)
					if e != nil {
						return e
					}
					a.Image = file
					a.Hash, e = fileHash(s.abs(file))
					if e != nil {
						return e
					}
					if e = s.saveBoard(pid, *b); e != nil {
						return e
					}
				}
				r, e := s.assetReview(ctx, pid, *b, settings, a.ID, prompt, a.Image, a.Sources)
				a.Review = r
				if se := s.saveBoard(pid, *b); se != nil {
					return se
				}
				return e
			})
			if err != nil {
				return err
			}
		}
	}
	return nil
}
func (s *Studio) generateShot(ctx context.Context, pid string, b Board, q Shot, settings Settings) (Shot, error) {
	refs, e := s.shotReferences(b, q)
	if e != nil {
		return q, e
	}
	if q.Stale {
		q.Candidates = nil
		q.Selected = ""
		q.Recommended = ""
		q.Image = ""
		q.Stale = false
	}
	for len(q.Candidates) < 2 {
		i := len(q.Candidates)
		c := Candidate{ID: fmt.Sprintf("%s-%s-%d", b.GenerationID, q.ID, i+1), Intent: imageIntent(b, fmt.Sprintf("%s:candidate:%d", q.ID, i+1)), Round: 1, Prompt: candidatePrompt(b, q, i, refs, ""), References: clone(refs)}
		c.VideoPrompt = candidateVideoPrompt(q, c)
		q.Candidates = append(q.Candidates, c)
	}
	save := func() error {
		return s.store.update(pid, func(p *Project) {
			for bi := range p.Boards {
				if p.Boards[bi].Version != b.Version {
					continue
				}
				for i := range p.Boards[bi].Shots {
					if p.Boards[bi].Shots[i].ID == q.ID {
						p.Boards[bi].Shots[i] = clone(q)
					}
				}
			}
		})
	}
	for i := 0; i < 2; i++ {
		c := &q.Candidates[i]
		if c.Image != "" {
			continue
		}
		file, err := s.scopedRequest(ctx, pid, settings, "image", "storyboard_image", c.Intent, c.Prompt, referenceFiles(c.References), 0)
		if err != nil {
			return q, err
		}
		c.Image = file
		c.Hash, err = fileHash(s.abs(file))
		if err != nil {
			return q, err
		}
		if err = save(); err != nil {
			return q, err
		}
	}
	if q.Candidates[0].Review.State == "" || q.Candidates[1].Review.State == "" || q.Candidates[0].Review.State == "incomplete" || q.Candidates[1].Review.State == "incomplete" {
		ids, images := []string{}, []string{}
		for _, c := range q.Candidates[:2] {
			ids = append(ids, c.ID)
			images = append(images, c.Image)
		}
		direction, _ := json.Marshal(map[string]any{"purpose": q.Purpose, "camera": q.Camera, "continuity": q.Continuity, "visual": b.Visual})
		results, err := s.reviewImages(ctx, pid, settings, imageIntent(b, q.ID+":compare"), q.ImagePrompt+"\n"+string(direction), ids, images, refs)
		if err != nil {
			for i := 0; i < 2; i++ {
				q.Candidates[i].Review = Review{State: "incomplete", Issues: []string{err.Error()}}
			}
			q.Warning = "审核未完成，不能视为通过"
			_ = save()
			return q, err
		}
		for i := 0; i < 2; i++ {
			q.Candidates[i].Review = results[q.Candidates[i].ID]
		}
		if err = save(); err != nil {
			return q, err
		}
	}
	selectBest(&q)
	good := false
	for _, c := range q.Candidates {
		good = good || reviewOK(c.Review)
	}
	if !good {
		if len(q.Candidates) < 3 {
			best := selectedCandidate(q)
			correction := ""
			if best != nil {
				correction = best.Review.Correction + "；" + strings.Join(best.Review.Issues, "；")
			}
			if strings.Trim(correction, "； ") == "" {
				q.Warning = "两张候选未通过且审核未提供具体修复建议，请人工处理"
				_ = save()
				return q, errors.New(q.Warning)
			}
			c := Candidate{ID: b.GenerationID + "-" + q.ID + "-repair", Intent: imageIntent(b, q.ID+":repair:1"), Round: 2, Prompt: candidatePrompt(b, q, 2, refs, correction), References: clone(refs)}
			c.VideoPrompt = candidateVideoPrompt(q, c)
			q.Candidates = append(q.Candidates, c)
			if e = save(); e != nil {
				return q, e
			}
		}
		c := &q.Candidates[2]
		if c.Image == "" {
			file, err := s.scopedRequest(ctx, pid, settings, "image", "storyboard_image", c.Intent, c.Prompt, referenceFiles(refs), 0)
			if err != nil {
				return q, err
			}
			c.Image = file
			c.Hash, err = fileHash(s.abs(file))
			if err != nil {
				return q, err
			}
			if err = save(); err != nil {
				return q, err
			}
		}
		if c.Review.State == "" || c.Review.State == "incomplete" {
			results, err := s.reviewImages(ctx, pid, settings, imageIntent(b, q.ID+":repair-review"), c.Prompt, []string{c.ID}, []string{c.Image}, refs)
			if err != nil {
				c.Review = Review{State: "incomplete", Issues: []string{err.Error()}}
				q.Warning = "修复审核未完成"
				_ = save()
				return q, err
			}
			c.Review = results[c.ID]
		}
		q.Selected = ""
		selectBest(&q)
	}
	chosen := selectedCandidate(q)
	if chosen == nil || !reviewOK(chosen.Review) {
		q.Warning = "已达到每镜一次自动修复上限，需要人工确认"
	} else {
		q.Warning = ""
		q.Image = chosen.Image
		q.VideoPrompt = chosen.VideoPrompt
	}
	if e = save(); e != nil {
		return q, e
	}
	return q, nil
}
func (s *Studio) generateBoard(ctx context.Context, pid string, b *Board, settings Settings) error {
	p, e := s.store.project(pid)
	if e != nil {
		return e
	}
	if e = preflightBoard(*b, settings); e != nil {
		return e
	}
	if b.Schema >= 3 {
		a, err := s.lockedAssets(p)
		if err != nil {
			return err
		}
		if hash(a) != b.AssetHash {
			return errors.New("参考素材快照已变化，不能继续旧分镜任务")
		}
	} else {
		if len(b.SourceMappings) == 0 && len(p.References) > 0 {
			if e = s.classifySources(ctx, p, b, settings); e != nil {
				return e
			}
			if e = s.saveBoard(pid, *b); e != nil {
				return e
			}
		}
		if e = preflightBoard(*b, settings); e != nil {
			return e
		}
		if e = s.generateAssets(ctx, pid, b, settings); e != nil {
			return e
		}
	}
	pending := map[string]bool{}
	for _, q := range b.Shots {
		if shotNeedsGeneration(q) {
			pending[q.ID] = true
		}
	}
	for len(pending) > 0 {
		indices := []int{}
		for i, q := range b.Shots {
			if !pending[q.ID] {
				continue
			}
			ok := true
			for _, id := range q.References {
				if pending[id] {
					ok = false
				}
			}
			if ok {
				indices = append(indices, i)
			}
		}
		if len(indices) == 0 {
			return errors.New("镜头参考存在循环")
		}
		view := clone(*b)
		var mu sync.Mutex
		e = parallel(ctx, indices, s.imageSem, func(i int) error {
			return s.step(ctx, pid, "image", "生成、比较分镜候选："+view.Shots[i].Title, func() error {
				q, err := s.generateShot(ctx, pid, view, view.Shots[i], settings)
				mu.Lock()
				b.Shots[i] = q
				mu.Unlock()
				return err
			})
		})
		if e != nil {
			return e
		}
		for _, i := range indices {
			delete(pending, b.Shots[i].ID)
		}
	}
	if e = s.writeProduction(pid, *b); e != nil {
		return e
	}
	return nil
}
