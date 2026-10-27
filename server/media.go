package main

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func command(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var out, errbuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errbuf
	if e := cmd.Run(); e != nil {
		detail := errbuf.String()
		if len(detail) > 1200 {
			detail = detail[len(detail)-1200:]
		}
		return out.Bytes(), fmt.Errorf("%s 执行失败：%s", name, detail)
	}
	return out.Bytes(), nil
}

type Probe struct {
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
	Streams []struct {
		Type   string `json:"codec_type"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
		Frames string `json:"nb_frames"`
	} `json:"streams"`
}

func (s *Studio) probe(ctx context.Context, path string) (Probe, error) {
	raw, e := command(ctx, "", "ffprobe", "-v", "error", "-show_format", "-show_streams", "-of", "json", path)
	var p Probe
	if e != nil {
		return p, e
	}
	e = json.Unmarshal(raw, &p)
	return p, e
}
func (s *Studio) mockAsset(ctx context.Context, pid, key, kind, prompt string, duration int) (string, error) {
	ext := "json"
	if kind == "image" {
		ext = "png"
	}
	if kind == "video" {
		ext = "mp4"
	}
	if kind == "tts" {
		ext = "wav"
	}
	rel := filepath.Join(pid, key, "result."+ext)
	path := s.abs(rel)
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return "", e
	}
	var e error
	switch kind {
	case "image":
		e = atomicWrite(path, mockPNG(prompt))
	case "vision":
		e = atomicWrite(path, mockReviewResponse(prompt))
	case "video":
		img := filepath.Join(filepath.Dir(path), "frame.png")
		if e = atomicWrite(img, mockPNG(prompt)); e != nil {
			return "", e
		}
		_, e = command(ctx, "", "ffmpeg", "-v", "error", "-y", "-loop", "1", "-i", img, "-t", strconv.Itoa(duration), "-vf", "scale=496:864,zoompan=z='min(zoom+0.00015,1.04)':d=1:s=496x864:fps=24", "-an", "-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", "-threads", "1", path)
	case "tts":
		_, e = command(ctx, "", "ffmpeg", "-v", "error", "-y", "-f", "lavfi", "-i", "anullsrc=r=48000:cl=stereo", "-t", strconv.Itoa(duration), "-c:a", "pcm_s16le", path)
	default:
		e = errors.New("模拟文本通过本地故事模板生成")
	}
	if e != nil {
		return "", e
	}
	zero := 0.0
	return rel, s.finishAttempt(pid, key, rel, json.RawMessage(`{"mock":true}`), &zero, "模拟素材；旁白为静音占位，不是真实模型效果")
}
func mockPNG(seed string) []byte {
	w, h := 360, 640
	im := image.NewRGBA(image.Rect(0, 0, w, h))
	hs := hash(seed)
	v := int(hs[0])
	for y := 0; y < h; y++ {
		t := float64(y) / float64(h)
		c := color.RGBA{uint8(25 + int(30*t)), uint8(40 + int(45*t) + v%20), uint8(63 + int(55*t)), 255}
		draw.Draw(im, image.Rect(0, y, w, y+1), &image.Uniform{c}, image.Point{}, draw.Src)
	}
	circle := func(cx, cy, r int, c color.RGBA) {
		for y := cy - r; y < cy+r; y++ {
			for x := cx - r; x < cx+r; x++ {
				if (x-cx)*(x-cx)+(y-cy)*(y-cy) < r*r {
					im.Set(x, y, c)
				}
			}
		}
	}
	circle(280, 90, 35, color.RGBA{240, 202, 139, 255})
	for i := 0; i < 8; i++ {
		height := 90 + (int(hs[i])%7)*19
		rect := image.Rect(i*49, 400-height, i*49+42, 440)
		draw.Draw(im, rect, &image.Uniform{color.RGBA{25, 38, 51, 255}}, image.Point{}, draw.Src)
		for y := rect.Min.Y + 15; y < 410; y += 24 {
			for x := rect.Min.X + 9; x < rect.Max.X-5; x += 15 {
				draw.Draw(im, image.Rect(x, y, x+5, y+9), &image.Uniform{color.RGBA{211, 162, 99, 255}}, image.Point{}, draw.Src)
			}
		}
	}
	draw.Draw(im, image.Rect(0, 440, w, h), &image.Uniform{color.RGBA{34, 47, 55, 255}}, image.Point{}, draw.Src)
	// Recurring simple cat silhouette for the explicitly labelled local demo.
	fur := color.RGBA{203, 153, 98, 255}
	circle(180, 465, 47, fur)
	circle(180, 404, 38, fur)
	for y := 350; y < 390; y++ {
		span := (y - 350) / 2
		for x := 148 - span; x < 148+span; x++ {
			im.Set(x, y, fur)
		}
		for x := 212 - span; x < 212+span; x++ {
			im.Set(x, y, fur)
		}
	}
	circle(167, 401, 5, color.RGBA{20, 35, 40, 255})
	circle(194, 401, 5, color.RGBA{20, 35, 40, 255})
	circle(180, 417, 4, color.RGBA{129, 78, 72, 255})
	draw.Draw(im, image.Rect(145, 434, 217, 445), &image.Uniform{color.RGBA{104, 174, 162, 255}}, image.Point{}, draw.Src)
	var out bytes.Buffer
	_ = png.Encode(&out, im)
	return out.Bytes()
}
func mockBoard(p Project, instruction, shotID string) Board {
	if prev := board(p); prev != nil {
		b := clone(*prev)
		for i := range b.Shots {
			q := &b.Shots[i]
			if shotID != "" && q.ID != shotID {
				continue
			}
			q.ImagePrompt += "。修改意见：" + instruction
			q.VideoPrompt += "。修改意见：" + instruction
			q.Image = ""
			q.Video = ""
			if shotID != "" {
				break
			}
		}
		return b
	}
	titles := []string{"出发", "陌生街头", "发现线索", "走进小巷", "短暂迷路", "遇到朋友", "一起前行", "灯光亮起", "愿望抵达", "新的开始"}
	narr := []string{"小猫第一次离开熟悉的窗台，带着一个小小的愿望。", "城市很大，每一条路都通向未知的地方。", "一束温暖的光，让它停下了脚步。", "它鼓起勇气，走进那条安静的小巷。", "拐过街角，熟悉的方向突然不见了。", "就在这时，一个新朋友出现在眼前。", "原来有人陪伴，陌生的路也不再漫长。", "傍晚的灯一盏盏亮起，照亮了回家的方向。", "它终于发现，想寻找的温暖就在身边。", "明天还有新的旅程，而今天已经足够美好。"}
	b := Board{Title: "小猫与城市的光 · 模拟分镜", Synopsis: "本地演示模板，用于验证审核与合成流程。真实故事将由已配置的文本模型根据你的想法生成。想法：" + p.Idea, Characters: []Character{{ID: "c1", Name: "小猫", Description: "橘色虎斑猫，绿色围巾，圆眼睛，保持相同毛色和体型"}}}
	for i, title := range titles {
		transition := "cut"
		if i == 3 || i == 7 {
			transition = "dissolve"
		}
		b.Shots = append(b.Shots, Shot{ID: fmt.Sprintf("s%d", i+1), Title: title, Scene: "温暖的城市街道", Action: title, Characters: []string{"c1"}, References: []string{}, Duration: 6, Narration: narr[i], ImagePrompt: "电影质感竖屏摄影，参考角色图保持橘色虎斑猫与绿色围巾；" + title + "，自然光线，明确主体，真实空间，无字幕。", VideoPrompt: "参考图中的橘色小猫，" + title + "。一个自然连续的动作，镜头轻缓跟随，保持毛色服装，无文字无声音。", Transition: transition})
	}
	return b
}
func (s *Studio) cliPath() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seedanceBin != "" {
		return s.seedanceBin, nil
	}
	sourceHash := bytes.Buffer{}
	sources, err := filepath.Glob(filepath.Join(s.root, "tools", "seedance", "*.go"))
	if err != nil {
		return "", err
	}
	sources = append(sources, filepath.Join(s.root, "tools", "seedance", "go.mod"))
	for _, file := range sources {
		b, err := os.ReadFile(file)
		if err != nil {
			return "", err
		}
		sourceHash.Write(b)
	}
	path := filepath.Join(s.data, "bin", "seedance-"+hash(sourceHash.String())[:16])
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return "", e
	}
	if _, e := os.Stat(path); e != nil {
		if _, e = command(s.ctx, filepath.Join(s.root, "tools", "seedance"), "go", "build", "-o", path, "."); e != nil {
			return "", e
		}
	}
	s.seedanceBin = path
	return path, nil
}
func (s *Studio) cliArgs(m Model, prompt string, refs []string, duration int, dir string) ([]string, error) {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	file := filepath.Join(dir, "prompt.txt")
	if e := atomicWrite(file, []byte(prompt)); e != nil {
		return nil, e
	}
	args := []string{"-json", "-prompt-file", file, "-model", m.ID, "-resolution", "480p", "-audio=false", "-duration", strconv.Itoa(duration), "-ratio", "9:16", "-count", "1", "-out", filepath.Join(dir, "seedance")}
	if m.PriceModel != "" {
		args = append(args, "-price-model", m.PriceModel)
	}
	for _, ref := range refs {
		args = append(args, "-image", s.abs(ref))
	}
	return args, nil
}
func (s *Studio) cliQuote(ctx context.Context, p Provider, m Model, prompt string, refs []string, duration int, dir string) (map[string]any, error) {
	bin, e := s.cliPath()
	if e != nil {
		return nil, e
	}
	args, e := s.cliArgs(m, prompt, refs, duration, dir)
	if e != nil {
		return nil, e
	}
	args = append(args, "-quote")
	raw, e := command(ctx, "", bin, args...)
	if e != nil {
		return nil, e
	}
	var q map[string]any
	e = json.Unmarshal(raw, &q)
	return q, e
}
func (s *Studio) seedance(ctx context.Context, pid, key string, p Provider, m Model, prompt string, refs []string, duration int, dir string, recovering bool) (string, error) {
	bin, e := s.cliPath()
	if e != nil {
		return "", e
	}
	args, e := s.cliArgs(m, prompt, refs, duration, dir)
	if e != nil {
		return "", e
	}
	record := filepath.Join(dir, "seedance", "run.json")
	if _, e = os.Stat(record); e == nil {
		args = []string{"-json", "-resume", record}
	} else {
		if recovering {
			return "", errors.New("上次提交结果未知且没有CLI记录，请先核对方舟控制台；不会重新提交")
		}
		// Confirmation is bound to the actual per-shot CLI quote shown in the accepted video batch.
		project, err := s.store.project(pid)
		if err != nil {
			return "", err
		}
		s.store.mu.Lock()
		sq := s.store.state.Quotes[project.ActiveQuote]
		s.store.mu.Unlock()
		confirm := ""
		for _, spec := range sq.Quote.Requests {
			if spec.Kind == "video" && spec.Prompt == prompt && spec.Duration == duration && hash(spec.References) == hash(refs) {
				raw, _ := json.Marshal(spec.Parameters)
				var q map[string]any
				_ = json.Unmarshal(raw, &q)
				confirm, _ = q["confirmation_id"].(string)
			}
		}
		if confirm == "" {
			return "", errors.New("没有匹配的已确认Seedance报价")
		}
		args = append(args, "-confirm", confirm)
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	credential, err := s.providerKey(p)
	if err != nil {
		return "", err
	}
	cmd.Env = append(os.Environ(), "ARK_API_KEY="+credential)
	raw, e := cmd.Output()
	if e != nil {
		return "", errors.New("Seedance 未完成；记录已保留，恢复仅查询已知任务，请检查供应商控制台与本地记录")
	}
	var result struct {
		Record struct {
			Tasks []struct {
				ID    string          `json:"id"`
				State string          `json:"status"`
				File  string          `json:"downloaded_file"`
				Usage json.RawMessage `json:"usage"`
				Fee   *float64        `json:"official_estimated_yuan"`
			} `json:"tasks"`
		} `json:"record"`
	}
	if e = json.Unmarshal(raw, &result); e != nil || len(result.Record.Tasks) != 1 {
		return "", errors.New("Seedance 返回记录无效")
	}
	t := result.Record.Tasks[0]
	if t.State != "succeeded" || t.File == "" {
		return "", errors.New("Seedance 任务尚未成功下载")
	}
	data, e := os.ReadFile(filepath.Join(dir, "seedance", filepath.Base(t.File)))
	if e != nil {
		return "", e
	}
	rel := filepath.Join(pid, key, "result.mp4")
	if e = atomicWrite(s.abs(rel), data); e != nil {
		return "", e
	}
	return rel, s.finishAttempt(pid, key, rel, t.Usage, t.Fee, "Seedance任务ID："+t.ID+"；按实际completion_tokens估算，非账户账单")
}
func (s *Studio) render(ctx context.Context, pid string, b Board, music string) (string, error) {
	renderName := fmt.Sprintf("render-v%d-%s", b.Version, hash([]any{b, music, "render-v2"})[:12])
	dir := filepath.Join(s.data, "media", pid, renderName)
	finalRel := filepath.Join(pid, renderName, "final.mp4")
	if report, err := os.ReadFile(filepath.Join(dir, "validation.json")); err == nil {
		var saved struct {
			Black bool `json:"black_frame_warning"`
		}
		if json.Unmarshal(report, &saved) == nil && !saved.Black {
			if _, err = os.Stat(s.abs(finalRel)); err == nil {
				return finalRel, nil
			}
		}
	}
	if e := os.MkdirAll(dir, 0700); e != nil {
		return "", e
	}
	normalized := []string{}
	duration := 0.0
	captions := []string{}
	num := 1
	for i, q := range b.Shots {
		voice, e := s.probe(ctx, s.abs(q.Voice))
		if e != nil {
			return "", e
		}
		vd, _ := strconv.ParseFloat(voice.Format.Duration, 64)
		if vd <= 0 {
			return "", errors.New("旁白时长无效")
		}
		rate := math.Max(1, vd/float64(q.Duration))
		if rate > 1.25 {
			return "", fmt.Errorf("镜头%s旁白过长，请缩短文字后重新确认生成", q.ID)
		}
		clip := filepath.Join(dir, fmt.Sprintf("clip-%02d.mp4", i))
		af := fmt.Sprintf("atempo=%.6f,apad,atrim=duration=%d,asetpts=PTS-STARTPTS", rate, q.Duration)
		captionFiles, err := captionImages(dir, i, q.Narration)
		if err != nil {
			return "", err
		}
		clipArgs := []string{"-v", "error", "-y", "-i", s.abs(q.Video), "-i", s.abs(q.Voice)}
		for _, file := range captionFiles {
			clipArgs = append(clipArgs, "-i", file)
		}
		graph := fmt.Sprintf("[0:v]scale=486:864:force_original_aspect_ratio=increase,crop=486:864,setsar=1,fps=24,tpad=stop_mode=clone:stop_duration=%d,trim=duration=%d,setpts=PTS-STARTPTS[v0];[1:a]%s[a]", q.Duration, q.Duration, af)
		envelope := math.Min(vd/rate, float64(q.Duration))
		for j := range captionFiles {
			start := float64(j) * envelope / float64(len(captionFiles))
			end := float64(j+1) * envelope / float64(len(captionFiles))
			graph += fmt.Sprintf(";[v%d][%d:v]overlay=x=0:y=H-h-36:enable='gte(t,%.6f)*lt(t,%.6f)'[v%d]", j, j+2, start, end, j+1)
		}
		clipArgs = append(clipArgs, "-filter_complex", graph, "-map", fmt.Sprintf("[v%d]", len(captionFiles)), "-map", "[a]", "-t", strconv.Itoa(q.Duration), "-c:v", "libx264", "-preset", "fast", "-crf", "22", "-pix_fmt", "yuv420p", "-threads", "1", "-filter_complex_threads", "1", "-c:a", "aac", "-ar", "48000", clip)
		_, e = command(ctx, "", "ffmpeg", clipArgs...)
		if e != nil {
			return "", e
		}
		normalized = append(normalized, clip)
		// Phrase-level captions follow the measured narration envelope within each shot.
		text := []rune(strings.ReplaceAll(q.Narration, "\n", " "))
		parts := []string{}
		for len(text) > 0 {
			n := 18
			if len(text) < n {
				n = len(text)
			}
			parts = append(parts, string(text[:n]))
			text = text[n:]
		}
		span := math.Min(vd/rate, float64(q.Duration))
		for j, part := range parts {
			start := duration + float64(j)*span/float64(len(parts))
			end := duration + float64(j+1)*span/float64(len(parts))
			captions = append(captions, fmt.Sprintf("%d\n%s --> %s\n%s\n", num, srtTime(start), srtTime(end), part))
			num++
		}
		duration += float64(q.Duration)
		if i < len(b.Shots)-1 && q.Transition == "dissolve" {
			duration -= 0.3
		}
	}
	srt := filepath.Join(dir, "captions.srt")
	if e := atomicWrite(srt, []byte(strings.Join(captions, "\n"))); e != nil {
		return "", e
	}
	args := []string{"-v", "error", "-y"}
	for _, file := range normalized {
		args = append(args, "-i", file)
	}
	filters := []string{}
	for i := range normalized {
		filters = append(filters, fmt.Sprintf("[%d:v]settb=AVTB,setpts=PTS-STARTPTS[src%d]", i, i))
	}
	v, a := "src0", "0:a"
	elapsed := float64(b.Shots[0].Duration)
	for i := 1; i < len(normalized); i++ {
		nv, na := fmt.Sprintf("v%d", i), fmt.Sprintf("a%d", i)
		if b.Shots[i-1].Transition == "dissolve" {
			filters = append(filters, fmt.Sprintf("[%s][src%d]xfade=transition=fade:duration=0.3:offset=%.6f[%s];[%s][%d:a]acrossfade=d=0.3[%s]", v, i, elapsed-0.3, nv, a, i, na))
			elapsed -= 0.3
		} else {
			filters = append(filters, fmt.Sprintf("[%s][%s][src%d][%d:a]concat=n=2:v=1:a=1[%s][%s]", v, a, i, i, nv, na))
		}
		elapsed += float64(b.Shots[i].Duration)
		v = nv
		a = na
	}
	filters = append(filters, fmt.Sprintf("[%s]null[picture]", v))
	if music != "" {
		idx := len(normalized)
		args = append(args, "-stream_loop", "-1", "-i", s.abs(music))
		filters = append(filters, fmt.Sprintf("[%d:a]volume=0.10,atrim=duration=%.6f[music];[%s][music]amix=inputs=2:duration=first:normalize=0[mix]", idx, elapsed, a))
		a = "mix"
	}
	draft := filepath.Join(dir, "edited.mp4")
	args = append(args, "-filter_complex", strings.Join(filters, ";"), "-map", "[picture]", "-map", "["+a+"]", "-t", fmt.Sprintf("%.6f", elapsed), "-c:v", "libx264", "-preset", "fast", "-crf", "22", "-pix_fmt", "yuv420p", "-threads", "1", "-filter_complex_threads", "1", "-c:a", "aac", "-movflags", "+faststart", draft)
	if _, e := command(ctx, "", "ffmpeg", args...); e != nil {
		return "", e
	}
	output := filepath.Join(dir, "final.mp4")
	if e := s.hypitExport(ctx, pid, b, draft, output); e != nil {
		return "", e
	}
	probe, e := s.probe(ctx, output)
	if e != nil {
		return "", e
	}
	d, _ := strconv.ParseFloat(probe.Format.Duration, 64)
	hasV, hasA := false, false
	for _, st := range probe.Streams {
		if st.Type == "video" {
			hasV = st.Width == 486 && st.Height == 864
		}
		if st.Type == "audio" {
			hasA = true
		}
	}
	if !hasV || !hasA || d < 55 || d > 65 {
		return "", fmt.Errorf("成片校验失败：视频=%v，音轨=%v，时长=%.2f", hasV, hasA, d)
	}
	// Record black-frame evidence; warn instead of discarding deliberately dark story content.
	cmd := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-i", output, "-vf", "blackdetect=d=0.5:pix_th=0.05", "-an", "-f", "null", "-")
	raw, err := cmd.CombinedOutput()
	if err != nil {
		return "", errors.New("成片黑帧检查无法完成")
	}
	report := map[string]any{"duration": d, "width": 486, "height": 864, "audio": hasA, "black_frame_warning": strings.Contains(string(raw), "black_start:"), "caption_timing": "按每镜实测旁白时长分句；非逐词强制对齐", "hypit": "已执行 check / plan / build / get"}
	rb, _ := json.MarshalIndent(report, "", "  ")
	if e = atomicWrite(filepath.Join(dir, "validation.json"), rb); e != nil {
		return "", e
	}
	if report["black_frame_warning"] == true {
		return "", errors.New("检测到超过0.5秒的黑帧；成片与报告已保留，请检查素材后重新合成")
	}
	return finalRel, nil
}
func srtTime(v float64) string {
	n := int(math.Round(v * 1000))
	return fmt.Sprintf("%02d:%02d:%02d,%03d", n/3600000, n/60000%60, n/1000%60, n%1000)
}
func xmlText(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
func (s *Studio) writeProduction(pid string, b Board) error {
	dir := filepath.Join(s.data, "productions", pid, fmt.Sprintf("v%d", b.Version))
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	raw, _ := json.MarshalIndent(b, "", "  ")
	if e := atomicWrite(filepath.Join(dir, "storyboard.json"), raw); e != nil {
		return e
	}
	for name, text := range map[string]string{"Brief.md": b.Title + "\n\n中文竖屏旁白剧情短片，目标60秒。\n", "Treatment.md": b.Synopsis, "Script.md": func() string {
		var out strings.Builder
		for _, q := range b.Shots {
			fmt.Fprintf(&out, "## %s · %ds\n%s\n\n图：%s\n\n视频：%s\n\n", q.Title, q.Duration, q.Narration, q.ImagePrompt, q.VideoPrompt)
		}
		return out.String()
	}()} {
		if e := atomicWrite(filepath.Join(dir, name), []byte(text)); e != nil {
			return e
		}
	}
	return nil
}

// FFmpeg owns deterministic cuts, transitions and subtitles. Hypit owns the final
// local normalization/export Run and its durable production Result. No hosted generation.
func (s *Studio) hypitExport(ctx context.Context, pid string, b Board, draft, output string) (retErr error) {
	if e := s.writeProduction(pid, b); e != nil {
		return e
	}
	dir := filepath.Join(s.data, "productions", pid, fmt.Sprintf("v%d", b.Version))
	source := fmt.Sprintf(`<?svml using="@hypit/markup@1"?>
<svml>
<import as="media" from="@hypit/media@1"/>
<import as="time" from="@hypit/timeline-author@1"/>
<import as="op" from="@hypit/media-operations@1"/>
<time:Clock id="clock" frame-rate="24"/>
<media:Video id="edited" src="%s"/>
<op:Normalize id="normalized" source={edited} video="primary-moving" audio="default" span-authority="video" clock={clock}/>
<op:Transform id="final" source={normalized.media}><op:Retime rate="1"/></op:Transform>
</svml>`, xmlText("./edited.mp4"))
	draftBytes, err := os.ReadFile(draft)
	if err != nil {
		return err
	}
	if err = atomicWrite(filepath.Join(dir, "edited.mp4"), draftBytes); err != nil {
		return err
	}
	files := map[string]string{"package.json": `{"private":true,"hypit":{"project":true}}`, "main.svml": source, "main.svrun": `<?svml using="@hypit/markup/run@1"?><svrun version="1"><author source="./main.svml"/><target output="final.video"/></svrun>`, "hypit.runtime.json": `{"format":"hypit.runtime-local@1","dataRoot":".hypit/runtimes/local","endpoints":{"media.local":{"use":"@hypit/media-local","config":{"defaultConcurrency":1}}}}`}
	for name, text := range files {
		if e := atomicWrite(filepath.Join(dir, name), []byte(text)); e != nil {
			return e
		}
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, cleanupErr := command(cleanupCtx, dir, "hypit", "runtime", "down", "hypit.runtime.json")
		if cleanupErr != nil && retErr == nil {
			retErr = fmt.Errorf("Hypit Runtime停止失败：%w", cleanupErr)
		}
	}()
	for _, args := range [][]string{{"check", "main.svrun"}, {"plan", "main.svrun", "--runtime", "hypit.runtime.json", "--json"}} {
		raw, e := command(ctx, dir, "hypit", args...)
		if e != nil {
			return e
		}
		if e = atomicWrite(filepath.Join(dir, args[0]+".json"), raw); e != nil {
			return e
		}
	}
	raw, e := command(ctx, dir, "hypit", "build", "main.svrun", "--runtime", "hypit.runtime.json", "--follow", "--json")
	if e != nil {
		return e
	}
	if e = atomicWrite(filepath.Join(dir, "build.json"), raw); e != nil {
		return e
	}
	id := findBuildID(raw)
	if id == "" {
		return errors.New("Hypit未返回可识别Build ID，记录已保存")
	}
	_, e = command(ctx, dir, "hypit", "get", id, "--output", "final.video", "--to", output)
	return e
}
func findBuildID(raw []byte) string {
	var v any
	if json.Unmarshal(raw, &v) == nil {
		var walk func(any) string
		walk = func(x any) string {
			switch x := x.(type) {
			case string:
				if strings.HasPrefix(x, "bld_") {
					return x
				}
			case map[string]any:
				for _, k := range []string{"buildId", "build_id", "id"} {
					if val, ok := x[k].(string); ok && strings.HasPrefix(val, "bld_") {
						return val
					}
				}
				for _, v := range x {
					if found := walk(v); found != "" {
						return found
					}
				}
			case []any:
				for _, v := range x {
					if found := walk(v); found != "" {
						return found
					}
				}
			}
			return ""
		}
		return walk(v)
	}
	return ""
}

// Rasterize captions ourselves: works with FFmpeg builds without libass/drawtext.
// A user-supplied CJK font can override platform defaults without bundling fonts.
func captionImages(dir string, shot int, text string) ([]string, error) {
	candidates := []string{os.Getenv("LYPIT_FONT"), "/System/Library/Fonts/Hiragino Sans GB.ttc", "/System/Library/Fonts/STHeiti Medium.ttc", "/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc", "C:/Windows/Fonts/msyh.ttc"}
	var data []byte
	for _, path := range candidates {
		if path == "" {
			continue
		}
		b, e := os.ReadFile(path)
		if e == nil {
			data = b
			break
		}
	}
	if len(data) == 0 {
		return nil, errors.New("找不到中文字体，请用LYPIT_FONT指定TTF/OTF/TTC文件")
	}
	f, e := opentype.Parse(data)
	if e != nil {
		collection, err := opentype.ParseCollection(data)
		if err != nil {
			return nil, err
		}
		f, e = collection.Font(0)
		if e != nil {
			return nil, e
		}
	}
	face, e := opentype.NewFace(f, &opentype.FaceOptions{Size: 23, DPI: 72, Hinting: font.HintingFull})
	if e != nil {
		return nil, e
	}
	defer face.Close()
	chars := []rune(text)
	files := []string{}
	for len(chars) > 0 {
		n := 18
		if n > len(chars) {
			n = len(chars)
		}
		phrase := string(chars[:n])
		chars = chars[n:]
		im := image.NewRGBA(image.Rect(0, 0, 486, 66))
		width := font.MeasureString(face, phrase).Ceil()
		x := (486 - width) / 2
		draw.Draw(im, image.Rect(x-12, 5, x+width+12, 57), &image.Uniform{color.RGBA{9, 18, 13, 175}}, image.Point{}, draw.Src)
		d := font.Drawer{Dst: im, Src: image.NewUniform(color.RGBA{244, 247, 242, 255}), Face: face, Dot: fixed.P(x, 40)}
		d.DrawString(phrase)
		var out bytes.Buffer
		if e = png.Encode(&out, im); e != nil {
			return nil, e
		}
		file := filepath.Join(dir, fmt.Sprintf("caption-%02d-%02d.png", shot, len(files)))
		if e = atomicWrite(file, out.Bytes()); e != nil {
			return nil, e
		}
		files = append(files, file)
	}
	return files, nil
}
