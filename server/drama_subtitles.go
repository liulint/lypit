package main

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
	_ "golang.org/x/image/webp"
)

func dramaFont() (*opentype.Font, error) {
	for _, path := range dramaFontPaths() {
		info, e := os.Stat(path)
		if e != nil || !info.Mode().IsRegular() || info.Size() > 100<<20 {
			continue
		}
		b, e := os.ReadFile(path)
		if e != nil {
			continue
		}
		if collection, e := opentype.ParseCollection(b); e == nil {
			if f, e := collection.Font(0); e == nil {
				return f, nil
			}
		}
		if f, e := opentype.Parse(b); e == nil {
			return f, nil
		}
	}
	return nil, errors.New("系统中文字体不可读取，请恢复操作系统的标准中文字体后再合成字幕")
}
func drawDramaCaption(f *opentype.Font, c DramaCaption, width, height int) (image.Image, error) {
	face, e := opentype.NewFace(f, &opentype.FaceOptions{Size: float64(width) * 22 / 360, DPI: 72, Hinting: font.HintingFull})
	if e != nil {
		return nil, e
	}
	defer face.Close()
	var buffer sfnt.Buffer
	for _, r := range c.Text {
		if unicode.IsSpace(r) {
			continue
		}
		index, e := f.GlyphIndex(&buffer, r)
		if e != nil || index == 0 {
			return nil, errors.New("系统字体缺少字幕字符，未以方框替代")
		}
	}
	lines := []string{}
	line := ""
	for _, r := range c.Text {
		if r == '\n' {
			lines = append(lines, line)
			line = ""
			continue
		}
		next := line + string(r)
		if line != "" && font.MeasureString(face, next).Ceil() > width-40 {
			lines = append(lines, line)
			line = string(r)
		} else {
			line = next
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	step := face.Metrics().Height.Ceil() + 4
	if step*len(lines) > height-48 {
		return nil, errors.New("字幕超过画面可读空间，请拆分字幕时段")
	}
	out := image.NewNRGBA(image.Rect(0, 0, width, height))
	y := height - 24 - step*(len(lines)-1)
	if c.Overlay {
		y = 24 + face.Metrics().Ascent.Ceil()
	}
	for _, text := range lines {
		x := (width - font.MeasureString(face, text).Ceil()) / 2
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				drawer := font.Drawer{Dst: out, Src: image.NewUniform(color.Black), Face: face, Dot: fixed.P(x+dx, y+dy)}
				drawer.DrawString(text)
			}
		}
		drawer := font.Drawer{Dst: out, Src: image.NewUniform(color.White), Face: face, Dot: fixed.P(x, y)}
		drawer.DrawString(text)
		y += step
	}
	return out, nil
}
func dramaCaptionLayers(cuts []DramaCut, dir string, width, height, inputStart int) ([]string, string, string, error) {
	type caption struct {
		value      DramaCaption
		start, end float64
	}
	all := []caption{}
	at := 0.0
	for _, cut := range cuts {
		for _, c := range cut.Captions {
			all = append(all, caption{c, at + c.Start, at + c.End})
		}
		if cut.Subtitle != "" {
			all = append(all, caption{DramaCaption{Text: cut.Subtitle}, at, at + cut.Duration})
		}
		at += cut.Duration
	}
	if len(all) == 0 {
		return nil, "", "0:v", nil
	}
	f, e := dramaFont()
	if e != nil {
		return nil, "", "", e
	}
	args := []string{}
	filters := []string{}
	source := "0:v"
	for i, c := range all {
		im, e := drawDramaCaption(f, c.value, width, height)
		if e != nil {
			return nil, "", "", e
		}
		name := fmt.Sprintf("caption-%04d.png", i)
		out, e := os.OpenFile(filepath.Join(dir, name), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
		if e != nil {
			return nil, "", "", e
		}
		e = png.Encode(out, im)
		closeErr := out.Close()
		if e != nil {
			return nil, "", "", e
		}
		if closeErr != nil {
			return nil, "", "", closeErr
		}
		args = append(args, "-loop", "1", "-framerate", "24", "-i", name)
		dest := fmt.Sprintf("caption%d", i)
		filters = append(filters, fmt.Sprintf("[%s][%d:v]overlay=0:0:shortest=1:enable='gte(t,%g)*lt(t,%g)'[%s]", source, inputStart+i, c.start, c.end, dest))
		source = dest
	}
	return args, strings.Join(filters, ";"), "[" + source + "]", nil
}
