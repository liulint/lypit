package main

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
)

func editableProject(st *State, id string, version, revision int) (*Project, *Board, error) {
	p := st.Projects[id]
	if p == nil {
		return nil, nil, errors.New("项目不存在")
	}
	b := board(*p)
	if b == nil || p.Status == "running" || b.Version != version || p.Revision != revision {
		return nil, nil, errors.New("分镜版本已变化或制作中，请刷新后重试")
	}
	return p, b, nil
}
func (s *Studio) storyboardRoutes(api *gin.RouterGroup) {
	api.GET("/projects/:id/storyboard", func(c *gin.Context) {
		p, e := s.store.project(c.Param("id"))
		if e != nil {
			fail(c, e)
			return
		}
		c.JSON(200, gin.H{"visual": p.Visual, "board": board(p), "revision": p.Revision})
	})
	api.POST("/projects/:id/impact", func(c *gin.Context) {
		var v struct {
			ShotID      string `json:"shot_id"`
			Instruction string `json:"instruction"`
		}
		if e := c.ShouldBindJSON(&v); e != nil {
			fail(c, e)
			return
		}
		p, e := s.store.project(c.Param("id"))
		if e != nil || board(p) == nil {
			fail(c, errors.New("没有分镜"))
			return
		}
		b := *board(p)
		roots := []string{}
		if v.ShotID != "" {
			for _, q := range b.Shots {
				if q.ID == v.ShotID {
					roots = append(roots, q.ID)
				}
			}
			if len(roots) == 0 {
				fail(c, errors.New("镜头不存在"))
				return
			}
		} else {
			for _, q := range b.Shots {
				roots = append(roots, q.ID)
			}
		}
		c.JSON(200, gin.H{"affected": dependentShots(b, roots), "version": b.Version, "revision": p.Revision, "notice": "这里只预览引用依赖；全片自然语言修改保守列出全部镜头，具体修改在确认后的导演计划中决定。此操作不调用模型。"})
	})
	api.GET("/projects/:id/dependencies", func(c *gin.Context) {
		p, e := s.store.project(c.Param("id"))
		if e != nil || board(p) == nil {
			fail(c, errors.New("没有分镜"))
			return
		}
		items := []gin.H{}
		b := *board(p)
		for _, q := range b.Shots {
			refs, e := s.shotReferences(b, q)
			item := gin.H{"shot_id": q.ID, "references": refs, "previous_shots": q.References, "stale": q.Stale}
			if e != nil {
				item["pending"] = e.Error()
			}
			items = append(items, item)
		}
		c.JSON(200, items)
	})
	api.POST("/projects/:id/select", func(c *gin.Context) {
		var v struct {
			Version     int    `json:"version"`
			Revision    int    `json:"revision"`
			ShotID      string `json:"shot_id"`
			CandidateID string `json:"candidate_id"`
		}
		if e := c.ShouldBindJSON(&v); e != nil {
			fail(c, e)
			return
		}
		affected := []string{}
		e := s.store.change(func(st *State) error {
			p, old, e := editableProject(st, c.Param("id"), v.Version, v.Revision)
			if e != nil {
				return e
			}
			var found *Candidate
			for _, q := range old.Shots {
				if q.ID == v.ShotID {
					if q.Stale {
						return errors.New("本镜参考已变化，请先补生成")
					}
					for _, candidate := range q.Candidates {
						if candidate.ID == v.CandidateID {
							copy := candidate
							found = &copy
						}
					}
				}
			}
			if found == nil || found.Image == "" {
				return errors.New("候选不存在或图片未完成")
			}
			actual, e := fileHash(s.abs(found.Image))
			if e != nil || found.Hash != "" && actual != found.Hash {
				return errors.New("候选图片内容已变化")
			}
			if e = s.hashReferences(found.References); e != nil {
				return e
			}
			b := cloneBoardVersion(p)
			for i := range b.Shots {
				q := &b.Shots[i]
				if q.ID == v.ShotID {
					q.Selected = found.ID
					q.Image = found.Image
					q.VideoPrompt = found.VideoPrompt
					q.Warning = ""
					if !reviewOK(found.Review) {
						q.Warning = "已换选未通过审核的候选，请说明接受理由或重新生成"
					}
				}
			}
			affected = dependentShots(*b, []string{v.ShotID})
			invalidateDependents(b, v.ShotID)
			return nil
		})
		if e != nil {
			fail(c, e)
			return
		}
		c.JSON(200, gin.H{"affected": affected, "message": "已换选，旧视频报价失效；受影响后镜不会自动收费重生"})
	})
	api.POST("/projects/:id/review", func(c *gin.Context) {
		var v struct {
			Version     int    `json:"version"`
			Revision    int    `json:"revision"`
			Target      string `json:"target"`
			ShotID      string `json:"shot_id"`
			CandidateID string `json:"candidate_id"`
			AssetID     string `json:"asset_id"`
			View        string `json:"view"`
			Accept      bool   `json:"accept"`
			Reason      string `json:"reason"`
		}
		if e := c.ShouldBindJSON(&v); e != nil {
			fail(c, e)
			return
		}
		if v.Accept && (strings.TrimSpace(v.Reason) == "" || len(v.Reason) > 2000) {
			fail(c, errors.New("接受警告必须填写具体理由（最多2000字节）"))
			return
		}
		e := s.store.change(func(st *State) error {
			p, old, e := editableProject(st, c.Param("id"), v.Version, v.Revision)
			if e != nil {
				return e
			}
			active := p.ActiveQuote
			b := cloneBoardVersion(p)
			reason := ""
			if v.Accept {
				reason = strings.TrimSpace(v.Reason)
			}
			found := false
			switch v.Target {
			case "candidate", "video":
				for i := range b.Shots {
					q := &b.Shots[i]
					if q.ID != v.ShotID {
						continue
					}
					if v.Target == "video" {
						q.VideoReview.Accepted = reason
						found = true
						b.Approved = old.Approved
						p.ActiveQuote = active
						p.Status = "attention"
						p.Error = "视频审核意见已更新，可恢复原任务继续合成"
					} else {
						for j := range q.Candidates {
							candidate := &q.Candidates[j]
							if candidate.ID == v.CandidateID {
								candidate.Review.Accepted = reason
								found = true
								if candidate.ID == q.Selected {
									q.Warning = ""
								}
							}
						}
					}
				}
			case "character":
				for i := range b.Characters {
					if b.Characters[i].ID == v.AssetID {
						for j := range b.Characters[i].Views {
							view := &b.Characters[i].Views[j]
							if view.Kind == v.View {
								view.Review.Accepted = reason
								found = true
							}
						}
					}
				}
			case "scene", "prop":
				assets := &b.Scenes
				if v.Target == "prop" {
					assets = &b.Props
				}
				for i := range *assets {
					if (*assets)[i].ID == v.AssetID {
						(*assets)[i].Review.Accepted = reason
						found = true
					}
				}
			default:
				return errors.New("未知审核对象")
			}
			if !found {
				return errors.New("审核对象不存在")
			}
			return nil
		})
		if e != nil {
			fail(c, e)
			return
		}
		c.JSON(200, gin.H{"message": "已保存人工处理意见，保留原始模型审核结果"})
	})
	api.POST("/projects/:id/prompts", func(c *gin.Context) {
		var v struct {
			Version     int    `json:"version"`
			Revision    int    `json:"revision"`
			ShotID      string `json:"shot_id"`
			Instruction string `json:"instruction"`
		}
		if e := c.ShouldBindJSON(&v); e != nil {
			fail(c, e)
			return
		}
		if strings.TrimSpace(v.Instruction) == "" || len(v.Instruction) > 10000 {
			fail(c, errors.New("修改意见不能为空或过长"))
			return
		}
		e := s.store.change(func(st *State) error {
			p, old, e := editableProject(st, c.Param("id"), v.Version, v.Revision)
			if e != nil {
				return e
			}
			roots := []string{}
			for _, q := range old.Shots {
				if v.ShotID == "" || q.ID == v.ShotID {
					roots = append(roots, q.ID)
				}
			}
			if len(roots) == 0 {
				return errors.New("指定镜头不存在")
			}
			b := cloneBoardVersion(p)
			affected := dependentShots(*b, roots)
			for i := range b.Shots {
				q := &b.Shots[i]
				if slices.Contains(roots, q.ID) {
					q.ImagePrompt += "\n用户修改意见：" + v.Instruction
					if q.BaseVideoPrompt == "" {
						q.BaseVideoPrompt = q.VideoPrompt
					}
					q.BaseVideoPrompt += "\n用户修改意见：" + v.Instruction
					q.VideoPrompt = q.BaseVideoPrompt
				}
				if slices.Contains(affected, q.ID) {
					q.Stale = true
					q.Video = ""
					q.Warning = "提示词或引用已改变，旧图保留供对照，需确认补生成"
				}
			}
			return nil
		})
		if e != nil {
			fail(c, e)
			return
		}
		c.JSON(200, gin.H{"message": "仅更新提示词与依赖状态，没有发起模型调用"})
	})
	api.PUT("/projects/:id/visual", func(c *gin.Context) {
		var v struct {
			Revision int        `json:"revision"`
			Visual   VisualPlan `json:"visual"`
		}
		if e := c.ShouldBindJSON(&v); e != nil {
			fail(c, e)
			return
		}
		if strings.TrimSpace(v.Visual.Style) == "" || len(v.Visual.Style) > 500 {
			fail(c, errors.New("请填写有效风格"))
			return
		}
		e := s.store.change(func(st *State) error {
			p := st.Projects[c.Param("id")]
			if p == nil || p.Status == "running" || p.Revision != v.Revision {
				return errors.New("项目已变化或制作中")
			}
			if assets(*p) != nil {
				return errors.New("请在参考素材设定中修改视觉风格，先预览影响再保存")
			}
			p.Visual = visualDefaults(v.Visual, p.Idea)
			if board(*p) == nil {
				p.Revision++
				return nil
			}
			b := cloneBoardVersion(p)
			b.Visual = p.Visual
			invalidateAssets(b, nil, true)
			return nil
		})
		if e != nil {
			fail(c, e)
			return
		}
		c.JSON(200, gin.H{"message": "视觉设定已更新，受影响素材等待重新报价"})
	})
	api.PUT("/projects/:id/reference-mappings", func(c *gin.Context) {
		var v struct {
			Version  int             `json:"version"`
			Revision int             `json:"revision"`
			Mappings []SourceMapping `json:"mappings"`
		}
		if e := c.ShouldBindJSON(&v); e != nil {
			fail(c, e)
			return
		}
		e := s.store.change(func(st *State) error {
			p, old, e := editableProject(st, c.Param("id"), v.Version, v.Revision)
			if e != nil {
				return e
			}
			if e = validateMappings(*p, *old, v.Mappings); e != nil {
				return e
			}
			touched, all := changedMappingTargets(old.SourceMappings, v.Mappings)
			p.ReferenceMappings = clone(v.Mappings)
			b := cloneBoardVersion(p)
			b.SourceMappings = clone(v.Mappings)
			invalidateAssets(b, touched, all)
			return nil
		})
		if e != nil {
			fail(c, e)
			return
		}
		c.JSON(200, gin.H{"message": "参考用途已纠正，受影响分支等待补生成"})
	})
}
func invalidateAssets(b *Board, ids []string, all bool) {
	roots := []string{}
	for i := range b.Characters {
		c := &b.Characters[i]
		if all || slices.Contains(ids, c.ID) {
			c.Views = nil
			c.Image = ""
		}
	}
	for _, group := range []*[]SceneAsset{&b.Scenes, &b.Props} {
		for i := range *group {
			a := &(*group)[i]
			if all || slices.Contains(ids, a.ID) {
				a.Image = ""
				a.Hash = ""
				a.Review = Review{}
			}
		}
	}
	for _, q := range b.Shots {
		affected := all || slices.Contains(ids, q.SceneID) || slices.Contains(ids, q.PropID)
		for _, id := range q.Characters {
			affected = affected || slices.Contains(ids, id)
		}
		if affected {
			roots = append(roots, q.ID)
		}
	}
	for i := range b.Shots {
		if slices.Contains(dependentShots(*b, roots), b.Shots[i].ID) {
			b.Shots[i].Stale = true
			b.Shots[i].Video = ""
			b.Shots[i].Warning = fmt.Sprintf("角色、场景、道具或风格参考已更新")
		}
	}
}

// Reason-only changes do not change the actual reference graph.
func changedMappingTargets(old, next []SourceMapping) ([]string, bool) {
	previous, current := map[string]SourceMapping{}, map[string]SourceMapping{}
	for _, m := range old {
		previous[m.File] = m
	}
	for _, m := range next {
		current[m.File] = m
	}
	ids := []string{}
	all := false
	for _, group := range [][]SourceMapping{old, next} {
		for _, m := range group {
			a, aok := previous[m.File]
			b, bok := current[m.File]
			if aok && bok && a.Kind == b.Kind && a.Target == b.Target {
				continue
			}
			if m.Kind == "style" {
				all = true
			} else if !slices.Contains(ids, m.Target) {
				ids = append(ids, m.Target)
			}
		}
	}
	return ids, all
}
