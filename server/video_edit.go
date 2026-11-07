package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"
)

func videoEditModelVersion(model string) string {
	for _, item := range []struct {
		version string
		models  []string
	}{
		{"2.5", []string{"doubao-seedance-2-5-260628", "doubao-seedance-2-5", "doubao-seedance-2.5"}},
		{"2.0", []string{"doubao-seedance-2-0-260128", "doubao-seedance-2-0", "doubao-seedance-2.0", "doubao-seedance-2-0-fast-260128", "doubao-seedance-2-0-fast", "doubao-seedance-2.0-fast", "doubao-seedance-2-0-mini-260615", "doubao-seedance-2-0-mini", "doubao-seedance-2.0-mini"}},
	} {
		for _, known := range item.models {
			if model == known {
				return item.version
			}
		}
	}
	return ""
}

// Only the current shot's immutable supplier receipt may supply the edit video.
// Quotes do not query suppliers or upload arbitrary local files.
func (s *Studio) videoEditInput(p Project, q Shot, cfg Settings) (VideoInput, error) {
	v := VideoInput{ShotID: q.ID, Mode: "edit_video", Images: []VideoImage{}}
	if q.Video == "" || !safeMedia(q.Video) || !q.Approved {
		return v, errors.New("请先确认本镜画面并生成视频，再选择基于原视频修改")
	}
	minDuration := 4.0
	maxDuration := 30.0
	if cfg.Mode != "mock" {
		ep, err := resolveEndpoint(cfg, "video")
		if err != nil {
			return v, err
		}
		version := videoEditModelVersion(ep.Model)
		if version == "" {
			return v, errors.New("当前模型未核实视频编辑能力，请选择 Seedance 2.0/2.5 具体模型，或选择重新生成一版；不会自动切换方式")
		}
		if q.Duration < 4 {
			return v, errors.New("当前模型视频编辑的输出至少4秒，无法保持这一镜的准确时长；请选择重新生成一版")
		}
		if version == "2.0" {
			minDuration, maxDuration = 2, 15
		}
		if q.VideoTaskID == "" {
			return v, errors.New("本镜原视频缺少供应商任务记录，请选择重新生成一版")
		}
		old, err := s.store.task(q.VideoTaskID)
		if err != nil || old.ProjectID != p.ID || old.Status != "completed" {
			return v, errors.New("本镜原视频任务尚未完成或记录不可用，请先恢复原任务")
		}
		matched := false
		for _, source := range old.Input.Shots {
			matched = matched || (source.ID == q.ID && source.Video == q.Video)
		}
		if !matched {
			return v, errors.New("本镜原视频与供应商任务快照不一致，请重新核对")
		}
		v.SourceTaskID = old.ID
		if err = s.store.db.QueryRow("SELECT remote_id FROM attempts WHERE id=? AND task_id=? AND status='received'", old.ID+":video-"+q.ID, old.ID).Scan(&v.SourceRemoteID); err != nil || v.SourceRemoteID == "" {
			return v, errors.New("本镜原视频没有已接收的供应商任务回执，无法作为编辑参考")
		}
	}
	b, err := readVideoFile(s.media(q.Video))
	if err != nil {
		return v, err
	}
	ctx, cancel := context.WithTimeout(s.ctx, 20*time.Second)
	defer cancel()
	if cfg.Mode == "mock" {
		err = s.checkClipDuration(ctx, q.Video, q.Duration)
	} else {
		err = s.validateEditSource(ctx, q.Video, q.Duration, minDuration, maxDuration, projectAspect(p))
	}
	if err != nil {
		return v, err
	}
	v.SourceVideo, v.SourceHash, v.SourceShotID = q.Video, digest(b), q.ID
	return v, nil
}

func (s *Studio) validateEditSource(ctx context.Context, file string, seconds int, minDuration, maxDuration float64, aspect string) error {
	b, err := runCommand(ctx, s.root, "ffprobe", "-v", "error", "-show_entries", "format=duration:stream=codec_type,width,height,r_frame_rate", "-of", "json", s.media(file))
	if err != nil {
		return errors.New("无法检查原视频，请先安装可用的 FFprobe；未调用模型")
	}
	var info struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			Kind   string `json:"codec_type"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
			FPS    string `json:"r_frame_rate"`
		} `json:"streams"`
	}
	if json.Unmarshal(b, &info) != nil {
		return errors.New("原视频信息无效，未调用模型")
	}
	duration, err := strconv.ParseFloat(info.Format.Duration, 64)
	if err != nil || math.IsNaN(duration) || math.IsInf(duration, 0) || duration < minDuration || duration > maxDuration || math.Abs(duration-float64(seconds)) > 0.5 {
		return fmt.Errorf("基于原视频修改的原片须为 %.0f～%.0f 秒，且须与当前镜头%d秒一致；可改选重新生成一版", minDuration, maxDuration, seconds)
	}
	for _, stream := range info.Streams {
		if stream.Kind != "video" {
			continue
		}
		var n, d float64
		if _, err = fmt.Sscanf(stream.FPS, "%f/%f", &n, &d); err != nil || d <= 0 || math.IsNaN(n) || math.IsInf(n, 0) || math.IsNaN(d) || math.IsInf(d, 0) {
			return errors.New("原视频帧率无效，不能提交编辑")
		}
		pixels := int64(stream.Width) * int64(stream.Height)
		if stream.Width < 300 || stream.Width > 6000 || stream.Height < 300 || stream.Height > 6000 || pixels < 407696 || pixels > 8295044 || float64(stream.Width)/float64(stream.Height) < 0.4 || float64(stream.Width)/float64(stream.Height) > 2.5 || n/d < 24 || n/d > 60 {
			return errors.New("原视频尺寸、像素或帧率不符合视频编辑模型要求；可改选重新生成一版")
		}
		expected := 9.0 / 16
		if aspect == "16:9" {
			expected = 16.0 / 9
		}
		if math.Abs(float64(stream.Width)/float64(stream.Height)-expected) > 0.02 {
			return errors.New("原视频画幅与当前项目不一致，请选择重新生成一版")
		}
		return nil
	}
	return errors.New("原视频缺少有效视频轨道，不能提交编辑")
}
