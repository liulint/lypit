package main

import "errors"

// Policy 4 adds approved character sheets. Policies 0/2/3 keep their historical inputs.
const fourViewPolicyVersion = 4

func (s *Studio) fourViewBoardReferences(p Project, q Shot) ([]Image, error) {
	masters, err := s.boardReferenceImages(p, q)
	if err != nil {
		return nil, err
	}
	refs := []Image{}
	for i, id := range q.References {
		if !masters[i].Approved {
			return nil, errors.New("请先确认本镜素材主图")
		}
		refs = append(refs, masters[i])
		for _, a := range p.Assets {
			if a.ID != id || a.Kind != "character" {
				continue
			}
			if a.Sheet == nil || !a.Sheet.Approved || len(a.Views) != 4 {
				return nil, errors.New("请先制作并确认角色四视角设定图")
			}
			for _, v := range a.Views {
				if !v.Approved {
					return nil, errors.New("角色四视角尚未确认")
				}
				if err := s.verifyImage(v); err != nil {
					return nil, err
				}
			}
			if err := s.verifyImage(*a.Sheet); err != nil {
				return nil, err
			}
			refs = append(refs, *a.Sheet)
		}
	}
	return refs, nil
}

func (s *Studio) taskBoardReferences(t Task, q Shot) ([]Image, error) {
	if t.ReferencePolicyVersion == fourViewPolicyVersion {
		return s.fourViewBoardReferences(t.Input, q)
	}
	return s.boardReferenceImages(t.Input, q)
}

func (s *Studio) prepareFourViewReferences(refs []Image, p Project, shot Shot) (ReferencePlan, [][]byte, error) {
	// Use asset IDs and input order, not file lookup: different assets may share a file.
	sources := []ReferenceSource{}
	for _, id := range shot.References {
		for _, a := range p.Assets {
			if a.ID != id {
				continue
			}
			images := []Image{}
			if a.Master != nil {
				images = append(images, *a.Master)
			}
			if a.Kind == "character" && a.Sheet != nil {
				images = append(images, *a.Sheet)
			}
			for i, im := range images {
				im.ReferencePlan = nil
				role := "主图"
				if i == 1 {
					role = "四视角设定图（正面、侧面、背面、近景）"
				}
				sources = append(sources, ReferenceSource{Image: im, AssetID: a.ID, Kind: a.Kind, Name: a.Name + " · " + role})
			}
		}
	}
	if len(refs) == len(sources)+1 && shotHasImage(shot) {
		im := shot.Candidates[shot.Selected]
		im.ReferencePlan = nil
		sources = append(sources, ReferenceSource{Image: im, Kind: "composition", Name: "当前已生成画面"})
	}
	if len(refs) != len(sources) {
		return ReferencePlan{}, nil, errors.New("实际参考图片数量与镜头快照不符，未调用模型")
	}
	for i, ref := range refs {
		if ref.File != sources[i].Image.File || ref.Hash != sources[i].Image.Hash {
			return ReferencePlan{}, nil, errors.New("实际参考图片顺序或内容与镜头快照不符，未调用模型")
		}
	}

	plan, raw, err := s.prepareReferenceSources(sources, cliImageReferenceLimit)
	if err != nil {
		return plan, nil, err
	}
	plan.Version = fourViewPolicyVersion
	for i := range plan.Boards {
		plan.Boards[i].File = p.ID + "/reference-" + plan.Boards[i].Hash + ".png"
	}
	return plan, raw, nil
}

// Quote previews are the exact bytes sent to the provider. Execution only reads/rebuilds;
// concurrent workers never write the same preview file.
func (s *Studio) saveReferencePreviews(p Project, plan ReferencePlan, raw [][]byte) error {
	for i, b := range raw {
		if _, err := s.writeMedia(p.ID, "reference-"+plan.Boards[i].Hash+".png", b); err != nil {
			return err
		}
	}
	return nil
}

func (s *Studio) prepareFourViewTaskReferences(t *Task, key string, refs []Image) (ReferencePlan, [][]byte, error) {
	expected := t.ImageReferencePlan
	if t.Stage == "boards" {
		plan, ok := t.ShotReferencePlans[key]
		if !ok {
			return ReferencePlan{}, nil, errors.New("分镜缺少已确认参考板快照，禁止提交")
		}
		expected = &plan
	}
	if expected == nil {
		return ReferencePlan{}, nil, errors.New("任务缺少四视角参考报价快照，禁止提交")
	}
	var shot Shot
	for _, q := range t.Input.Shots {
		if (t.Stage == "boards" && key == "shot-"+q.ID+"-0") || (t.Stage == "board_retry" && q.ID == t.ShotID && key == "shot-"+q.ID+"-retry") {
			shot = q
			break
		}
	}
	if shot.ID == "" {
		return ReferencePlan{}, nil, errors.New("分镜作业标识与报价不符，未调用模型")
	}
	plan, raw, err := s.prepareFourViewReferences(refs, t.Input, shot)
	if err != nil {
		return plan, nil, err
	}
	if string(jsonBytes(plan)) != string(jsonBytes(expected)) {
		return plan, nil, errors.New("参考板或原图与已确认报价不一致，请重新报价；未调用模型")
	}
	return plan, raw, nil
}
