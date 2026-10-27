package main

import (
	"errors"
	"github.com/gin-gonic/gin"
	"strings"
)

func editableAssets(st *State, pid string, version, revision int) (*Project, *AssetSet, error) {
	p := st.Projects[pid]
	if p == nil || p.Status == "running" || p.Revision != revision {
		return nil, nil, errors.New("项目已变化或制作中，请刷新")
	}
	a := assets(*p)
	if a == nil || a.Version != version {
		return nil, nil, errors.New("参考素材版本已变化")
	}
	return p, a, nil
}

type assetEdit struct {
	Version  int      `json:"version"`
	Revision int      `json:"revision"`
	Assets   AssetSet `json:"assets"`
}

func (s *Studio) assetRoutes(api *gin.RouterGroup) {
	api.POST("/projects/:id/assets/impact", func(c *gin.Context) {
		var v assetEdit
		if c.ShouldBindJSON(&v) != nil {
			fail(c, errors.New("素材编辑格式无效"))
			return
		}
		p, e := s.store.project(c.Param("id"))
		if e != nil {
			fail(c, e)
			return
		}
		a := assets(p)
		if a == nil || a.Version != v.Version || p.Revision != v.Revision {
			fail(c, errors.New("素材版本已变化"))
			return
		}
		_, roots, e := cleanAssetSet(v.Assets, a, p)
		if e != nil {
			fail(c, e)
			return
		}
		if hash(v.Assets.Story) != hash(a.Story) {
			roots = append(roots, "*")
		}
		c.JSON(200, gin.H{"roots": roots, "affected": assetImpact(p, roots), "notice": "仅预览，不调用模型。修改已生成素材会保留历史图片，清空对应当前确认；重做另行报价。"})
	})
	api.PUT("/projects/:id/assets", func(c *gin.Context) {
		var v assetEdit
		if c.ShouldBindJSON(&v) != nil {
			fail(c, errors.New("素材编辑格式无效"))
			return
		}
		affected := []string{}
		e := s.store.change(func(st *State) error {
			p, old, e := editableAssets(st, c.Param("id"), v.Version, v.Revision)
			if e != nil {
				return e
			}
			next, roots, e := cleanAssetSet(v.Assets, old, *p)
			if e != nil {
				return e
			}
			if hash(assetDefinitions(next)) == hash(assetDefinitions(*old)) {
				return nil
			}
			if hash(next.Story) != hash(old.Story) {
				roots = append(roots, "*")
			}
			affected = assetImpact(*p, roots)
			invalidateAssetShots(p, roots)
			p.AssetSets = append(p.AssetSets, next)
			p.AssetWorkflow = true
			p.Visual = next.Visual
			p.Revision++
			p.ActiveQuote = ""
			p.Status = "review"
			p.Error = ""
			return nil
		})
		if e != nil {
			fail(c, e)
			return
		}
		c.JSON(200, gin.H{"affected": affected, "message": "素材设定已保存；需要重做的图片须重新报价"})
	})
	api.POST("/projects/:id/assets/approve", func(c *gin.Context) {
		var v struct {
			Version  int      `json:"version"`
			Revision int      `json:"revision"`
			Targets  []string `json:"targets"`
			Reason   string   `json:"reason"`
		}
		if c.ShouldBindJSON(&v) != nil || len(v.Targets) == 0 {
			fail(c, errors.New("请选择确认的素材"))
			return
		}
		e := s.store.change(func(st *State) error {
			p, a, e := editableAssets(st, c.Param("id"), v.Version, v.Revision)
			if e != nil {
				return e
			}
			if a.Locked {
				return errors.New("参考素材已锁定；如需修改，请先编辑设定或单独报价重做")
			}
			seen := map[string]bool{}
			for _, target := range v.Targets {
				if seen[target] {
					return errors.New("重复素材")
				}
				seen[target] = true
				found := false
				for _, i := range assetItems(a) {
					if i.ID != target {
						continue
					}
					found = true
					if *i.Image == "" || i.Review.State == "" || i.Review.State == "incomplete" {
						return errors.New("素材图片或模型审核未完成，请先恢复已确认任务")
					}
					if i.Review.State != "passed" && strings.TrimSpace(v.Reason) == "" {
						return errors.New("模型未通过或历史素材需要填写人工接受理由")
					}
					digest, err := fileHash(s.abs(*i.Image))
					if err != nil || digest != *i.Hash {
						return errors.New("素材图片已变化或缺失")
					}
					if err = s.hashReferences(*i.StoredRefs); err != nil {
						return err
					}
					if i.Review.State != "passed" {
						i.Review.Accepted = strings.TrimSpace(v.Reason)
					}
					a.Approvals[target] = AssetApproval{Hash: i.fingerprint(a.Visual), Reason: strings.TrimSpace(v.Reason), At: now()}
				}
				if !found {
					return errors.New("素材不存在")
				}
			}
			p.AssetSets[len(p.AssetSets)-1] = *a
			p.Revision++
			p.ActiveQuote = ""
			return nil
		})
		if e != nil {
			fail(c, e)
			return
		}
		c.JSON(200, gin.H{"message": "已人工确认选定参考素材"})
	})
	api.POST("/projects/:id/assets/lock", func(c *gin.Context) {
		var v struct {
			Version  int `json:"version"`
			Revision int `json:"revision"`
		}
		if c.ShouldBindJSON(&v) != nil {
			fail(c, errors.New("版本无效"))
			return
		}
		e := s.store.change(func(st *State) error {
			p, a, e := editableAssets(st, c.Param("id"), v.Version, v.Revision)
			if e != nil {
				return e
			}
			if e = validateAssetDefinitions(*a, *p); e != nil {
				return e
			}
			if e = s.validateAssetFiles(*a, true); e != nil {
				return e
			}
			a.Locked = true
			p.AssetSets[len(p.AssetSets)-1] = *a
			p.Revision++
			p.ActiveQuote = ""
			return nil
		})
		if e != nil {
			fail(c, e)
			return
		}
		c.JSON(200, gin.H{"message": "参考素材已锁定，可以生成整套分镜"})
	})
	api.POST("/projects/:id/assets/import", func(c *gin.Context) {
		e := s.store.change(func(st *State) error {
			p := st.Projects[c.Param("id")]
			if p == nil || p.Status == "running" || assets(*p) != nil {
				return errors.New("当前项目不可导入历史素材")
			}
			b := board(*p)
			if b == nil {
				return errors.New("没有可导入的历史分镜")
			}
			input := AssetSet{Visual: b.Visual, Story: b.Story, Characters: clone(b.Characters), Scenes: clone(b.Scenes), Props: clone(b.Props)}
			if input.Story.Synopsis == "" {
				input.Story.Synopsis = p.Idea
			}
			if len(input.Scenes) == 0 {
				input.Scenes = []SceneAsset{{ID: "scene1", Name: "历史场景", Description: "沿用历史故事主要场景，请核对并完善空间与光线"}}
			}
			a, _, err := cleanAssetSet(input, nil, *p)
			if err != nil {
				return err
			}
			for ci := range a.Characters {
				c := &a.Characters[ci]
				for _, old := range b.Characters {
					if c.ID != old.ID {
						continue
					}
					for vi := range c.Views {
						for _, view := range old.Views {
							if view.Kind == c.Views[vi].Kind {
								c.Views[vi] = clone(view)
							}
						}
						if c.Views[vi].Kind == "front" && c.Views[vi].Image == "" && old.Image != "" {
							c.Views[vi].Image = old.Image
							c.Views[vi].Review = Review{State: "legacy", Reason: "历史图片，待人工核对"}
						}
					}
					c.Image = old.Image
				}
			}
			for _, g := range []struct {
				items *[]SceneAsset
				old   []SceneAsset
			}{{&a.Scenes, b.Scenes}, {&a.Props, b.Props}} {
				for i := range *g.items {
					for _, v := range g.old {
						if (*g.items)[i].ID == v.ID {
							(*g.items)[i] = clone(v)
						}
					}
				}
			}
			for _, i := range assetItems(&a) {
				if *i.Image != "" {
					digest, e := fileHash(s.abs(*i.Image))
					if e != nil {
						return e
					}
					*i.Hash = digest
					if i.Review.State == "" {
						*i.Review = Review{State: "legacy", Reason: "历史素材，待人工核对"}
					}
				}
			}
			p.AssetSets = append(p.AssetSets, a)
			p.AssetWorkflow = true
			p.Revision++
			p.ActiveQuote = ""
			return nil
		})
		if e != nil {
			fail(c, e)
			return
		}
		c.JSON(200, gin.H{"message": "历史素材已导入，人工确认后可复用；缺失角度另行报价"})
	})
}
func assetRoot(target string) string { return strings.Split(target, ":")[0] }
func containsAssetTarget(a AssetSet, target string) bool {
	for _, i := range assetItems(&a) {
		if i.ID == target {
			return true
		}
	}
	return false
}
