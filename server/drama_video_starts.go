package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// A tail start is an image input, never the provider's video-reference mode.
type DramaVideoStart struct {
	Entry       string      `json:"entry"`
	Previous    string      `json:"previous,omitempty"`
	Recommended string      `json:"recommended"`
	Reason      string      `json:"reason"`
	Choice      string      `json:"choice"`
	Mode        string      `json:"mode"`
	Waiting     string      `json:"waiting,omitempty"`
	Source      *DramaMedia `json:"source,omitempty"`
	Frame       *DramaMedia `json:"frame,omitempty"`
}

func dramaStartRecommendation(previous, current DramaEntry) (string, string) {
	if previous.ID == "" {
		return "board", "第一镜使用本镜分镜图"
	}
	if current.Fields["与上一镜关系"] != "连续接拍" {
		return "board", "切镜或连续关系未明确，使用本镜分镜图"
	}
	// An explicit continuity declaration is necessary; scene identity alone is not enough.
	if previous.Fields["来源"] == "" || previous.Fields["来源"] != current.Fields["来源"] || previous.Fields["景别/机位"] == "" || previous.Fields["景别/机位"] != current.Fields["景别/机位"] {
		return "board", "场景或景别机位不同，使用本镜分镜图"
	}
	return "tail", "分镜声明连续接拍，且场景与景别机位一致"
}

func (s *Studio) dramaTailSource(work, entry, id, hash string) (DramaMedia, error) {
	d, err := s.dramaDoc(work, "motion")
	if err != nil {
		return DramaMedia{}, err
	}
	previous, found := "", false
	for _, e := range d.Entries {
		if e.ID == entry {
			found = true
			break
		}
		if strings.HasPrefix(e.ID, "MOTION-") {
			previous = e.ID
		}
	}
	m, err := s.dramaMediaByID(work, id)
	if err != nil || !found || previous == "" || m.Entry != previous || m.Kind != "video" || m.Hash != hash || !m.Approved || !s.dramaMediaCurrent(m) {
		return DramaMedia{}, errors.New("尾帧来源须为上一镜当前已确认的视频，请确认上一镜后重新报价")
	}
	var selected string
	if err = s.store.db.QueryRow("SELECT media_id FROM drama_video_choices WHERE work_id=? AND entry=?", work, previous).Scan(&selected); err != nil || selected != m.ID {
		return DramaMedia{}, errors.New("上一镜选用版本已变化，请重新查看尾帧并报价")
	}
	return m, s.verifyDramaMedia(m)
}

