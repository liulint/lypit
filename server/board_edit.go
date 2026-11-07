package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

const imageEditPlanVersion = 3

type ImageEditSpec struct {
	Base         Image    `json:"base"`
	BaseIndex    int      `json:"base_index"`
	BaseVersion  int      `json:"base_version"`
	Changes      string   `json:"changes"`
	Preserve     string   `json:"preserve"`
	AuxiliaryIDs []string `json:"auxiliary_ids"`
}

func (s *Studio) imageEditScope(p Project, id, changes, preserve string, ids []string) (Shot, ImageEditSpec, []Image, error) {
	var spec ImageEditSpec
	q, err := s.retryShot(p, id, changes)
	if err != nil {
		return q, spec, nil, err
	}
	if !shotHasImage(q) {
		return q, spec, nil, errors.New("基于当前图修改必须先有有效底图，请先补生成图片")
	}
	changes, preserve = strings.TrimSpace(changes), strings.TrimSpace(preserve)
	if changes == "" || preserve == "" || len([]rune(changes)) > 1000 || len([]rune(preserve)) > 2000 {
		return q, spec, nil, errors.New("请填写修改内容（最多1000字）和必须保留内容（最多2000字）")
	}
	if ids == nil {
		ids = q.References
	}
	allowed := map[string]bool{}
	for _, id := range q.References {
		allowed[id] = true
	}
	seen := map[string]bool{}
	chosen := []string{}
	for _, id := range ids {
		if !allowed[id] || seen[id] {
			return q, spec, nil, errors.New("辅助素材只能从本镜原引用中选择，不能新增或重复")
		}
		seen[id] = true
		chosen = append(chosen, id)
	}
	auxShot := q
	auxShot.References = chosen
	aux := []Image{}
	if len(chosen) > 0 {
		aux, err = s.boardReferenceImages(p, auxShot)
		if err != nil {
			return q, spec, nil, err
		}
	}
	version := 0
	for i := 0; i <= q.Selected; i++ {
		if q.Candidates[i].File != "" {
			version++
		}
	}
	spec = ImageEditSpec{Base: q.Candidates[q.Selected], BaseIndex: q.Selected, BaseVersion: version, Changes: changes, Preserve: preserve, AuxiliaryIDs: chosen}
	refs := append([]Image{spec.Base}, aux...)
	return q, spec, refs, nil
}

func (s *Studio) prepareEditReferences(refs []Image, p Project, key string, spec *ImageEditSpec) (ReferencePlan, [][]byte, error) {
	var empty ReferencePlan
	if spec == nil || len(refs) != 1+len(spec.AuxiliaryIDs) || len(refs) == 0 || refs[0].File != spec.Base.File || refs[0].Hash != spec.Base.Hash {
		return empty, nil, errors.New("编辑底图或辅助素材与确认快照不符，未调用模型")
	}
	base, baseBytes, err := s.prepareReferenceBoards(refs[:1], Project{}, "")
	if err != nil {
		return empty, nil, fmt.Errorf("编辑底图无法独立传入：%w", err)
	}
	// Auxiliary IDs are quote-scoped. They do not alter the shot's material provenance.
	auxInput, err := decodeBody[Project](jsonBytes(p))
	if err != nil {
		return empty, nil, err
	}
	for i := range auxInput.Shots {
		if strings.HasPrefix(key, "shot-"+auxInput.Shots[i].ID+"-") {
			auxInput.Shots[i].References = spec.AuxiliaryIDs
		}
	}
	aux, auxBytes, err := s.prepareReferenceBoardsWithLimit(refs[1:], auxInput, key, 4)
	if err != nil {
		return empty, nil, err
	}
	base.Version = imageEditPlanVersion
	base.Sources[0].Name, base.Sources[0].Kind = "当前图 · 唯一编辑底图", "edit_base"
	base.Boards[0].Kind = "edit_base"
	base.Sources = append(base.Sources, aux.Sources...)
	for _, board := range aux.Boards {
		for i := range board.Cells {
			board.Cells[i].Source++
		}
		base.Boards = append(base.Boards, board)
	}
	return base, append(baseBytes, auxBytes...), nil
}

