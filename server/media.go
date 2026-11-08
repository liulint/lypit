package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func (s *Studio) writeMedia(pid, name string, b []byte) (string, error) {
	rel := filepath.ToSlash(filepath.Join(pid, name))
	if filepath.Base(name) != name || strings.Contains(pid, "..") || !safeMedia(rel) {
		return "", errors.New("媒体输出路径无效")
	}
	dest := s.media(rel)
	if e := os.MkdirAll(filepath.Dir(dest), 0700); e != nil {
		return "", e
	}
	tmp := dest + ".part"
	if e := os.WriteFile(tmp, b, 0600); e != nil {
		return "", e
	}
	if e := os.Rename(tmp, dest); e != nil {
		return "", e
	}
	return rel, nil
}
func (s *Studio) makeImage(ctx context.Context, t *Task, key, prompt string, refs []Image, sheet bool) (Image, error) {
	if e := ctx.Err(); e != nil {
		return Image{}, e
	}
	var b []byte
	var e error
	if t.Mode == "mock" {
		b = mockPNG(key, sheet, landscapeImage(t.Input, key))
	} else {
		b, e = s.remoteImage(ctx, t, key, prompt, refs, sheet)
		if e != nil {
			return Image{}, e
		}
	}
	cfg, _, e := image.DecodeConfig(bytes.NewReader(b))
	if e != nil || cfg.Width < 100 || cfg.Height < 100 || cfg.Width > 4096 || cfg.Height > 4096 {
		return Image{}, errors.New("图片无法解码或尺寸超限")
	}
	im, _, e := image.Decode(bytes.NewReader(b))
	if e != nil {
		return Image{}, errors.New("图片内容不完整")
	}
	if sheet && (cfg.Width != cfg.Height || cfg.Width%2 != 0) {
		return Image{}, errors.New("设定图必须为正方形偶数尺寸，禁止静默缩放")
	}
	var normalized bytes.Buffer
	if e = png.Encode(&normalized, im); e != nil {
		return Image{}, e
	}
	b = normalized.Bytes()
	rel, e := s.writeMedia(t.ProjectID, t.ID+"-"+key+".png", b)
	return Image{File: rel, Hash: digest(b)}, e
}
func (s *Studio) cropSheet(sheet Image) ([]Image, error) {
	b, e := os.ReadFile(s.media(sheet.File))
	if e != nil {
		return nil, e
	}
	im, _, e := image.Decode(bytes.NewReader(b))
	if e != nil {
		return nil, e
	}
	bounds := im.Bounds()
	w, h := bounds.Dx()/2, bounds.Dy()/2
	out := []Image{}
	for i := 0; i < 4; i++ {
		dest := image.NewRGBA(image.Rect(0, 0, w, h))
		draw.Draw(dest, dest.Bounds(), im, image.Pt((i%2)*w, (i/2)*h), draw.Src)
		var buf bytes.Buffer
		if e = png.Encode(&buf, dest); e != nil {
			return nil, e
		}
		file := strings.TrimSuffix(sheet.File, ".png") + fmt.Sprintf("-view%d.png", i)
		rel, e := s.writeMedia(filepath.Dir(file), filepath.Base(file), buf.Bytes())
		if e != nil {
			return nil, e
		}
		out = append(out, Image{File: rel, Hash: digest(buf.Bytes())})
	}
	return out, nil
}
func mockPNG(seed string, sheet, landscape bool) []byte {
	w, h := 540, 960
	if landscape {
		w, h = h, w
	}
	if sheet {
		w, h = 1024, 1024
	}
	im := image.NewRGBA(image.Rect(0, 0, w, h))
	sum := digest([]byte(seed))
	base := uint8(sum[0])
	bg := color.RGBA{219 + base%20, 224 + base%15, 211 + base%25, 255}
	draw.Draw(im, im.Bounds(), &image.Uniform{bg}, image.Point{}, draw.Src)
	rect := func(x, y, r, b int, c color.RGBA) {
		draw.Draw(im, image.Rect(x, y, r, b), &image.Uniform{c}, image.Point{}, draw.Src)
	}
	circle := func(cx, cy, r int, c color.RGBA) {
		for y := -r; y <= r; y++ {
			for x := -r; x <= r; x++ {
				if x*x+y*y <= r*r {
					im.Set(cx+x, cy+y, c)
				}
			}
		}
	}
	panel := func(x, y, pw, ph, view int) {
		rect(x+pw/8, y+ph/8, x+pw*7/8, y+ph*4/5, color.RGBA{190, 204, 188, 255})
		rect(x+pw/8, y+ph/8, x+pw*7/8, y+ph/8+5, color.RGBA{110, 137, 116, 255})
		for i := 1; i < 4; i++ {
			rect(x+pw/8+i*pw*3/16, y+ph/8, x+pw/8+i*pw*3/16+3, y+ph*4/5, color.RGBA{219, 229, 211, 255})
		}
		skin := color.RGBA{213, 178, 145, 255}
		cx, cy := x+pw/2, y+ph/3
		radius := pw / 12
		if view == 3 {
			radius = pw / 5
			cy = y + ph/2
		}
		circle(cx, cy, radius+3, color.RGBA{48, 57, 51, 255})
		circle(cx, cy+radius/3, radius, skin)
		if view != 3 {
			rect(cx-pw/10, cy+radius, cx+pw/10, y+ph*2/3, color.RGBA{245, 240, 218, 255})
			rect(cx-pw/12, y+ph*2/3, cx-4, y+ph*7/8, color.RGBA{65, 80, 72, 255})
			rect(cx+4, y+ph*2/3, cx+pw/12, y+ph*7/8, color.RGBA{65, 80, 72, 255})
		}
		rect(x+pw/12, y+ph*15/16, x+pw*11/12, y+ph*15/16+3, color.RGBA{82, 108, 88, 255})
		for i := 0; i < view+1; i++ {
			rect(x+pw/12+i*12, y+ph/16, x+pw/12+6+i*12, y+ph/16+6, color.RGBA{61, 92, 72, 255})
		}
	}
	if sheet {
		for i := 0; i < 4; i++ {
			panel(i%2*w/2, i/2*h/2, w/2, h/2, i)
		}
		rect(w/2-2, 0, w/2+2, h, color.RGBA{255, 255, 255, 255})
		rect(0, h/2-2, w, h/2+2, color.RGBA{255, 255, 255, 255})
	} else {
		panel(0, 0, w, h, int(base)%3)
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, im)
	return buf.Bytes()
}
func runCommand(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	path := name
	if name == "ffmpeg" || name == "ffprobe" {
		resolved, err := localTool(name)
		if err != nil {
			return nil, fmt.Errorf("%s 未安装，请在服务配置页安装本机制作工具", name)
		}
		path = resolved
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = dir
	configureProcess(cmd)
	cmd.WaitDelay = 3 * time.Second
	b, e := cmd.Output()
	if e != nil {
		if ee, ok := e.(*exec.ExitError); ok {
			b = append(b, ee.Stderr...)
		}
		return b, fmt.Errorf("%s 执行失败：%w", name, e)
	}
	return b, nil
}
func (s *Studio) makeVideo(ctx context.Context, t *Task, q Shot) (string, error) {
	if q.Selected < 0 || q.Selected >= len(q.Candidates) {
		return "", errors.New("镜头选版无效")
	}
	if e := s.verifyImage(q.Candidates[q.Selected]); e != nil {
		return "", e
	}
	if t.Mode != "mock" {
		return s.remoteVideo(ctx, t, q)
	}
	if input, ok := inputFor(*t, q.ID); ok && input.Mode == "edit_video" {
		b, e := readVideoFile(s.media(input.SourceVideo))
		if e != nil || digest(b) != input.SourceHash {
			return "", errors.New("本镜原视频已变化，请重新报价")
		}
	}
	if input, ok := inputFor(*t, q.ID); ok && input.Mode == "continue" {
		b, e := readVideoFile(s.media(input.SourceVideo))
		if e != nil || digest(b) != input.SourceHash {
			return "", errors.New("上一镜已变化，请重新报价")
		}
		file := t.ProjectID + "/" + t.ID + "-mock-start-" + q.ID + ".png"
		if _, e = runCommand(ctx, s.root, "ffmpeg", "-v", "error", "-y", "-sseof", "-0.1", "-i", s.media(input.SourceVideo), "-frames:v", "1", s.media(file)); e != nil {
			return "", e
		}
		b, e = os.ReadFile(s.media(file))
		if e != nil {
			return "", e
		}
		q.Candidates = []Image{{File: file, Hash: digest(b)}}
		q.Selected = 0
	}
	rel := t.ProjectID + "/" + t.ID + "-clip-" + q.ID + ".mp4"
	if e := os.MkdirAll(filepath.Dir(s.media(rel)), 0700); e != nil {
		return "", e
	}
	_, e := runCommand(ctx, s.root, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-loop", "1", "-i", s.media(q.Candidates[q.Selected].File), "-f", "lavfi", "-i", "anullsrc=r=48000:cl=stereo", "-t", fmt.Sprint(q.Duration), "-vf", scaleFilter(t.Input), "-r", "24", "-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest", "-movflags", "+faststart", s.media(rel))
	return rel, e
}
func (s *Studio) makeVoice(ctx context.Context, t *Task) (string, error) {
	if t.Mode == "mock" {
		rel := t.ProjectID + "/" + t.ID + "-voice.wav"
		_, e := runCommand(ctx, s.root, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-f", "lavfi", "-i", "anullsrc=r=48000:cl=stereo", "-t", fmt.Sprint(projectDuration(t.Input)), s.media(rel))
		return rel, e
	}
	lines := []string{}
	for _, q := range t.Input.Shots {
		lines = append(lines, q.Narration)
	}
	ep, e := resolveEndpoint(t.Config, "voice")
	if e != nil {
		return "", e
	}
	body := map[string]any{"model": ep.Model, "voice": "alloy", "input": strings.Join(lines, "\n"), "response_format": "wav"}
	b, e := s.paid(ctx, t, "voice", ep, "/audio/speech", "application/json", jsonBytes(body))
	if e != nil {
		return "", e
	}
	return s.writeMedia(t.ProjectID, t.ID+"-voice.wav", b)
}
func (s *Studio) render(ctx context.Context, t *Task) (string, error) {
	p := t.Input
	if e := validateShotPlan(p); e != nil {
		return "", e
	}
	dir := filepath.Join(s.data, "productions", t.ID)
	if e := os.MkdirAll(dir, 0700); e != nil {
		return "", e
	}
	// Normalize every source independently. The confirmed input order is immutable.
	list := []string{}
	captions := []string{}
	offset := 0
	for i, q := range p.Shots {
		if !videoDependencyCurrent(p, i) {
			return "", errors.New("视频参考或续接来源已变化，请先重新生成后续镜头")
		}
		if e := ctx.Err(); e != nil {
			return "", e
		}
		clip := filepath.Join(dir, fmt.Sprintf("part-%02d.mp4", i))
		audioArgs := []string{"-an"}
		if dialogueMode(p) {
			if e := s.requireAudio(ctx, q.Video); e != nil {
				return "", e
			}
			audioArgs = []string{"-map", "0:v:0", "-map", "0:a:0", "-af", "aresample=48000,apad", "-ac", "2", "-c:a", "aac"}
		}
		args := []string{"-hide_banner", "-loglevel", "error", "-y", "-i", s.media(q.Video), "-t", fmt.Sprint(q.Duration), "-vf", scaleFilter(p) + ",tpad=stop_mode=clone:stop_duration=10"}
		args = append(args, audioArgs...)
		args = append(args, "-r", "24", "-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", clip)
		_, e := runCommand(ctx, dir, "ffmpeg", args...)
		if e != nil {
			return "", e
		}
		list = append(list, fmt.Sprintf("file 'part-%02d.mp4'", i))
		if strings.TrimSpace(q.Narration) != "" {
			captions = append(captions, fmt.Sprintf("%d\n%s --> %s\n%s\n", len(captions)+1, srtTime(offset), srtTime(offset+q.Duration), strings.ReplaceAll(q.Narration, "\n", " ")))
		}
		offset += q.Duration
		if e = s.progress(t, (i+1)*48/len(p.Shots), "统一镜头规格"); e != nil {
			return "", e
		}
	}
	if e := os.WriteFile(filepath.Join(dir, "concat.txt"), []byte(strings.Join(list, "\n")), 0600); e != nil {
		return "", e
	}
	rel := p.ID + "/" + t.ID + "-final.mp4"
	partial := p.ID + "/" + t.ID + "-final.part.mp4"
	if e := os.MkdirAll(filepath.Dir(s.media(rel)), 0700); e != nil {
		return "", e
	}
	if e := s.progress(t, 65, "FFmpeg 合成并导出（不请求模型）"); e != nil {
		return "", e
	}
	args := []string{"-hide_banner", "-loglevel", "error", "-y", "-f", "concat", "-safe", "1", "-i", "concat.txt"}
	if dialogueMode(p) {
		args = append(args, "-map", "0:v:0", "-map", "0:a:0")
	} else {
		args = append(args, "-i", s.media(p.Voice), "-map", "0:v:0", "-map", "1:a:0")
	}
	args = append(args, "-af", "apad", "-t", fmt.Sprint(offset), "-c:v", "copy", "-c:a", "aac", "-movflags", "+faststart", s.media(partial))
	_, e := runCommand(ctx, dir, "ffmpeg", args...)
	if e != nil {
		return "", e
	}
	if _, e = s.writeMedia(p.ID, t.ID+"-captions.srt", []byte(strings.Join(captions, "\n"))); e != nil {
		return "", e
	}
	if e = s.progress(t, 85, "检查成片尺寸、时长、音轨与完整解码"); e != nil {
		return "", e
	}
	if e = s.validateFilm(ctx, partial, p); e != nil {
		return "", e
	}
	if e = os.Rename(s.media(partial), s.media(rel)); e != nil {
		return "", e
	}
	return rel, nil
}
func srtTime(n int) string { return fmt.Sprintf("%02d:%02d:%02d,000", n/3600, n/60%60, n%60) }
func (s *Studio) validateFilm(ctx context.Context, file string, p Project) error {
	b, e := runCommand(ctx, s.root, "ffprobe", "-v", "error", "-show_streams", "-show_format", "-of", "json", s.media(file))
	if e != nil {
		return e
	}
	var info struct {
		Streams []struct {
			Type   string `json:"codec_type"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if json.Unmarshal(b, &info) != nil {
		return errors.New("成片探测失败")
	}
	video, audio := false, false
	w, h := frameSize(p)
	duration := float64(projectDuration(p))
	for _, v := range info.Streams {
		if v.Type == "video" && v.Width == w && v.Height == h {
			video = true
		}
		if v.Type == "audio" {
			audio = true
		}
	}
	var d float64
	_, _ = fmt.Sscan(info.Format.Duration, &d)
	if !video || !audio || d < duration-0.5 || d > duration+0.5 {
		return errors.New("成片尺寸、音轨或时长未通过检查")
	}
	_, e = runCommand(ctx, s.root, "ffmpeg", "-v", "error", "-i", s.media(file), "-f", "null", "-")
	return e
}

// Audio is mandatory for dialogue; recovery keeps the original paid receipt.
func (s *Studio) requireAudio(ctx context.Context, file string) error {
	b, e := runCommand(ctx, "", "ffprobe", "-v", "error", "-select_streams", "a:0", "-show_entries", "stream=codec_type", "-of", "csv=p=0", s.media(file))
	if e != nil {
		return e
	}
	if strings.TrimSpace(string(b)) != "audio" {
		return errors.New("角色对白视频没有音轨，请核对原供应商任务与模型同步音频能力；不会自动重新生成")
	}
	return nil
}

func (s *Studio) checkClipDuration(ctx context.Context, file string, seconds int) error {
	b, e := runCommand(ctx, s.root, "ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "csv=p=0", s.media(file))
	if e != nil {
		return e
	}
	var n float64
	if _, e = fmt.Sscan(strings.TrimSpace(string(b)), &n); e != nil || n < float64(seconds)-0.5 || n > float64(seconds)+0.5 {
		return errors.New("供应商输出时长与当前镜头不符；已保留原任务，请核对，不自动重提")
	}
	return nil
}