func (s *Studio) dramaTailFrame(ctx context.Context, w DramaWork, entry string, source DramaMedia) (DramaMedia, error) {
	id := digest([]byte("tail-v1:" + w.ID + ":" + entry + ":" + source.ID + ":" + source.Hash))[:24]
	if m, err := s.dramaMediaByID(w.ID, id); err == nil {
		if m.Parent != source.ID {
			return m, errors.New("尾帧来源不一致")
		}
		return m, s.verifyDramaMedia(m)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return m, err
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	// Count decoded frames, then select the last one. Seeking -0.1s is not the actual final frame.
	count, err := runCommand(ctx, s.root, "ffprobe", "-v", "error", "-select_streams", "v:0", "-count_frames", "-show_entries", "stream=nb_read_frames", "-of", "csv=p=0", s.media(source.File))
	if err != nil {
		return DramaMedia{}, fmt.Errorf("读取视频尾帧失败：%w", err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(count)))
	if err != nil || n < 1 {
		return DramaMedia{}, errors.New("上一视频没有可解码的视频帧")
	}
	file, err := s.writeMedia(w.ID, uid()+"-tail.png", nil)
	if err != nil {
		return DramaMedia{}, err
	}
	if _, err = runCommand(ctx, s.root, "ffmpeg", "-v", "error", "-y", "-i", s.media(source.File), "-map", "0:v:0", "-vf", fmt.Sprintf("select=eq(n\\,%d)", n-1), "-frames:v", "1", s.media(file)); err != nil {
		return DramaMedia{}, fmt.Errorf("提取真实尾帧失败：%w", err)
	}
	b, err := os.ReadFile(s.media(file))
	if err != nil {
		return DramaMedia{}, err
	}
	if err = s.validateDramaImageAspect(Image{File: file}, w.Aspect); err != nil {
		return DramaMedia{}, err
	}
	m := DramaMedia{ID: id, WorkID: w.ID, Entry: "TAIL-" + strings.TrimPrefix(entry, "MOTION-"), Kind: "image", File: file, Hash: digest(b), Parent: source.ID, Approved: true, Current: true, ReferenceOnly: true, Mock: source.Mock}
	m.Path = s.dramaMediaPath(w.ID, filepath.Base(file))
	return m, s.saveDramaMedia(m)
}

func (s *Studio) selectedDramaTailFrame(w DramaWork, p DramaProduction) ([]DramaBinding, DramaMedia, error) {
	source, err := s.dramaTailSource(w.ID, p.Entry, p.TailVideoID, p.TailVideoHash)
	if err != nil {
		return nil, source, err
	}
	id := digest([]byte("tail-v1:" + w.ID + ":" + p.Entry + ":" + source.ID + ":" + source.Hash))[:24]
	m, err := s.dramaMediaByID(w.ID, p.FrameMediaID)
	if err != nil || m.ID != id || m.Hash != p.FrameMediaHash || m.Parent != source.ID || !m.ReferenceOnly || !m.Approved || m.Kind != "image" {
		return nil, source, errors.New("真实尾帧版本不一致，请重新预览并报价")
	}
	if err = s.verifyDramaMedia(m); err != nil {
		return nil, source, err
	}
	return []DramaBinding{{Slot: "REF-" + m.ID, Order: 1, Use: "起始帧", Control: "上一镜真实结尾的姿态与构图", Exclude: "未发生的动作", Media: m}}, source, nil
}

func (s *Studio) dramaVideoStartRoutes(g *gin.RouterGroup) {
	g.POST("/works/:id/video-starts", func(c *gin.Context) {
		var req struct {
			MotionBase string `json:"motion_base"`
			BoardBase  string `json:"board_base"`
			Entry      string `json:"entry"`
			Choices    []struct {
				Entry string `json:"entry"`
				Mode  string `json:"mode"`
			} `json:"choices"`
		}
		if !bind(c, &req) {
			return
		}
		s.dramaMu.Lock()
		defer s.dramaMu.Unlock()
		w, err := s.dramaWork(c.Param("id"))
		if err != nil {
			result(c, nil, err)
			return
		}
		motion, err := s.dramaDoc(w.ID, "motion")
		if err != nil {
			result(c, nil, err)
			return
		}
		board, err := s.dramaDoc(w.ID, "storyboard")
		if err != nil {
			result(c, nil, err)
			return
		}
		if req.MotionBase != motion.Hash || req.BoardBase != board.Hash {
			result(c, nil, errConflict)
			return
		}
		if len(req.Choices) > 200 {
			result(c, nil, errors.New("选择数量过多"))
			return
		}
		valid := map[string]bool{}
		for _, e := range motion.Entries {
			if strings.HasPrefix(e.ID, "MOTION-") {
				valid[e.ID] = true
			}
		}
		if req.Entry != "" && !valid[req.Entry] {
			result(c, nil, errors.New("镜头不存在"))
			return
		}
		seen := map[string]bool{}
		for _, ch := range req.Choices {
			if !valid[ch.Entry] || seen[ch.Entry] || !contains([]string{"auto", "board", "tail"}, ch.Mode) {
				result(c, nil, errors.New("输入选择无效"))
				return
			}
			seen[ch.Entry] = true
		}
		// Persist intent, not future media. Reloading or a failed generation never changes tail to board.
		tx, err := s.store.db.Begin()
		if err != nil {
			result(c, nil, err)
			return
		}
		defer tx.Rollback()
		for _, ch := range req.Choices {
			_, err = tx.Exec("INSERT INTO drama_video_starts(work_id,entry,motion_hash,board_hash,mode) VALUES(?,?,?,?,?) ON CONFLICT(work_id,entry) DO UPDATE SET motion_hash=excluded.motion_hash,board_hash=excluded.board_hash,mode=excluded.mode", w.ID, ch.Entry, motion.Hash, board.Hash, ch.Mode)
			if err != nil {
				result(c, nil, err)
				return
			}
		}
		if err = tx.Commit(); err != nil {
			result(c, nil, err)
			return
		}
		medias, err := s.dramaMedias(w.ID)
		if err != nil {
			result(c, nil, err)
			return
		}
		boards := map[string]DramaEntry{}
		for _, e := range board.Entries {
			boards[e.ID] = e
		}
		out := []DramaVideoStart{}
		previous := ""
		for _, e := range motion.Entries {
			if !strings.HasPrefix(e.ID, "MOTION-") {
				continue
			}
			prior := previous
			previous = e.ID
			if req.Entry != "" && e.ID != req.Entry {
				continue
			}
			b := boards[strings.Replace(e.ID, "MOTION-", "SHOT-", 1)]
			recommended, reason := dramaStartRecommendation(boards[strings.Replace(prior, "MOTION-", "SHOT-", 1)], b)
			choice := "auto"
			err = s.store.db.QueryRow("SELECT mode FROM drama_video_starts WHERE work_id=? AND entry=? AND motion_hash=? AND board_hash=?", w.ID, e.ID, motion.Hash, board.Hash).Scan(&choice)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				result(c, nil, err)
				return
			}
			mode := choice
			if mode == "auto" {
				mode = recommended
			}
			v := DramaVideoStart{Entry: e.ID, Previous: prior, Recommended: recommended, Reason: reason, Choice: choice, Mode: mode}
			if mode == "tail" {
				v.Waiting = "等待上一镜生成并确认选用；不会自动改用分镜图"
				if prior == "" {
					v.Waiting = "第一镜没有上一视频，请选择本镜分镜图"
				}
				for _, m := range medias {
					if m.Entry == prior && m.Kind == "video" && m.Selected && m.Approved && m.Current {
						source, e2 := s.dramaTailSource(w.ID, e.ID, m.ID, m.Hash)
						if e2 != nil {
							v.Waiting = e2.Error()
							break
						}
						v.Source = &source
						frame, e2 := s.dramaTailFrame(c.Request.Context(), w, e.ID, source)
						if e2 != nil {
							v.Waiting = e2.Error()
							break
						}
						v.Frame = &frame
						v.Waiting = ""
						break
					}
				}
			}
			out = append(out, v)
		}
		result(c, out, nil)
	})
}
