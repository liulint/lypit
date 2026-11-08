package main

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type DramaCaption struct {
	Start   float64 `json:"start"`
	End     float64 `json:"end"`
	Text    string  `json:"text"`
	Overlay bool    `json:"overlay"`
}

var dramaRange = regexp.MustCompile(`^(-?[0-9]+(?:\.[0-9]+)?)(?:-([0-9]+(?:\.[0-9]+)?))?\s+(.+)$`)

func (s *Studio) parseDramaAudio(field, value string, all []DramaMedia, cutDuration float64) (DramaAudio, error) {
	var a DramaAudio
	a.Type = field
	idx := strings.Index(value, "（")
	options := ""
	if idx >= 0 {
		options = strings.TrimSuffix(value[idx+len("（"):], "）")
		value = strings.TrimSpace(value[:idx])
	}
	v := dramaRange.FindStringSubmatch(value)
	if len(v) != 4 {
		return a, errors.New("音轨格式：开始秒[-结束秒] 项目相对路径（起点：秒；增益：dB）")
	}
	a.Start, _ = strconv.ParseFloat(v[1], 64)
	if v[2] != "" {
		a.End, _ = strconv.ParseFloat(v[2], 64)
	}
	path := strings.TrimSpace(v[3])
	parts := strings.Fields(path)
	if len(parts) == 2 {
		path = parts[0]
		n, e := strconv.ParseFloat(parts[1], 64)
		if e != nil {
			return a, e
		}
		a.Gain = n
	}
	for _, option := range strings.Split(options, "；") {
		pair := strings.SplitN(option, "：", 2)
		if len(pair) != 2 {
			continue
		}
		n, e := strconv.ParseFloat(pair[1], 64)
		if e != nil {
			return a, errors.New("音轨参数须为数字")
		}
		switch pair[0] {
		case "起点":
			a.Offset = n
		case "增益":
			a.Gain = n
		default:
			return a, errors.New("未知音轨参数")
		}
	}
	a.Loop = strings.HasPrefix(field, "环境声") || strings.HasPrefix(field, "音乐")
	if a.Start >= cutDuration || a.Offset < 0 || a.Gain < -40 || a.Gain > 6 || a.End != 0 && a.End <= a.Start {
		return a, errors.New("音轨区间、起点或增益无效")
	}
	for _, m := range all {
		if m.Path == path && m.Approved && m.Current && (m.Kind == "voice" || m.Kind == "music" || m.Kind == "audio") {
			a.Media = m
			return a, nil
		}
	}
	return a, errors.New("音轨文件不存在、未确认或来源变化")
}
func dramaParseCaption(field, value, script string, duration float64) (DramaCaption, error) {
	var c DramaCaption
	v := dramaRange.FindStringSubmatch(value)
	if len(v) != 4 || v[2] == "" {
		return c, errors.New("字幕或文字须明确：开始秒-结束秒 正文")
	}
	c.Start, _ = strconv.ParseFloat(v[1], 64)
	c.End, _ = strconv.ParseFloat(v[2], 64)
	c.Text = v[3]
	c.Overlay = strings.HasPrefix(field, "画面文字")
	if c.Overlay {
		if !strings.HasPrefix(c.Text, "普通 ") {
			return c, errors.New("仅支持普通画面文字；动画面板尚未适配")
		}
		c.Text = strings.TrimPrefix(c.Text, "普通 ")
	}
	if c.Start < 0 || c.End <= c.Start || c.End > duration {
		return c, errors.New("字幕或文字时间越过本段")
	}
	if c.Overlay {
		found := false
		for _, line := range strings.Split(script, "\n") {
			if strings.HasPrefix(line, "[画面文字]") && strings.Contains(line, c.Text) {
				found = true
			}
		}
		if !found {
			return c, errors.New("画面文字须来自剧本的画面文字标签")
		}
	}
	if !strings.Contains(script, c.Text) {
		return c, errors.New("字幕或画面文字须逐字来自剧本")
	}
	return c, nil
}
func dramaASS(t float64) string {
	n := int(t*100 + 0.5)
	return fmt.Sprintf("%d:%02d:%02d.%02d", n/360000, n/6000%60, n/100%60, n%100)
}
func dramaASSText(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "{", "\\{")
	s = strings.ReplaceAll(s, "}", "\\}")
	return strings.ReplaceAll(s, "\n", "\\N")
}
func dramaCaptions(cuts []DramaCut, width, height int) string {
	lines := []string{fmt.Sprintf("[Script Info]\nScriptType: v4.00+\nPlayResX: %d\nPlayResY: %d\n\n[V4+ Styles]\nFormat: Name,Fontname,Fontsize,PrimaryColour,SecondaryColour,OutlineColour,BackColour,Bold,Italic,Underline,StrikeOut,ScaleX,ScaleY,Spacing,Angle,BorderStyle,Outline,Shadow,Alignment,MarginL,MarginR,MarginV,Encoding\nStyle: Dialogue,Arial,22,&H00FFFFFF,&H000000FF,&H00101010,&H80000000,0,0,0,0,100,100,0,0,1,1,0,2,20,20,24,1\nStyle: Overlay,Arial,22,&H00FFFFFF,&H000000FF,&H00101010,&H80000000,0,0,0,0,100,100,0,0,1,1,0,8,20,20,24,1\n\n[Events]\nFormat: Layer,Start,End,Style,Name,MarginL,MarginR,MarginV,Effect,Text", width, height)}
	at := 0.0
	for _, cut := range cuts {
		caps := append([]DramaCaption{}, cut.Captions...)
		if cut.Subtitle != "" {
			caps = append(caps, DramaCaption{End: cut.Duration, Text: cut.Subtitle})
		}
		for _, c := range caps {
			style := "Dialogue"
			if c.Overlay {
				style = "Overlay"
			}
			lines = append(lines, fmt.Sprintf("Dialogue: 0,%s,%s,%s,,0,0,0,,%s", dramaASS(at+c.Start), dramaASS(at+c.End), style, dramaASSText(c.Text)))
		}
		at += cut.Duration
	}
	return strings.Join(lines, "\n")
}

