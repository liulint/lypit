package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

// Pin a human-approved frame as a local reference before changing its source
// document. This explicit selection preserves the frame's identity and avoids
// a dependency cycle between a generated frame and its own storyboard entry.
func (s *Studio) dramaFrameBindingRoutes(g *gin.RouterGroup) {
	g.POST("/works/:id/bind-frame", func(c *gin.Context) {
		var req struct {
			Base    string `json:"base"`
			Entry   string `json:"entry"`
			MediaID string `json:"media_id"`
		}
		if !bind(c, &req) {
			return
		}
		s.dramaMu.Lock()
		defer s.dramaMu.Unlock()
		id := c.Param("id")
		d, e := s.dramaDoc(id, "storyboard")
		if e != nil {
			result(c, nil, e)
			return
		}
		source, e := s.dramaMediaByID(id, req.MediaID)
		if e != nil {
			result(c, nil, e)
			return
		}
		if source.Kind != "image" || source.Entry != req.Entry || !source.Approved {
			result(c, nil, errors.New("请选择本镜已确认的画面"))
			return
		}
		var entry *DramaEntry
		for _, v := range d.Entries {
			if v.ID == req.Entry {
				copy := v
				entry = &copy
				break
			}
		}
		if entry == nil {
			result(c, nil, errors.New("分镜条目不存在"))
			return
		}
		pinned := source
		pinned.ID = digest([]byte("frame-reference:" + id + ":" + source.ID + ":" + source.Hash))[:24]
		if source.ReferenceOnly {
			pinned = source
		} else {
			pinned.Parent = source.ID
			pinned.TaskID = ""
			pinned.ReferenceOnly = true
			pinned.Path = strings.Replace(source.Path, "/制作成果/", "/确认参考/", 1)
			if pinned.Path == source.Path {
				pinned.Path = "确认参考/" + filepath.Base(source.File)
			}
		}
		declaration := fmt.Sprintf("REF-%s（顺序：1）· %s《%s已确认首帧》（用途：起始帧；控制：起始构图与角色外貌；不得控制：尚未发生的动作）", pinned.ID, pinned.Path, req.Entry)
		// A repeated request may see the already-written declaration and must only read it.
		if entry.Fields["输入参考图"] == declaration {
			if e = s.verifyDramaMedia(pinned); e != nil {
				result(c, nil, e)
				return
			}
			result(c, d, nil)
			return
		}
		if d.Hash != req.Base {
			result(c, nil, errConflict)
			return
		}
		if !s.dramaMediaCurrent(source) {
			result(c, nil, errors.New("画面来源已变化，请先复核并确认"))
			return
		}
		if e = s.verifyDramaMedia(source); e != nil {
			result(c, nil, e)
			return
		}
		if e = s.saveDramaMedia(pinned); e != nil {
			result(c, nil, e)
			return
		}
		lines := strings.Split(d.Text, "\n")
		found := false
		for i := entry.Start + 1; i < entry.End; i++ {
			if strings.HasPrefix(lines[i], "- 输入参考图：") {
				lines[i] = "- 输入参考图：" + declaration
				found = true
				break
			}
		}
		if !found {
			result(c, nil, errors.New("分镜缺少参考字段，请先修正分镜"))
			return
		}
		out, e := s.saveDramaDoc(id, "storyboard", d.Hash, strings.Join(lines, "\n"), false, nil)
		result(c, out, e)
	})
}

// Resolve an explicitly selected local first frame for an unbound plan only.
// The quote/task owns the immutable media snapshot; source documents stay intact.
func (s *Studio) selectedDramaVideoFrame(work string, p DramaProduction, entry DramaEntry) ([]DramaBinding, error) {
	if err := dramaSingleFirstFramePlan(entry, p.Mode); err != nil {
		return nil, err
	}
	m, err := s.dramaMediaByID(work, p.FrameMediaID)
	if err != nil {
		return nil, errors.New("所选首帧不存在于本集")
	}
	if m.Hash != p.FrameMediaHash {
		return nil, errors.New("首帧版本已变化，请重新选择并查看报价")
	}
	if m.Kind != "image" || m.Entry != strings.Replace(entry.ID, "MOTION-", "SHOT-", 1) || !m.Approved || !s.dramaMediaCurrent(m) {
		return nil, errors.New("请选择本镜当前已确认的分镜图片作为首帧")
	}
	if err = s.verifyDramaMedia(m); err != nil {
		return nil, err
	}
	return []DramaBinding{{Slot: "REF-" + m.ID, Order: 1, Use: "起始帧", Control: "起始构图与角色外貌", Exclude: "尚未发生的动作", Media: m}}, nil
}

func dramaSingleFirstFramePlan(entry DramaEntry, mode string) error {
	decl := strings.TrimSpace(entry.Fields["输入参考图"])
	planned := strings.HasPrefix(decl, "PLAN-") && strings.Count(decl, "PLAN-") == 1 && strings.Count(decl, "用途：") == 1 && strings.Count(decl, "顺序：") == 1 && strings.Contains(decl, "顺序：1）") && strings.Contains(decl, "用途：起始帧")
	missing := decl == "无（待补参考图：本镜起始帧）"
	// A batch explicitly selects the same single-first-frame mode, never a multi-reference or end-frame scheme.
	bound := strings.HasPrefix(decl, "REF-") && len(dramaRefRE.FindAllStringSubmatch(decl, -1)) == 1 && strings.Count(decl, "REF-") == 1 && strings.Count(decl, "用途：") == 1 && strings.Contains(decl, "用途：起始帧；") && strings.Contains(decl, "顺序：1）")
	if (!planned && !missing && !bound) || strings.Contains(decl, "结束帧") || strings.Contains(decl, "顺序：2") || (mode != "" && mode != "first_frame") {
		return errors.New("此参考方案不能直接替换为单首帧，请先在视频说明中确认参考方式")
	}
	return nil
}
