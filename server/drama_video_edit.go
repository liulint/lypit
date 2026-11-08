package main

import (
	"context"
	"errors"
	"strings"
	"time"
)

func dramaEditSourceHash(m DramaMedia) string {
	return digest(jsonBytes(struct {
		ID, Entry, File, Hash, TaskID string
		Duration                      float64
	}{m.ID, m.Entry, m.File, m.Hash, m.TaskID, m.Duration}))
}

// A video being corrected need not be approved as a final result. It must be
// the explicitly selected, current output of this exact shot and supplier task.
func (s *Studio) dramaVideoEditInput(w DramaWork, p DramaProduction, cfg Settings) (VideoInput, DramaMedia, error) {
	v := VideoInput{Mode: "edit_video", ShotID: strings.TrimPrefix(p.Entry, "MOTION-"), Images: []VideoImage{}}
	if p.Source == nil || p.FrameMediaID != "" || len(p.FrameSelections) > 0 {
		return v, DramaMedia{}, errors.New("视频修改须选择当前视频，不可同时传首帧")
	}
	m, err := s.dramaMediaByID(w.ID, p.Source.ID)
	if err != nil || m.Kind != "video" || m.Entry != p.Entry || m.Hash != p.Source.Hash || !s.dramaMediaCurrent(m) {
		return v, m, errors.New("请选择本镜当前有效的视频版本，版本变化后请重新报价")
	}
	if err = s.verifyDramaMedia(m); err != nil {
		return v, m, err
	}
	min, max := 4.0, 30.0
	if cfg.Mode != "mock" {
		ep, err := resolveEndpoint(cfg, "video")
		if err != nil {
			return v, m, err
		}
		version := videoEditModelVersion(ep.Model)
		if version == "" {
			return v, m, errors.New("当前模型未核实视频编辑能力，请选择受支持的Seedance 2.0/2.5模型，或选择重新生成；不会自动切换方式")
		}
		if p.Duration < 4 {
			return v, m, errors.New("视频编辑至少4秒，请选择重新生成")
		}
		if version == "2.0" {
			min, max = 2, 15
		}
		old, err := s.store.task(m.TaskID)
		if err != nil || old.ProjectID != w.ID {
			return v, m, errors.New("原视频任务不可用，请先恢复原任务")
		}
		job, err := s.dramaJob(m.TaskID)
		if err != nil || job.Production == nil {
			return v, m, errors.New("原视频缺少制作快照")
		}
		items := []DramaProduction{*job.Production}
		if job.Production.Kind == "batch" {
			items = job.Production.Items
		}
		matched := false
		for _, item := range items {
			matched = matched || item.Kind == "video" && item.Entry == m.Entry && item.Duration == p.Duration
		}
		if !matched || m.File != w.ID+"/"+m.TaskID+"-clip-"+v.ShotID+".mp4" {
			return v, m, errors.New("原视频与本镜供应商任务快照不一致，请重新核对")
		}
		if err = s.store.db.QueryRow("SELECT remote_id FROM attempts WHERE id=? AND task_id=? AND status='received'", m.TaskID+":video-"+v.ShotID, m.TaskID).Scan(&v.SourceRemoteID); err != nil || v.SourceRemoteID == "" {
			return v, m, errors.New("原视频没有已接收的供应商任务回执，不能提交编辑")
		}
	}
	ctx, cancel := context.WithTimeout(s.ctx, 20*time.Second)
	defer cancel()
	if cfg.Mode == "mock" {
		err = s.checkClipDuration(ctx, m.File, int(p.Duration))
	} else {
		err = s.validateEditSource(ctx, m.File, int(p.Duration), min, max, w.Aspect)
	}
	if err != nil {
		return v, m, err
	}
	v.SourceVideo, v.SourceHash, v.SourceShotID, v.SourceTaskID = m.File, m.Hash, v.ShotID, m.TaskID
	return v, m, nil
}
