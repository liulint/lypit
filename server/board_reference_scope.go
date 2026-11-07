package main

import (
	"errors"
)

func (s *Studio) boardReferenceImages(p Project, q Shot) ([]Image, error) {
	if len(q.References) == 0 {
		return nil, errors.New("请至少选择一份本镜参考素材")
	}
	assets := map[string]Asset{}
	for _, a := range p.Assets {
		assets[a.ID] = a
	}
	seen := map[string]bool{}
	refs := make([]Image, 0, len(q.References))
	for _, id := range q.References {
		a, ok := assets[id]
		if !ok || a.Master == nil || seen[id] {
			return nil, errors.New("本镜参考素材缺失或重复，请重新选择；不会自动丢弃引用")
		}
		seen[id] = true
		refs = append(refs, *a.Master)
	}
	return refs, nil
}

// A quote-scoped selection is applied to a copy, never to the project or old task.
// Only the newly confirmed single-shot result may commit this selection.
func selectedReferenceProject(p Project, shotID string, ids []string) (Project, error) {
	if ids == nil {
		return p, nil
	}
	if len(ids) == 0 {
		return p, errors.New("请至少选择一份本镜参考素材")
	}
	copyP, err := decodeBody[Project](jsonBytes(p))
	if err != nil {
		return p, err
	}
	for i := range copyP.Shots {
		q := &copyP.Shots[i]
		if q.ID != shotID {
			continue
		}
		allowed := map[string]bool{}
		for _, id := range q.References {
			allowed[id] = true
		}
		seen := map[string]bool{}
		for _, id := range ids {
			if !allowed[id] || seen[id] {
				return p, errors.New("只能从本镜原引用中选择素材，不能新增或重复引用")
			}
			seen[id] = true
		}
		q.References = append([]string(nil), ids...)
		if err := validateSpeech(copyP, *q); err != nil {
			return p, err
		}
		return copyP, nil
	}
	return p, errors.New("目标镜头不存在")
}
