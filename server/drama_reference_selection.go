package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

func (s *Studio) dramaMediaByID(work, id string) (DramaMedia, error) {
	var m DramaMedia
	var raw []byte
	if len(id) < 24 || len(id) > 26 || !dramaID.MatchString(id[:24]) || strings.Trim(id[24:], "0123456789") != "" {
		return m, errors.New("素材标识无效")
	}
	e := s.store.db.QueryRow("SELECT body FROM drama_media WHERE work_id=? AND id=?", work, id).Scan(&raw)
	if e == nil {
		e = json.Unmarshal(raw, &m)
	}
	return m, e
}

// A selected image is an immutable visual fact. Rechecking its creative ancestry
// here would cycle when an image is used to revise the document that produced it.
func (s *Studio) dramaReferenceCurrent(work, id, hash string) bool {
	m, e := s.dramaMediaByID(work, id)
	return e == nil && m.Approved && m.Hash == hash && s.verifyDramaMedia(m) == nil
}
func (s *Studio) selectDramaReferences(work string, ids []string) ([]DramaMedia, error) {
	if len(ids) > 40 {
		return nil, errors.New("每次最多选择40份参考素材")
	}
	out := []DramaMedia{}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			return nil, errors.New("参考素材重复")
		}
		seen[id] = true
		m, e := s.dramaMediaByID(work, id)
		if e != nil {
			return nil, e
		}
		if m.Kind != "image" || !m.Approved || !s.dramaMediaCurrent(m) {
			return nil, errors.New("请选择本集当前已确认的图片")
		}
		if e = s.verifyDramaMedia(m); e != nil {
			return nil, e
		}
		out = append(out, m)
	}
	return out, nil
}
func (s *Studio) dramaReferencePrompt(work string, inputs map[string]string) (string, error) {
	var b strings.Builder
	for key, hash := range inputs {
		if !strings.HasPrefix(key, "@media:") {
			continue
		}
		m, e := s.dramaMediaByID(work, strings.TrimPrefix(key, "@media:"))
		if e != nil {
			return "", e
		}
		if !s.dramaReferenceCurrent(work, m.ID, hash) {
			return "", errors.New("已选参考素材发生变化，请重新选择")
		}
		fmt.Fprintf(&b, "\n已选真实参考：%s\n", jsonBytes(m))
	}
	if b.Len() > 0 {
		b.WriteString("\n图片是已确认可引用的本集素材，不得声称已观看其视觉内容。按条目用途选择适用参考，不把全部参考强塞到每一镜。REF使用上述准确项目路径、用途、允许控制及不得控制；缺少角色/场景对应关系时明确待选择。\n")
	}
	return b.String(), nil
}