// Timeline tracks are mixed after concatenation so voice, ambience and music can
// cross cut boundaries without restarting or clipping a word at every shot.
func (s *Studio) dramaTimelineAudio(ctx context.Context, cuts []DramaCut, total float64) ([]string, string, error) {
	type track struct {
		a          DramaAudio
		start, end float64
	}
	tracks := []track{}
	at := 0.0
	for _, cut := range cuts {
		for _, a := range cut.Audio {
			tracks = append(tracks, track{a: a, start: at + a.Start, end: func() float64 {
				if a.End == 0 {
					return 0
				}
				return at + a.End
			}()})
		}
		at += cut.Duration
	}
	args := []string{}
	filters := []string{"[0:a]asetpts=PTS-STARTPTS[base]"}
	labels := "[base]"
	for i, t := range tracks {
		if e := s.verifyDramaMedia(t.a.Media); e != nil {
			return nil, "", e
		}
		if t.start < 0 {
			return nil, "", errors.New("跨切点音轨超出成片开始")
		}
		p, e := s.probeDrama(ctx, t.a.Media.File)
		if e != nil {
			return nil, "", e
		}
		duration := dramaSeconds(p.Format.Duration)
		end := t.end
		if end == 0 {
			if t.a.Loop {
				end = total
				for _, next := range tracks {
					same := (strings.HasPrefix(t.a.Type, "环境声") && strings.HasPrefix(next.a.Type, "环境声")) || (strings.HasPrefix(t.a.Type, "音乐") && strings.HasPrefix(next.a.Type, "音乐"))
					if same && next.start > t.start && next.start < end {
						end = next.start
					}
				}
			} else {
				end = t.start + duration - t.a.Offset
			}
		}
		if end > total+0.02 || end <= t.start || !t.a.Loop && end-t.start > duration-t.a.Offset+0.02 {
			return nil, "", errors.New("音轨越过成片或来源时长，请修改剪辑单；不会截断补齐")
		}
		if t.a.Loop {
			args = append(args, "-stream_loop", "-1")
		}
		args = append(args, "-i", s.media(t.a.Media.File))
		label := fmt.Sprintf("track%d", i)
		filters = append(filters, fmt.Sprintf("[%d:a]atrim=start=%g:duration=%g,asetpts=PTS-STARTPTS,volume=%gdB,adelay=%d:all=1[%s]", i+1, t.a.Offset, end-t.start, t.a.Gain, int(t.start*1000), label))
		labels += "[" + label + "]"
	}
	filters = append(filters, fmt.Sprintf("%samix=inputs=%d:duration=first:normalize=0,loudnorm=I=-16:TP=-1.5:LRA=11[mix]", labels, len(tracks)+1))
	return args, strings.Join(filters, ";"), nil
}
