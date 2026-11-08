package main

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
)

var errDramaImageAspect = errors.New("分镜图片画幅验收失败")

// New quotes bind this requirement; older confirmed jobs retain their inputs.
func (s *Studio) validateDramaImageAspect(im Image, aspect string) error {
	b, err := os.ReadFile(s.media(im.File))
	if err != nil {
		return err
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil || cfg.Height == 0 {
		return errors.New("分镜图片无法读取尺寸")
	}
	want := 9.0 / 16
	if aspect == "16:9" {
		want = 16.0 / 9
	} else if aspect != "9:16" {
		return errors.New("分镜图片目标画幅无效")
	}
	if absFloat(float64(cfg.Width)/float64(cfg.Height)-want) > 0.02 {
		return fmt.Errorf("%w：实际%d×%d，要求%s；原产物与回执已保留，未替换原图。请重新报价生成，或明确选择本地不裁切留边；恢复仅复核原回执，不会重新调用模型", errDramaImageAspect, cfg.Width, cfg.Height, aspect)
	}
	return nil
}

func (s *Studio) prepareDramaProductionImageRefs(t *Task, p *DramaProduction) ([]Image, error) {
	refs := []Image{}
	for _, b := range p.Bindings {
		if err := s.verifyDramaMedia(b.Media); err != nil {
			return nil, err
		}
		refs = append(refs, Image{File: b.Media.File, Hash: b.Media.Hash, Approved: true})
	}
	if p.Source != nil {
		if err := s.verifyDramaMedia(*p.Source); err != nil {
			return nil, err
		}
	}
	if p.ReferencePlan == nil {
		return refs, nil
	}
	limit := 5
	if p.Operation == "edit" {
		limit = 4
	}
	plan, packed, err := s.prepareReferenceSources(p.ReferencePlan.Sources, limit)
	if err != nil {
		return nil, err
	}
	if string(jsonBytes(plan)) != string(jsonBytes(p.ReferencePlan)) {
		return nil, errors.New("参考板与已确认报价不一致")
	}
	refs = []Image{}
	for i, b := range packed {
		// Different batch items may have different boards, even within one task.
		file, err := s.writeMedia(t.ProjectID, fmt.Sprintf("%s-%s-reference-%d.png", t.ID, digest([]byte(p.Entry))[:16], i), b)
		if err != nil {
			return nil, err
		}
		refs = append(refs, Image{File: file, Hash: digest(b), Approved: true})
	}
	return refs, nil
}

// A local, explicit derivative. Preserve every source pixel and keep the source
// file/version; never charge a second image call just to fix the canvas ratio.
func (s *Studio) padDramaFrame(w DramaWork, source DramaMedia) (DramaMedia, error) {
	if source.Kind != "image" || !source.Current {
		return DramaMedia{}, errors.New("请选择来源有效的图片")
	}
	if e := s.verifyDramaMedia(source); e != nil {
		return DramaMedia{}, e
	}
	b, e := os.ReadFile(s.media(source.File))
	if e != nil {
		return DramaMedia{}, e
	}
	im, _, e := image.Decode(bytes.NewReader(b))
	if e != nil {
		return DramaMedia{}, errors.New("图片无法解码")
	}
	rw, rh := 9, 16
	if w.Aspect == "16:9" {
		rw, rh = 16, 9
	}
	bounds := im.Bounds()
	unit := (bounds.Dx() + rw - 1) / rw
	if n := (bounds.Dy() + rh - 1) / rh; n > unit {
		unit = n
	}
	width, height := rw*unit, rh*unit
	if width > 4096 || height > 4096 {
		return DramaMedia{}, errors.New("不缩放留边后的画布超过4096，请先主动处理图片尺寸")
	}
	out := image.NewNRGBA64(image.Rect(0, 0, width, height))
	draw.Draw(out, out.Bounds(), image.NewUniform(color.NRGBA64{R: 65535, G: 65535, B: 65535, A: 65535}), image.Point{}, draw.Src)
	dest := image.Rect((width-bounds.Dx())/2, (height-bounds.Dy())/2, (width+bounds.Dx())/2, (height+bounds.Dy())/2)
	draw.Draw(out, dest, im, bounds.Min, draw.Src)
	var buf bytes.Buffer
	if e = png.Encode(&buf, out); e != nil {
		return DramaMedia{}, e
	}
	m := source
	m.ID = uid()
	m.Approved = false
	m.Parent = source.ID
	m.Hash = digest(buf.Bytes())
	m.File, e = s.writeMedia(w.ID, m.ID+"-frame.png", buf.Bytes())
	if e != nil {
		return m, e
	}
	m.Path = s.dramaMediaPath(m.WorkID, filepath.Base(m.File))
	e = s.saveDramaMedia(m)
	return m, e
}