func (s *Studio) prepareTaskReferences(t *Task, key string, refs []Image) (ReferencePlan, [][]byte, error) {
	if t.ReferencePolicyVersion == fourViewPolicyVersion {
		return s.prepareFourViewTaskReferences(t, key, refs)
	}
	if t.Stage == "board_edit" {
		if t.ImageEdit == nil || t.ReferencePolicyVersion != imageEditPlanVersion || t.ImageReferencePlan == nil {
			return ReferencePlan{}, nil, errors.New("编辑任务缺少独立底图报价快照，禁止重新提交")
		}
		if key != "shot-"+t.ShotID+"-edit" {
			return ReferencePlan{}, nil, errors.New("编辑作业标识与报价不符，未调用模型")
		}
		_, spec, expected, err := s.imageEditScope(t.Input, t.ShotID, t.ImageEdit.Changes, t.ImageEdit.Preserve, t.ImageEdit.AuxiliaryIDs)
		if err != nil {
			return ReferencePlan{}, nil, err
		}
		if string(jsonBytes(spec)) != string(jsonBytes(t.ImageEdit)) || string(jsonBytes(expected)) != string(jsonBytes(refs)) {
			return ReferencePlan{}, nil, errors.New("编辑底图或辅助素材与已确认快照不符，未调用模型")
		}
		return s.prepareEditReferences(refs, t.Input, key, t.ImageEdit)
	}
	return s.prepareReferenceBoards(refs, t.Input, key)
}

func (s *Studio) editBoard(ctx context.Context, t *Task, makeImage imageMaker) error {
	if t.ImageEdit == nil {
		return errors.New("编辑任务缺少底图与保留内容快照")
	}
	key := "shot-" + t.ShotID + "-edit"
	expected := t.ProjectID + "/" + t.ID + "-" + key + ".png"
	for _, q := range t.Input.Shots {
		if q.ID == t.ShotID && shotHasImage(q) && q.Candidates[q.Selected].File == expected {
			return s.verifyImage(q.Candidates[q.Selected])
		}
	}
	q, spec, refs, err := s.imageEditScope(t.Input, t.ShotID, t.ImageEdit.Changes, t.ImageEdit.Preserve, t.ImageEdit.AuxiliaryIDs)
	if err != nil {
		return err
	}
	if string(jsonBytes(spec)) != string(jsonBytes(t.ImageEdit)) {
		return errors.New("编辑底图已变化，请重新报价；未调用模型")
	}
	prompt := "编辑现有图片，只修改明确要求的部分。第1张输入为唯一编辑底图，保持其镜头构图、画幅、主体布局及未要求修改的内容；其他输入仅作辅助校对，不可据此重新构图或重画整幅。修改内容：" + spec.Changes + "。必须保留：" + spec.Preserve + "。只输出1张修改后的完整分镜，不输出拼图或对比图，不添加未要求的内容。"
	prompt, err = t.renderPrompt("board_edit", prompt, map[string]string{"changes": spec.Changes, "preserve": spec.Preserve})
	if err != nil {
		return err
	}
	if err = s.progress(t, 5, "基于已报价底图修改："+q.Title); err != nil {
		return err
	}
	im, err := makeImage(ctx, t, key, prompt, refs, false)
	if err != nil {
		return err
	}
	if im.File == "" {
		return errors.New("图片编辑没有返回有效图片，原图保留")
	}
	im.Operation, im.SourceFile, im.SourceHash = "edit", spec.Base.File, spec.Base.Hash
	for i := range t.Input.Shots {
		if t.Input.Shots[i].ID == q.ID {
			shot := &t.Input.Shots[i]
			shot.Candidates = append(shot.Candidates, im)
			shot.Selected = len(shot.Candidates) - 1
			shot.Approved = false
			shot.Video = ""
			t.Input.Output = ""
			return s.progress(t, 95, "修改图已保存，原图保留；请对比版本并重新确认画面")
		}
	}
	return errors.New("目标镜头不存在")
}
