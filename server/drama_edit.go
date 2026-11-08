package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type DramaVerification struct {
	Width            int     `json:"width"`
	Height           int     `json:"height"`
	Duration         float64 `json:"duration"`
	TargetDifference float64 `json:"target_difference"`
	FullDecode       bool    `json:"full_decode"`
	Audio            bool    `json:"audio"`
	Loudness         string  `json:"loudness_lufs"`
	TruePeak         string  `json:"true_peak_dbtp"`
	ContentQuality   string  `json:"content_quality"`
}
type DramaAudio struct {
	Type   string     `json:"type"`
	Media  DramaMedia `json:"media"`
	Start  float64    `json:"start"`
	End    float64    `json:"end"`
	Offset float64    `json:"offset"`
	Gain   float64    `json:"gain"`
	Loop   bool       `json:"loop"`
}
type DramaCut struct {
	Captions []DramaCaption `json:"captions"`
	ID       string         `json:"id"`
	Media    DramaMedia     `json:"media"`
	In       float64        `json:"in"`
	Out      float64        `json:"out"`
	Duration float64        `json:"duration"`
	Reason   string         `json:"reason"`
	Subtitle string         `json:"subtitle"`
	Audio    []DramaAudio   `json:"audio"`
}
type dramaProbe struct {
	Streams []struct {
		Type   string `json:"codec_type"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

func (s *Studio) probeDrama(ctx context.Context, file string) (dramaProbe, error) {
	var p dramaProbe
	b, e := runCommand(ctx, s.root, "ffprobe", "-v", "error", "-show_streams", "-show_format", "-of", "json", s.media(file))
	if e != nil {
		return p, e
	}
	e = json.Unmarshal(b, &p)
	return p, e
}
func (s *Studio) dramaCuts(id string) ([]DramaCut, error) {
	d, e := s.dramaDoc(id, "edit")
	if e != nil {
		return nil, e
	}
	if d.State != "accepted" {
		return nil, errors.New("请先采用当前剪辑单")
	}
	all, e := s.dramaMedias(id)
	if e != nil {
		return nil, e
	}
	script, e := s.dramaDoc(id, "script")
	if e != nil {
		return nil, e
	}
	out := []DramaCut{}
	for _, v := range d.Entries {
		if !strings.HasPrefix(v.ID, "CUT-") {
			continue
		}
		source := strings.SplitN(v.Fields["来源"], " · ", 2)
		if len(source) != 2 {
			return nil, errors.New("CUT来源格式为：MOTION或SHOT标识 · 项目相对路径")
		}
		var media *DramaMedia
		for _, m := range all {
			if m.Path == source[1] && m.Entry == source[0] && m.Approved && m.Current {
				mm := m
				media = &mm
				break
			}
		}
		if media != nil && media.Kind == "video" && !media.Selected {
			return nil, fmt.Errorf("%s 引用旧视频版本，请复核剪辑单并选择当前选用版本", v.ID)
		}
		if media == nil {
			return nil, fmt.Errorf("%s 没有匹配已确认素材", v.ID)
		}
		if e = s.verifyDramaMedia(*media); e != nil {
			return nil, e
		}
		a, _ := strconv.ParseFloat(v.Fields["入点"], 64)
		b, _ := strconv.ParseFloat(v.Fields["出点"], 64)
		cut := DramaCut{ID: v.ID, Media: *media, In: a, Out: b, Duration: b - a, Reason: v.Fields["取舍"], Subtitle: v.Fields["字幕"], Audio: []DramaAudio{}}
		if cut.Duration <= 0 {
			return nil, errors.New("剪辑区间无效")
		}
		if cut.Subtitle == "无" {
			cut.Subtitle = ""
		}
		if cut.Subtitle != "" && !strings.Contains(script.Text, cut.Subtitle) {
			return nil, errors.New("字幕须逐字来自剧本")
		}

		for field, value := range v.Fields {
			if strings.HasPrefix(field, "画面文字") || strings.HasPrefix(field, "字幕 ") {
				cap, e := dramaParseCaption(field, value, script.Text, cut.Duration)
				if e != nil {
					return nil, e
				}
				cut.Captions = append(cut.Captions, cap)
			}
			if field == "运镜" && value != "固定" {
				return nil, errors.New("当前静帧仅支持固定画面")
			}
			if strings.HasPrefix(field, "配音") || strings.HasPrefix(field, "音乐") || strings.HasPrefix(field, "环境声") || strings.HasPrefix(field, "音效") {
				audio, e := s.parseDramaAudio(field, value, all, cut.Duration)
				if e != nil {
					return nil, e
				}
				cut.Audio = append(cut.Audio, audio)
			}
		}

		if cut.Media.Kind != "image" {
			for _, old := range out {
				if old.Media.File == cut.Media.File && cut.In < old.Out && old.In < cut.Out && !strings.Contains(cut.Reason, "重复") {
					return nil, errors.New("同一视频区间重复使用，须明确重复的叙事作用")
				}
			}
		}

		out = append(out, cut)
	}
	if len(out) == 0 {
		return nil, errors.New("剪辑单没有CUT条目")
	}
	motion, err := s.dramaDoc(id, "motion")
	if err != nil {
		return nil, err
	}
	omitted := ""
	for _, line := range strings.Split(d.Text, "\n") {
		if strings.HasPrefix(line, "- 未采用镜头：") {
			omitted += line + "\n"
		}
	}
	for _, entry := range motion.Entries {
		if !strings.HasPrefix(entry.ID, "MOTION-") {
			continue
		}
		found := false
		for _, cut := range out {
			if cut.Media.Entry == entry.ID {
				found = true
			}
		}
		if !found && !strings.Contains(omitted, entry.ID+"（") {
			return nil, fmt.Errorf("镜头%s未进入剪辑，也未明确未采用原因", entry.ID)
		}
	}

	return out, nil
}
func (s *Studio) dramaEditRoutes(g *gin.RouterGroup) {
	g.POST("/works/:id/render", func(c *gin.Context) {
		s.dramaMu.Lock()
		defer s.dramaMu.Unlock()
		id := c.Param("id")
		w, e := s.dramaWork(id)
		if e != nil {
			result(c, nil, e)
			return
		}
		cuts, e := s.dramaCuts(id)
		if e != nil {
			result(c, nil, e)
			return
		}
		inputs, e := s.dramaInputs(id, "edit")
		if e != nil {
			result(c, nil, e)
			return
		}
		d, e := s.dramaDoc(id, "edit")
		if e != nil {
			result(c, nil, e)
			return
		}
		inputs["edit"] = d.Hash
		cfg, e := s.store.settings()
		if e != nil {
			result(c, nil, e)
			return
		}
		j := DramaJob{ID: uid(), WorkID: id, Kind: "render", Inputs: inputs, SkillVersion: dramaVersion, Production: &DramaProduction{Kind: "render", Document: "edit", Cuts: cuts}}
		t, e := s.newDramaTask(w, cfg, j)
		if e == nil {
			s.launchDrama(t.ID)
		}
		result(c, t, e)
	})
}
func (s *Studio) renderDrama(ctx context.Context, t *Task, j *DramaJob) error {
	dir := filepath.Join(s.data, "render", t.ID)
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	concat := []string{}
	total := 0.0
	for index, cut := range j.Production.Cuts {
		if e := s.verifyDramaMedia(cut.Media); e != nil {
			return e
		}
		args := []string{"-v", "error", "-y"}
		image := cut.Media.Kind == "image"
		if image {
			args = append(args, "-loop", "1")
		} else {
			probe, e := s.probeDrama(ctx, cut.Media.File)
			if e != nil {
				return e
			}
			duration, _ := strconv.ParseFloat(probe.Format.Duration, 64)
			if cut.Out > duration+0.02 {
				return errors.New("剪辑出点超过实际素材时长，禁止补帧或截断")
			}
			args = append(args, "-ss", fmt.Sprint(cut.In))
		}
		args = append(args, "-i", s.media(cut.Media.File))
		hasAudio := false
		if !image {
			p, e := s.probeDrama(ctx, cut.Media.File)
			if e != nil {
				return e
			}
			for _, stream := range p.Streams {
				if stream.Type == "audio" {
					hasAudio = true
				}
			}
		}
		audioIndex := 0
		if !hasAudio {
			audioIndex = 1
			args = append(args, "-f", "lavfi", "-i", "anullsrc=r=48000:cl=stereo")
		}
		filters := []string{fmt.Sprintf("[0:v]%s,fps=24,setsar=1[v]", scaleFilter(t.Input))}
		audioLabels := []string{}
		base := fmt.Sprintf("[%d:a]atrim=duration=%g,asetpts=PTS-STARTPTS[a0]", audioIndex, cut.Duration)
		filters = append(filters, base)
		audioLabels = append(audioLabels, "[a0]")

		filters = append(filters, strings.Join(audioLabels, "")+fmt.Sprintf("amix=inputs=%d:duration=first:normalize=0[a]", len(audioLabels)))
		name := fmt.Sprintf("segment-%04d.mp4", index)
		args = append(args, "-filter_complex", strings.Join(filters, ";"), "-map", "[v]", "-map", "[a]", "-t", fmt.Sprint(cut.Duration), "-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", "-c:a", "aac", filepath.Join(dir, name))
		if _, e := s.runDramaFFmpeg(ctx, dir, args...); e != nil {
			return e
		}
		concat = append(concat, "file '"+name+"'")
		total += cut.Duration
	}
	if e := os.WriteFile(filepath.Join(dir, "concat.txt"), []byte(strings.Join(concat, "\n")), 0600); e != nil {
		return e
	}

	width, height := frameSize(t.Input)
	captionText := dramaCaptions(j.Production.Cuts, width, height)
	if e := os.WriteFile(filepath.Join(dir, "captions.ass"), []byte(captionText), 0600); e != nil {
		return e
	}
	file := t.ProjectID + "/" + t.ID + "-film.mp4"
	if e := os.MkdirAll(filepath.Dir(s.media(file)), 0700); e != nil {
		return e
	}
	args := []string{"-v", "error", "-y", "-f", "concat", "-safe", "1", "-i", "concat.txt"}
	audioArgs, audioFilter, err := s.dramaTimelineAudio(ctx, j.Production.Cuts, total)
	if err != nil {
		return err
	}
	args = append(args, audioArgs...)
	// Audio args can carry -stream_loop, so count actual input declarations.
	inputStart := 1
	for _, arg := range audioArgs {
		if arg == "-i" {
			inputStart++
		}
	}
	layers, videoFilter, videoMap, err := dramaCaptionLayers(j.Production.Cuts, dir, width, height, inputStart)
	if err != nil {
		return err
	}
	args = append(args, layers...)
	if videoFilter != "" {
		audioFilter += ";" + videoFilter
	}
	args = append(args, "-filter_complex", audioFilter, "-map", videoMap, "-map", "[mix]", "-t", fmt.Sprint(total), "-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac", "-movflags", "+faststart", s.media(file))

	if _, e := s.runDramaFFmpeg(ctx, dir, args...); e != nil {
		return e
	}

	p, e := s.probeDrama(ctx, file)
	if e != nil {
		return e
	}
	seconds, _ := strconv.ParseFloat(p.Format.Duration, 64)
	if absFloat(seconds-total) > 0.2 {
		return errors.New("成片实际时长与剪辑单不符")
	}
	hasVideo, hasAudio := false, false
	for _, stream := range p.Streams {
		if stream.Type == "video" {
			hasVideo = stream.Width == width && stream.Height == height
		}
		if stream.Type == "audio" {
			hasAudio = true
		}
	}
	if !hasVideo || !hasAudio {
		return errors.New("成片尺寸或音轨检查失败")
	}
	if _, e = runCommand(ctx, s.root, "ffmpeg", "-v", "error", "-xerror", "-i", s.media(file), "-f", "null", "-"); e != nil {
		return e
	}
	verify := &DramaVerification{Width: width, Height: height, Duration: seconds, TargetDifference: seconds - float64(t.Input.Duration), FullDecode: true, Audio: true, ContentQuality: "not_assessed"}
	ffmpeg, err := localTool("ffmpeg")
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-i", s.media(file), "-af", "loudnorm=I=-16:TP=-1.5:LRA=11:print_format=json", "-f", "null", "-")
	configureProcess(cmd)
	raw, err := cmd.CombinedOutput()
	if err != nil {
		return errors.New("成片响度测量失败")
	}
	start := strings.LastIndex(string(raw), "{")
	end := strings.LastIndex(string(raw), "}")
	var loud struct {
		I  string `json:"input_i"`
		TP string `json:"input_tp"`
	}
	if start < 0 || end < start || json.Unmarshal(raw[start:end+1], &loud) != nil {
		return errors.New("成片响度测量结果无法解析")
	}
	verify.Loudness = loud.I
	verify.TruePeak = loud.TP

	b, e := os.ReadFile(s.media(file))
	if e != nil {
		return e
	}
	m := DramaMedia{Verification: verify, ID: t.ID, WorkID: t.ProjectID, Entry: strings.Split(s.dramaMediaPath(t.ProjectID, ""), "/")[1] + "-FILM", Kind: "film", File: file, Path: s.dramaMediaPath(t.ProjectID, "成片/"+filepath.Base(file)), Hash: digest(b), TaskID: t.ID, SourceHash: j.Inputs["edit"], Duration: seconds, Mock: t.Mode == "mock"}
	return s.saveDramaMedia(m)
}
func dramaSRT(seconds float64) string {
	ms := int(seconds*1000 + 0.5)
	return fmt.Sprintf("%02d:%02d:%02d,%03d", ms/3600000, ms/60000%60, ms/1000%60, ms%1000)
}

func (s *Studio) runDramaFFmpeg(ctx context.Context, dir string, args ...string) ([]byte, error) {
	b, e := runCommand(ctx, dir, "ffmpeg", args...)
	if e != nil {
		detail := strings.ReplaceAll(string(b), s.data, "[本机数据]")
		detail = strings.ReplaceAll(detail, s.root, "[程序目录]")
		if len(detail) > 4000 {
			detail = detail[len(detail)-4000:]
		}
		return b, fmt.Errorf("%w：%s", e, detail)
	}
	return b, nil
}
