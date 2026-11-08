package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"image"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type DramaMedia struct {
	Selected      bool               `json:"selected"`
	VideoInfo     *DramaVideoInfo    `json:"video_info,omitempty"`
	ReferenceOnly bool               `json:"reference_only,omitempty"`
	SharedOrigin  *DramaSharedOrigin `json:"shared_origin,omitempty"`
	Current       bool               `json:"current"`
	Verification  *DramaVerification `json:"verification,omitempty"`
	Category      string             `json:"category,omitempty"`
	ID            string             `json:"id"`
	WorkID        string             `json:"work_id"`
	Entry         string             `json:"entry"`
	Kind          string             `json:"kind"`
	File          string             `json:"file"`
	Path          string             `json:"path"`
	Hash          string             `json:"hash"`
	Approved      bool               `json:"approved"`
	TaskID        string             `json:"task_id"`
	SourceHash    string             `json:"source_hash"`
	Duration      float64            `json:"duration"`
	Mock          bool               `json:"mock"`
	Parent        string             `json:"parent,omitempty"`
}
type DramaBinding struct {
	Slot    string     `json:"slot"`
	Order   int        `json:"order"`
	Use     string     `json:"use"`
	Control string     `json:"control"`
	Exclude string     `json:"exclude"`
	Media   DramaMedia `json:"media"`
}
type DramaFrameSelection struct {
	TailVideoID   string `json:"tail_video_id,omitempty"`
	TailVideoHash string `json:"tail_video_hash,omitempty"`
	Entry         string `json:"entry"`
	MediaID       string `json:"media_id"`
	Hash          string `json:"hash"`
}

type DramaProduction struct {
	StartPlanVersion int                   `json:"start_plan_version,omitempty"`
	TailVideoID      string                `json:"tail_video_id,omitempty"`
	TailVideoHash    string                `json:"tail_video_hash,omitempty"`
	TailSource       *DramaMedia           `json:"tail_source,omitempty"`
	VideoOptions     *DramaVideoOptions    `json:"video_options,omitempty"`
	Entries          []string              `json:"entries,omitempty"`
	CompareSource    *DramaMedia           `json:"compare_source,omitempty"`
	PromptOverride   string                `json:"prompt_override,omitempty"`
	VideoEditInput   *VideoInput           `json:"video_edit_input,omitempty"`
	ImageAspect      string                `json:"image_aspect,omitempty"`
	ImageMaxCalls    int                   `json:"image_max_calls,omitempty"`
	ImageRetryPrompt string                `json:"image_retry_prompt,omitempty"`
	FrameSelections  []DramaFrameSelection `json:"frame_selections,omitempty"`
	FrameMediaID     string                `json:"frame_media_id,omitempty"`
	FrameMediaHash   string                `json:"frame_media_hash,omitempty"`
	Items            []DramaProduction     `json:"items,omitempty"`
	ReferencePlan    *ReferencePlan        `json:"reference_plan,omitempty"`
	Kind             string                `json:"kind"`
	Document         string                `json:"document"`
	Entry            string                `json:"entry"`
	Prompt           string                `json:"prompt"`
	Duration         float64               `json:"duration"`
	Mode             string                `json:"mode"`
	Bindings         []DramaBinding        `json:"bindings"`
	Source           *DramaMedia           `json:"source,omitempty"`
	Sheet            bool                  `json:"sheet"`
	Operation        string                `json:"operation"`
	Changes          string                `json:"changes,omitempty"`
	Preserve         string                `json:"preserve,omitempty"`
	Cuts             []DramaCut            `json:"cuts,omitempty"`
}
type DramaItemEstimate struct {
	Entry    string         `json:"entry"`
	Estimate *QuoteEstimate `json:"estimate,omitempty"`
	Reason   string         `json:"reason,omitempty"`
}
type DramaQuote struct {
	ItemEstimates   []DramaItemEstimate `json:"item_estimates,omitempty"`
	Estimate        *QuoteEstimate      `json:"estimate,omitempty"`
	EstimateReason  string              `json:"estimate_reason,omitempty"`
	Model           string              `json:"model"`
	Vendor          string              `json:"vendor"`
	ID              string              `json:"id"`
	WorkID          string              `json:"work_id"`
	Inputs          map[string]string   `json:"inputs"`
	SettingsVersion int                 `json:"settings_version"`
	Spec            DramaProduction     `json:"spec"`
	Unknown         bool                `json:"unknown"`
	Reason          string              `json:"reason"`
	Expires         string              `json:"expires"`
	TaskID          string              `json:"task_id,omitempty"`
	Config          Settings            `json:"-"`
}

func (s *Studio) dramaMedias(id string) ([]DramaMedia, error) {
	out := []DramaMedia{}
	rows, e := s.store.db.Query("SELECT body FROM drama_media WHERE work_id=? ORDER BY rowid DESC", id)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var b []byte
		var m DramaMedia
		if e = rows.Scan(&b); e != nil {
			return out, e
		}
		if e = json.Unmarshal(b, &m); e != nil {
			return out, e
		}
		out = append(out, m)
	}
	err := rows.Err()
	rows.Close()
	for i := range out {
		out[i].Current = s.dramaMediaCurrent(out[i])
	}
	if err == nil {
		err = s.decorateDramaVideos(out)
	}
	return out, err
}
func (s *Studio) dramaMediaCurrent(m DramaMedia) bool {
	if m.TaskID == "" {
		return true
	}
	j, e := s.dramaJob(m.TaskID)
	if e != nil {
		return false
	}
	if m.Kind == "film" && j.Production != nil && j.Production.Kind == "render" {
		for _, cut := range j.Production.Cuts {
			if cut.Media.Kind != "video" {
				continue
			}
			var selected string
			err := s.store.db.QueryRow("SELECT media_id FROM drama_video_choices WHERE work_id=? AND entry=?", m.WorkID, cut.Media.Entry).Scan(&selected)
			if err != nil && !errors.Is(err, sql.ErrNoRows) || err == nil && selected != cut.Media.ID {
				return false
			}
		}
	}
	if m.Kind == "video" && j.Production != nil {
		spec := j.Production
		if spec.Kind == "batch" {
			for i := range spec.Items {
				if spec.Items[i].Entry == m.Entry {
					spec = &spec.Items[i]
					break
				}
			}
		}
		if spec.Mode == "continue" && spec.Source != nil {
			var selected string
			err := s.store.db.QueryRow("SELECT media_id FROM drama_video_choices WHERE work_id=? AND entry=?", m.WorkID, spec.Source.Entry).Scan(&selected)
			if err != nil && !errors.Is(err, sql.ErrNoRows) || err == nil && selected != spec.Source.ID {
				return false
			}
		}
	}
	for kind, hash := range j.Inputs {
		if !s.dramaDependencyCurrent(m.WorkID, kind, hash, true) {
			return false
		}
	}
	return true
}
func (s *Studio) saveDramaMedia(m DramaMedia) error {
	return s.persistDramaMedia(m)
}
func (s *Studio) verifyDramaMedia(m DramaMedia) error {
	if !safeMedia(m.File) || !strings.HasPrefix(m.File, m.WorkID+"/") {
		return errors.New("媒体路径无效")
	}
	path := s.media(m.File)
	info, e := os.Lstat(path)
	if e != nil || !info.Mode().IsRegular() || info.Size() > 100<<20 {
		return errors.New("媒体缺失、类型无效或过大")
	}
	b, e := os.ReadFile(path)
	if e != nil || digest(b) != m.Hash {
		return errors.New("媒体内容已变化")
	}
	return nil
}
func copyable(body, heading string) string {
	on := false
	out := []string{}
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "### ") {
			if on {
				break
			}
			on = strings.TrimSpace(strings.TrimPrefix(line, "### ")) == heading
			continue
		}
		if on && strings.HasPrefix(line, ">") {
			out = append(out, strings.TrimPrefix(strings.TrimPrefix(line, ">"), " "))
		}
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

var dramaRefRE = regexp.MustCompile(`(REF-[A-Za-z0-9_-]+)（顺序：([0-9]+)）·\s*([^《\n]+)《[^》]+》（用途：([^；）]+)；控制：([^；）]+)；不得控制：([^）]+)）`)

func (s *Studio) dramaBindings(id, declaration string) ([]DramaBinding, error) {
	out := []DramaBinding{}
	if strings.Contains(declaration, "PLAN-") || strings.Contains(declaration, "待补") {
		return nil, errors.New("尚有项目外或待补参考图，请导入并确认真实图片后绑定REF")
	}
	all, e := s.dramaMedias(id)
	if e != nil {
		return nil, e
	}
	matches := dramaRefRE.FindAllStringSubmatch(declaration, -1)
	if strings.Count(declaration, "REF-") != len(matches) {
		return nil, errors.New("存在无法解析的REF声明")
	}
	if strings.Contains(declaration, "REF-") && len(matches) == 0 {
		return nil, errors.New("REF格式无效")
	}
	seen := map[string]bool{}
	for index, v := range matches {
		if !contains([]string{"身份", "造型状态", "地理", "构图", "尺度", "效果", "起始帧", "结束帧", "风格"}, v[4]) {
			return nil, errors.New("参考用途无效")
		}
		if v[2] != fmt.Sprint(index+1) {
			return nil, errors.New("参考图顺序必须从1连续编号")
		}
		var found *DramaMedia
		for _, m := range all {
			if m.Path == strings.TrimSpace(v[3]) {
				mm := m
				found = &mm
				break
			}
		}
		if found == nil || !found.Approved || !found.Current || found.Kind != "image" {
			return nil, errors.New("参考图不存在或未确认：" + v[3])
		}
		if seen[found.File] {
			return nil, errors.New("同一图片不得重复绑定")
		}
		seen[found.File] = true
		if e = s.verifyDramaMedia(*found); e != nil {
			return nil, e
		}
		out = append(out, DramaBinding{Slot: v[1], Order: index + 1, Use: v[4], Control: v[5], Exclude: v[6], Media: *found})
	}
	return out, nil
}
func (s *Studio) buildDramaProduction(w DramaWork, p DramaProduction, cfg Settings) (DramaProduction, map[string]string, error) {
	if p.Operation == "compare" && (p.Kind != "video" || p.VideoOptions == nil) {
		return p, nil, errors.New("试版须明确视频模型和清晰度")
	}
	if p.Kind == "video" && p.Operation == "compare" && p.Entry != "*" && p.CompareSource != nil {
		return s.buildDramaVideoComparison(w, p, cfg)
	}
	if len(p.Entries) > 200 || p.CompareSource != nil && p.Entry == "*" {
		return p, nil, errors.New("试版镜头最多200项，来源版本须逐镜绑定")
	}
	if len(p.Entries) > 0 && (p.Kind != "video" || p.Entry != "*" || p.Operation != "compare" && p.StartPlanVersion != 1) {
		return p, nil, errors.New("镜头清单仅用于批量视频试版或已就绪输入计划")
	}
	requested := map[string]bool{}
	for _, id := range p.Entries {
		if requested[id] {
			return p, nil, errors.New("镜头清单重复")
		}
		requested[id] = true
	}
	if p.Operation == "compare" && p.Entry == "*" && len(requested) == 0 {
		return p, nil, errors.New("请选择试版镜头")
	}
	if p.StartPlanVersion != 0 && (p.StartPlanVersion != 1 || p.Kind != "video" || p.Document != "motion" || p.Mode == "continue" || p.Mode == "edit_video") {
		return p, nil, errors.New("视频输入计划版本或模式无效")
	}
	if p.StartPlanVersion == 1 && p.Entry == "*" && (len(p.Entries) == 0 || len(p.FrameSelections) != len(p.Entries)) {
		return p, nil, errors.New("请明确本轮已就绪镜头与首帧清单")
	}
	frames := map[string]DramaFrameSelection{}
	if p.FrameSelections != nil {
		if p.Kind != "video" || p.Document != "motion" || p.Entry != "*" || len(p.FrameSelections) == 0 || len(p.FrameSelections) > 200 {
			return p, nil, errors.New("逐镜首帧清单仅支持批量视频，请重新查看报价")
		}
		for _, f := range p.FrameSelections {
			if _, exists := frames[f.Entry]; exists || f.MediaID == "" || f.Hash == "" {
				return p, nil, errors.New("首帧清单重复或不完整")
			}
			frames[f.Entry] = f
		}
	}
	if (p.FrameMediaID != "" || p.FrameMediaHash != "") && (p.Kind != "video" || p.Entry == "*" || p.FrameMediaID == "" || p.FrameMediaHash == "") {
		return p, nil, errors.New("首帧选择仅支持当前一镜视频")
	}
	if p.Operation == "regenerate" && (p.Entry == "*" || (p.Kind != "image" && p.Kind != "video") || strings.TrimSpace(p.Changes) == "" || len(p.Changes) > 10000) {
		return p, nil, errors.New("重新生成仅支持当前一张图或一镜视频，请填写修改建议（最多10000字节）")
	}
	p.PromptOverride = strings.TrimSpace(p.PromptOverride)
	if p.PromptOverride != "" && (p.Kind != "video" || p.Operation != "regenerate" || p.Mode == "edit_video" || len(p.PromptOverride) > 30000) {
		return p, nil, errors.New("本镜生成说明仅用于当前一镜重新生成，最多30000字节，不与原视频编辑混用")
	}

	if p.Entry == "*" && (p.Kind == "image" || p.Kind == "video") {
		d, err := s.dramaDoc(w.ID, p.Document)
		if err != nil {
			return p, nil, err
		}
		all, err := s.dramaMedias(w.ID)
		if err != nil {
			return p, nil, err
		}
		items := []DramaProduction{}
		inputs := map[string]string{}
		for _, entry := range d.Entries {
			if p.Kind == "image" && !strings.HasPrefix(entry.ID, "SHOT-") && !strings.HasPrefix(entry.ID, "IMG-") {
				continue
			}
			if p.Kind == "video" && !strings.HasPrefix(entry.ID, "MOTION-") {
				continue
			}
			if len(p.Entries) > 0 && !requested[entry.ID] {
				continue
			}
			delete(requested, entry.ID)
			exists := false
			for _, m := range all {
				if m.Entry == entry.ID && m.Kind == p.Kind && m.Current {
					exists = true
					break
				}
			}
			if exists && p.Operation != "compare" {
				continue
			}
			spec := p
			spec.Entry = entry.ID
			spec.Items = nil
			spec.Entries = nil
			if p.Operation == "compare" {
				for _, m := range all {
					if m.Entry == entry.ID && m.Kind == "video" && m.Selected {
						mm := m
						spec.CompareSource = &mm
						break
					}
				}
			}
			spec.FrameSelections = nil
			if p.FrameSelections != nil && spec.CompareSource == nil {
				f, ok := frames[entry.ID]
				if !ok {
					return p, nil, fmt.Errorf("%s 缺少首帧选择，请重新检查清单", entry.ID)
				}
				spec.FrameMediaID, spec.FrameMediaHash = f.MediaID, f.Hash
				spec.TailVideoID, spec.TailVideoHash = f.TailVideoID, f.TailVideoHash
				delete(frames, entry.ID)
			}
			spec, dep, err := s.buildDramaProduction(w, spec, cfg)
			if err != nil {
				return p, nil, fmt.Errorf("%s：%w", entry.ID, err)
			}
			items = append(items, spec)
			for k, h := range dep {
				inputs[k] = h
			}
		}
		if len(frames) > 0 || len(requested) > 0 {
			return p, nil, errors.New("待生成视频清单已变化或包含无效镜头，请重新查看报价")
		}
		if len(items) == 0 {
			return p, nil, errors.New("没有需要补齐的媒体")
		}
		return DramaProduction{VideoOptions: p.VideoOptions, Operation: p.Operation, Kind: "batch", Document: p.Document, Entry: "*", Items: items, Prompt: fmt.Sprintf("补齐%d个缺少的媒体条目；逐项使用以下准确输入，不重做已有结果。", len(items))}, inputs, nil
	}

	if p.Kind != "image" && p.Kind != "video" && p.Kind != "voice" && p.Kind != "music" {
		return p, nil, errors.New("未知媒体能力")
	}
	d, e := s.dramaDoc(w.ID, p.Document)
	if e != nil {
		return p, nil, e
	}
	if d.State != "accepted" {
		return p, nil, errors.New("请先采用当前提示词来源文档")
	}
	inputs := map[string]string{p.Document: d.Hash}
	for k, v := range d.Inputs {
		inputs[k] = v
	}
	var entry *DramaEntry
	for _, v := range d.Entries {
		if v.ID == p.Entry {
			vv := v
			entry = &vv
			break
		}
	}
	if entry == nil {
		return p, nil, errors.New("来源条目不存在")
	}
	p.Prompt = ""
	p.Cuts = nil
	p.ReferencePlan = nil
	p.Bindings = nil
	p.ImageAspect = ""
	p.ImageMaxCalls = 0
	p.ImageRetryPrompt = ""
	p.VideoEditInput = nil
	p.TailSource = nil
	if (p.TailVideoID != "" || p.TailVideoHash != "") && (p.StartPlanVersion != 1 || p.Kind != "video" || p.TailVideoID == "" || p.TailVideoHash == "" || p.FrameMediaID == "" || p.Mode != "" && p.Mode != "first_frame") {
		return p, nil, errors.New("尾帧输入须绑定上一镜视频与真实尾帧，不与视频续接或编辑混用")
	}
	switch p.Kind {
	case "image":
		if p.Document != "images" && p.Document != "storyboard" {
			return p, nil, errors.New("图片只能来自IMG或SHOT条目")
		}
		heading := "可复制提示词"
		decl := entry.Fields["参考"]
		if p.Document == "storyboard" {
			heading = "冻结关键帧提示词"
			decl = entry.Fields["输入参考图"]
			if strings.Contains(decl, "待补") || strings.Contains(decl, "PLAN-") {
				decl = ""
			}
		}
		p.Prompt = copyable(entry.Body, heading)
		p.Bindings, e = s.dramaBindings(w.ID, decl)
		p.Sheet = strings.Contains(entry.Fields["用途"], "四视图") || strings.Contains(entry.Fields["用途"], "2×2")
		if p.Document == "storyboard" && !p.Sheet {
			p.ImageAspect = w.Aspect
			p.ImageMaxCalls = 2
			p.ImageRetryPrompt = fmt.Sprintf("上一次输出未通过画幅验收。本次为已确认范围内唯一一次重生：必须输出%s的实际像素比例，保留同一镜头的完整内容，不裁切主体，不再次重试。", w.Aspect)
			p.Prompt += fmt.Sprintf("\n交付画幅必须为%s；不得以2:3或3:2替代。完整保留构图，不裁切主体。生成后将校验实际像素比例，不符则不发布为可用分镜。", w.Aspect)
		}
		if p.Sheet {
			master := false
			for _, b := range p.Bindings {
				if b.Use == "身份" && b.Media.Category == "character" {
					master = true
				}
			}
			if !master {
				return p, nil, errors.New("四视图须绑定用途为身份、分类为角色的已确认主图")
			}
			p.Prompt += "\n执行格式：只输出一张正方形2×2设定图，四格为同一角色的正面、背面、左侧、右侧，完整入画，不加文字。"
		}
		if p.Sheet && len(p.Bindings) == 0 {
			return p, nil, errors.New("四视图需要已确认角色主图参考")
		}
	case "video":
		for _, group := range d.Entries {
			if strings.HasPrefix(group.ID, "GROUP-") && strings.Contains(group.Fields["成员"], p.Entry) {
				return p, nil, errors.New("此镜头已纳入多镜容器，当前适配器只支持单镜；请明确解除组包后重新报价，不会拆成多次付费调用")
			}
		}
		if !strings.HasPrefix(p.Entry, "MOTION-") {
			return p, nil, errors.New("视频必须来自MOTION条目")
		}
		if p.Document != "motion" {
			return p, nil, errors.New("视频必须来自MOTION条目")
		}
		p.Prompt = copyable(entry.Body, "可复制提示词")
		p.Duration = dramaSeconds(entry.Fields["时长"])
		if p.Duration <= 0 || p.Duration != float64(int(p.Duration)) {
			return p, nil, errors.New("当前视频适配要求整数秒镜长")
		}
		if p.Mode == "edit_video" {
			if p.Operation != "regenerate" || strings.TrimSpace(p.Changes) == "" {
				return p, nil, errors.New("基于当前视频修改须填写本镜修改建议")
			}
			v, source, err := s.dramaVideoEditInput(w, p, cfg)
			if err != nil {
				return p, nil, err
			}
			p.Source, p.VideoEditInput = &source, &v
			inputs["@video-edit:"+source.ID] = dramaEditSourceHash(source)
			p.Prompt = "编辑视频1：仅按修改建议调整当前镜头，保留未要求改变的角色、画面、声音、台词和镜头时长。输出修改后的完整本镜视频，不延长、不续写、不拼接原片。修改建议：" + p.Changes
			break
		}
		if p.TailVideoID != "" {
			if err := dramaSingleFirstFramePlan(*entry, p.Mode); err != nil {
				return p, nil, err
			}
			var source DramaMedia
			p.Bindings, source, e = s.selectedDramaTailFrame(w, p)
			if e == nil {
				p.TailSource = &source
				inputs["@tail-video:"+source.ID] = source.Hash
				inputs["@media:"+p.FrameMediaID] = p.FrameMediaHash
			}
		} else if p.FrameMediaID != "" {
			p.Bindings, e = s.selectedDramaVideoFrame(w.ID, p, *entry)
			if e == nil {
				inputs["@media:"+p.FrameMediaID] = p.Bindings[0].Media.Hash
			}
		} else {
			p.Bindings, e = s.dramaBindings(w.ID, entry.Fields["输入参考图"])
		}
		if e != nil {
			return p, nil, e
		}
		first, last, multi := 0, 0, 0
		for _, b := range p.Bindings {
			switch b.Use {
			case "起始帧":
				first++
			case "结束帧":
				last++
			default:
				multi++
			}
		}
		if first > 1 || last > 1 || last > 0 && first == 0 || multi > 0 && (first+last) > 0 {
			return p, nil, errors.New("严格首尾帧和多参考不可混传，请修订参考用途")
		}
		for _, binding := range p.Bindings {
			if binding.Use == "起始帧" || binding.Use == "结束帧" {
				b, err := os.ReadFile(s.media(binding.Media.File))
				if err != nil {
					return p, nil, err
				}
				imageConfig, _, err := image.DecodeConfig(bytes.NewReader(b))
				width, height := frameSize(Project{AspectRatio: w.Aspect})
				if err != nil || absFloat(float64(imageConfig.Width)/float64(imageConfig.Height)-float64(width)/float64(height)) > 0.02 {
					return p, nil, errors.New("严格首尾帧画幅与作品不一致，请先修订或生成正确画幅")
				}
			}
		}

		if p.Mode != "continue" {
			p.Source = nil
			if first > 0 {
				p.Mode = "first_frame"
				if last > 0 {
					p.Mode = "first_last"
				}
			} else if multi > 0 {
				p.Mode = "reference"
			} else {
				if !strings.Contains(entry.Fields["输入参考图"], "明确选择文生视频") {
					return p, nil, errors.New("无参考图时须明确选择文生视频")
				}
				p.Mode = "text"
			}
		} else {
			if p.Source == nil || p.Source.Kind != "video" || !p.Source.Approved {
				return p, nil, errors.New("续接需要选择已确认上一镜视频")
			}
			previous := ""
			for _, item := range d.Entries {
				if item.ID == p.Entry {
					break
				}
				if strings.HasPrefix(item.ID, "MOTION-") {
					previous = item.ID
				}
			}
			if previous == "" || p.Source.Entry != previous {
				return p, nil, errors.New("续接只能绑定当前镜头的上一镜真实视频")
			}

			all, _ := s.dramaMedias(w.ID)
			ok := false
			for _, m := range all {
				if m.ID == p.Source.ID && m.Hash == p.Source.Hash && m.Approved && m.Current && m.Selected {
					*p.Source = m
					ok = true
				}
			}
			if !ok {
				return p, nil, errors.New("续接来源不存在或不是上一镜选用版本")
			}
			inputs["@video-choice:"+p.Source.Entry] = p.Source.ID
			if e = s.verifyDramaMedia(*p.Source); e != nil {
				return p, nil, e
			}
		}
	case "voice":
		if p.Document != "script" {
			return p, nil, errors.New("配音必须来自剧本场景")
		}
		lines := []string{}
		for _, line := range strings.Split(entry.Body, "\n") {
			if !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "-") {
				parts := strings.SplitN(line, "：", 2)
				if len(parts) == 2 {
					lines = append(lines, parts[1])
				}
			}
		}
		p.Prompt = strings.Join(lines, "\n")
		p.Duration = float64(w.Duration)
	case "music":
		if p.Document != "motion" || !strings.HasPrefix(entry.ID, "MUSIC-") {
			return p, nil, errors.New("音乐必须来自视频提示词的MUSIC时间线条目")
		}
		p.Prompt = copyable(entry.Body, "可复制提示词")
		p.Duration = dramaSeconds(entry.Fields["时长"])
		if p.Duration <= 0 {
			p.Duration = float64(w.Duration)
		}
		p.Mode = "instrumental"
	}
	if e != nil {
		return p, nil, e
	}
	if p.Operation == "edit" {
		if p.Kind != "image" || p.Source == nil || p.Changes == "" || p.Preserve == "" {
			return p, nil, errors.New("图片编辑需要原图、修改内容和必须保留内容")
		}
		all, _ := s.dramaMedias(w.ID)
		found := false
		for _, m := range all {
			if m.ID == p.Source.ID && m.Kind == "image" && m.Approved && m.Current {
				p.Source = &m
				found = true
				break
			}
		}
		if !found {
			return p, nil, errors.New("编辑底图未确认")
		}
		for _, b := range p.Bindings {
			if b.Media.File == p.Source.File {
				return p, nil, errors.New("编辑底图不能重复作为辅助参考")
			}
		}
		p.Prompt = "修改内容：" + p.Changes + "\n必须保留：" + p.Preserve + "\n" + p.Prompt
	} else if p.Kind != "video" {
		p.Source = nil
	}
	if p.PromptOverride != "" {
		p.Prompt = p.PromptOverride
	}
	if strings.TrimSpace(p.Prompt) == "" {
		return p, nil, errors.New("条目缺少可复制正文或可发声台词")
	}
	if p.TailVideoID != "" {
		p.Prompt = "以输入的上一镜真实尾帧作为严格首帧，从其中可见姿态与构图开始，仅执行本镜后续动作；不得先跳回本镜计划分镜图。\n" + p.Prompt
	}

	if p.Operation == "regenerate" {
		p.Prompt = "本次重新生成的修改建议：" + p.Changes + "\n仅制作当前条目一个新版本，保留其他条目。\n" + p.Prompt
	}
	if p.Kind == "image" && len(p.Bindings) > 0 {
		sources := []ReferenceSource{}
		limit := 5
		if p.Operation == "edit" {
			limit = 4
		}
		for _, b := range p.Bindings {
			if len(p.Bindings) > limit && !contains([]string{"character", "scene", "prop"}, b.Media.Category) {
				return p, nil, errors.New("超量参考须先明确角色、场景、道具分类；无法分类的参考请主动精简")
			}
			category := b.Media.Category
			if category == "" {
				category = "other"
			}
			sources = append(sources, ReferenceSource{Image: Image{File: b.Media.File, Hash: b.Media.Hash, Approved: true}, Name: b.Slot, Kind: category})
		}
		plan, _, err := s.prepareReferenceSources(sources, limit)
		if err != nil {
			return p, nil, err
		}
		p.ReferencePlan = &plan
		p.Prompt = referencePlanPrompt(plan) + p.Prompt
	}

	if p.Kind == "video" && p.VideoOptions != nil {
		if err := validateDramaVideoOptions(cfg, p, *p.VideoOptions); err != nil {
			return p, nil, err
		}
	}
	if cfg.Mode != "mock" {
		ep, err := resolveEndpoint(cfg, p.Kind)
		if err != nil {
			return p, nil, err
		}
		// CLI model omission intentionally uses the CLI configuration, matching cliImage.
		if ep.Model == "" && ep.Transport != "cli" {
			return p, nil, errors.New("请先配置模型")
		}
		if p.Kind == "video" {
			if (p.Mode == "reference" || p.Mode == "continue") && strings.Contains(strings.ToLower(ep.Model), "seedance-1") {
				return p, nil, errors.New("此模型不支持多参考或视频续接")
			}
			if !strings.Contains(strings.ToLower(ep.Model), "seedance") {
				return p, nil, errors.New("当前视频执行仅支持方舟Seedance协议，请选择兼容模型")
			}
			pol := dramaProfile(cfg, w.Profile.Mode, w.Profile.Language)
			if p.VideoOptions == nil && profileHash(pol) != profileHash(w.Profile) {
				return p, nil, errors.New("视频服务与作品制作档案不同，请先采用当前服务档案并复核分镜和视频提示词")
			}
			if !pol.Known {
				return p, nil, errors.New("当前模型原生时长未知，请配置受支持模型")
			}
			if p.Duration < float64(pol.Min) || p.Duration > float64(pol.Max) {
				return p, nil, errors.New("镜长超出所选模型范围，请先修订分镜")
			}
		}
		if p.Kind == "music" && ep.Model != "music-3.0" {
			return p, nil, errors.New("音乐适配当前仅支持MiniMax music-3.0")
		}
	}
	return p, inputs, nil
}
func (s *Studio) dramaProductionRoutes(g *gin.RouterGroup) {
	s.dramaVideoVariantRoutes(g)
	g.GET("/works/:id/media", func(c *gin.Context) { m, e := s.dramaMedias(c.Param("id")); result(c, m, e) })
	g.POST("/works/:id/media/:media/frame", func(c *gin.Context) {
		s.dramaMu.Lock()
		defer s.dramaMu.Unlock()
		w, e := s.dramaWork(c.Param("id"))
		if e != nil {
			result(c, nil, e)
			return
		}
		all, e := s.dramaMedias(w.ID)
		if e != nil {
			result(c, nil, e)
			return
		}
		for _, m := range all {
			if m.ID == c.Param("media") {
				frame, e := s.padDramaFrame(w, m)
				result(c, frame, e)
				return
			}
		}
		result(c, nil, errors.New("图片不存在"))
	})

	g.POST("/works/:id/media/import", func(c *gin.Context) {
		var v struct {
			Entry string `json:"entry"`
			Name  string `json:"name"`
			Data  string `json:"data"`
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 48<<20)
		if e := c.ShouldBindJSON(&v); e != nil {
			result(c, nil, errors.New("导入请求过大或格式错误"))
			return
		}
		id := c.Param("id")
		if _, e := s.dramaWork(id); e != nil {
			result(c, nil, e)
			return
		}
		b, e := base64.StdEncoding.DecodeString(v.Data)
		if e != nil || len(b) == 0 || len(b) > 32<<20 {
			result(c, nil, errors.New("导入文件须小于32MB"))
			return
		}
		ext := strings.ToLower(filepath.Ext(v.Name))
		kind := ""
		switch ext {
		case ".png", ".jpg", ".jpeg", ".webp":
			kind = "image"
		case ".wav", ".mp3":
			kind = "audio"
		default:
			result(c, nil, errors.New("仅支持图片、WAV或MP3"))
			return
		}
		m := DramaMedia{ID: uid(), WorkID: id, Entry: v.Entry, Kind: kind, Hash: digest(b)}
		file, e := s.writeMedia(id, m.ID+ext, b)
		if e != nil {
			result(c, nil, e)
			return
		}
		m.File = file
		m.Path = s.dramaMediaPath(m.WorkID, filepath.Base(file))
		if kind == "image" {
			config, _, err := image.DecodeConfig(bytes.NewReader(b))
			if err != nil || config.Width < 1 || config.Height < 1 || config.Width > 4096 || config.Height > 4096 {
				e = errors.New("图片无法解码或尺寸超限")
			}
		} else {
			e = s.requireAudio(c.Request.Context(), file)
		}
		if e == nil {
			e = s.saveDramaMedia(m)
		}
		result(c, m, e)
	})
	g.POST("/works/:id/media/:media/approve", func(c *gin.Context) {
		var v struct {
			Category string `json:"category"`
		}
		if c.Request.ContentLength != 0 && !bind(c, &v) {
			return
		}
		if v.Category != "" && !contains([]string{"character", "scene", "prop", "composition"}, v.Category) {
			result(c, nil, errors.New("素材分类无效"))
			return
		}
		s.dramaMu.Lock()
		defer s.dramaMu.Unlock()
		all, e := s.dramaMedias(c.Param("id"))
		for _, m := range all {
			if m.ID == c.Param("media") {
				e = s.verifyDramaMedia(m)
				if e == nil {
					m.Approved = true
					if v.Category != "" && m.Kind == "image" {
						m.Category = v.Category
					}
					e = s.saveDramaMedia(m)
				}
				result(c, m, e)
				return
			}
		}
		if e == nil {
			e = errors.New("媒体不存在")
		}
		result(c, nil, e)
	})
	g.POST("/works/:id/quotes", func(c *gin.Context) {
		var p DramaProduction
		if !bind(c, &p) {
			return
		}
		s.dramaMu.Lock()
		defer s.dramaMu.Unlock()
		w, e := s.dramaWork(c.Param("id"))
		if e != nil {
			result(c, nil, e)
			return
		}
		cfg, e := s.store.settings()
		if e != nil {
			result(c, nil, e)
			return
		}
		cfg, e = s.dramaVideoConfig(cfg, p)
		if e != nil {
			result(c, nil, e)
			return
		}
		p, inputs, e := s.buildDramaProduction(w, p, cfg)
		if e != nil {
			result(c, nil, e)
			return
		}
		var n int
		e = s.store.db.QueryRow("SELECT count(*) FROM attempts a JOIN tasks t ON a.task_id=t.id WHERE t.project_id=? AND a.status='submitting'", w.ID).Scan(&n)
		if e != nil || n > 0 {
			result(c, nil, errors.New("存在未知提交，请先恢复原任务，不创建新调用"))
			return
		}
		q := DramaQuote{ID: uid(), WorkID: w.ID, Inputs: inputs, SettingsVersion: cfg.Version, Spec: p, Unknown: cfg.Mode != "mock", Reason: "费用未知，无可靠上限；参考单价不代替准确费用。仅执行当前条目一次。", Expires: time.Now().Add(15 * time.Minute).UTC().Format(time.RFC3339), Config: cfg}
		if p.Kind == "batch" {
			q.Reason = fmt.Sprintf("费用未知，无可靠上限；共%d项，每项仅执行一次，已存在的媒体不重做。", len(p.Items))
		}
		if cfg.Mode == "mock" {
			q.Reason = "本地模拟，不调用模型；程序示意图或静音媒体"
		}
		capability := p.Kind
		if len(p.Items) > 0 {
			capability = p.Items[0].Kind
		}
		ep, _ := resolveEndpoint(cfg, capability)
		q.Model = ep.Model
		for _, vendor := range cfg.Vendors {
			if vendor.ID == ep.VendorID {
				q.Vendor = vendor.Name
			}
		}
		applyDramaVideoEstimate(&q, w, cfg)
		applyDramaImageCallScope(&q, cfg)
		type wire DramaQuote
		b := jsonBytes(struct {
			wire
			PrivateConfig Settings `json:"private_config"`
		}{wire(q), cfg})
		_, e = s.store.db.Exec("INSERT INTO drama_quotes VALUES(?,?)", q.ID, b)
		result(c, q, e)
	})
	g.POST("/quotes/:id/confirm", func(c *gin.Context) {
		var v struct {
			AllowUnknown bool `json:"allow_unknown"`
		}
		if !bind(c, &v) {
			return
		}
		s.dramaMu.Lock()
		defer s.dramaMu.Unlock()
		var raw []byte
		e := s.store.db.QueryRow("SELECT body FROM drama_quotes WHERE id=?", c.Param("id")).Scan(&raw)
		if e != nil {
			result(c, nil, e)
			return
		}
		var q DramaQuote
		var priv struct {
			Config Settings `json:"private_config"`
		}
		if e = json.Unmarshal(raw, &q); e != nil {
			result(c, nil, e)
			return
		}
		json.Unmarshal(raw, &priv)
		q.Config = priv.Config
		if q.TaskID != "" {
			t, e := s.store.task(q.TaskID)
			result(c, t, e)
			return
		}
		expires, _ := time.Parse(time.RFC3339, q.Expires)
		cfg, e := s.store.settings()
		if e != nil || time.Now().After(expires) || cfg.Version != q.SettingsVersion {
			result(c, nil, errConflict)
			return
		}
		if q.Unknown && !v.AllowUnknown {
			result(c, nil, errors.New("请明确接受未知费用及无可靠上限"))
			return
		}
		for kind, hash := range q.Inputs {
			if !s.dramaDependencyCurrent(q.WorkID, kind, hash, true) {
				result(c, nil, errConflict)
				return
			}
		}
		for _, b := range q.Spec.Bindings {
			if e = s.verifyDramaMedia(b.Media); e != nil {
				result(c, nil, e)
				return
			}
		}
		if q.Spec.Source != nil {
			if e = s.verifyDramaMedia(*q.Spec.Source); e != nil {
				result(c, nil, e)
				return
			}
		}
		w, e := s.dramaWork(q.WorkID)
		if e != nil {
			result(c, nil, e)
			return
		}
		id := uid()
		j := DramaJob{ID: id, WorkID: w.ID, Kind: "produce", Inputs: q.Inputs, SkillVersion: dramaVersion, Production: &q.Spec}
		if e = s.dramaSubmissionReady(w.ID, q.Spec, cfg); e != nil {
			result(c, nil, e)
			return
		}
		t, e := s.newDramaTask(w, q.Config, j, &q)
		if e == nil {
			s.launchDrama(t.ID)
		}
		result(c, t, e)
	})
	s.dramaEditRoutes(g)
}
func (s *Studio) dramaVideoRequest(ctx context.Context, t Task, q Shot, model string, v VideoInput) (map[string]any, error) {
	content := []map[string]any{{"type": "text", "text": t.CreativePrompt}}
	for _, ref := range v.Images {
		if e := s.verifyImage(ref.Image); e != nil {
			return nil, e
		}
		b, e := os.ReadFile(s.media(ref.Image.File))
		if e != nil {
			return nil, e
		}
		content = append(content, map[string]any{"type": "image_url", "image_url": map[string]string{"url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(b)}, "role": ref.Role})
	}
	if v.Mode == "continue" || v.Mode == "edit_video" {
		if v.Mode == "edit_video" {
			b, err := readVideoFile(s.media(v.SourceVideo))
			if err != nil || digest(b) != v.SourceHash {
				return nil, errors.New("编辑源视频已变化，请重新报价")
			}
		}
		u, e := s.previousVideoURL(ctx, t.ProjectID, v)
		if e != nil {
			return nil, e
		}
		b, e := download(ctx, u, 100<<20)
		if e != nil || digest(b) != v.SourceHash {
			return nil, errors.New("上一段供应商视频已变化或失效")
		}
		content = append(content, map[string]any{"type": "video_url", "video_url": map[string]string{"url": u}, "role": "reference_video"})
	}
	body := videoTaskOptions(t.Input, q, model, v.Mode)
	body["content"] = content
	return body, nil
}
func (s *Studio) executeDramaProduction(ctx context.Context, t *Task, j *DramaJob) error {
	p := j.Production
	if p.Kind == "video" && p.VideoOptions != nil {
		t.Input.VideoResolution = p.VideoOptions.Resolution
	}

	if p.Kind == "batch" {
		return s.executeDramaBatch(ctx, t, j, s.executeDramaProduction)
	}

	if p.Kind == "render" {
		return s.renderDrama(ctx, t, j)
	}
	hasAttempt := false
	if t.Mode != "mock" && (p.Kind == "image" || p.Kind == "video") {
		key := "shot-drama-" + digest([]byte(p.Entry))[:16]
		if p.Kind == "video" {
			key = "video-" + strings.TrimPrefix(p.Entry, "MOTION-")
		}
		var status string
		err := s.store.db.QueryRow("SELECT status FROM attempts WHERE id=?", t.ID+":"+key).Scan(&status)
		if err == nil {
			hasAttempt = true
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}

	refs := []Image{}
	for _, b := range p.Bindings {
		if !hasAttempt {
			if e := s.verifyDramaMedia(b.Media); e != nil {
				return e
			}
		}
		refs = append(refs, Image{File: b.Media.File, Hash: b.Media.Hash, Approved: true})
	}
	if p.Source != nil && !hasAttempt {
		if e := s.verifyDramaMedia(*p.Source); e != nil {
			return e
		}
	}
	if p.Kind == "image" && p.ReferencePlan != nil && !hasAttempt {
		var err error
		refs, err = s.prepareDramaProductionImageRefs(t, p)
		if err != nil {
			return err
		}
	}

	m := DramaMedia{ID: digest([]byte(t.ID + ":" + p.Entry))[:24], WorkID: t.ProjectID, Entry: p.Entry, Kind: p.Kind, TaskID: t.ID, Duration: p.Duration, Mock: t.Mode == "mock", SourceHash: j.Inputs[p.Document]}
	var file string
	var e error
	switch p.Kind {
	case "image":
		if p.Operation == "edit" {
			refs = append([]Image{{File: p.Source.File, Hash: p.Source.Hash, Approved: true}}, refs...)
			m.Parent = p.Source.ID
		}
		imageTask := *t
		imageTask.ImageAspect = p.ImageAspect
		var im Image
		calls := 1
		if p.ImageAspect != "" && p.ImageMaxCalls == 2 && p.ImageRetryPrompt != "" {
			calls = 2
		}
		for attempt := 0; attempt < calls; attempt++ {
			key := "shot-drama-" + digest([]byte(p.Entry))[:16]
			prompt := p.Prompt
			if attempt == 1 {
				key += "-aspect-retry-1"
				prompt += "\n" + p.ImageRetryPrompt
				// Recovery may resume between the first receipt and its retry.
				// A new call must reconstruct the frozen boards and verify inputs;
				// an existing retry receipt must remain recoverable without them.
				var status string
				lookup := s.store.db.QueryRow("SELECT status FROM attempts WHERE id=?", t.ID+":"+key).Scan(&status)
				if errors.Is(lookup, sql.ErrNoRows) {
					refs, e = s.prepareDramaProductionImageRefs(t, p)
					if e != nil {
						break
					}
					if p.Operation == "edit" {
						refs = append([]Image{{File: p.Source.File, Hash: p.Source.Hash, Approved: true}}, refs...)
					}
				} else if lookup != nil {
					e = lookup
					break
				}
			}
			im, e = s.makeImage(ctx, &imageTask, key, prompt, refs, p.Sheet)
			if e != nil {
				break // An unknown submission or other failure never spends another call.
			}
			file = im.File
			if p.ImageAspect != "" {
				e = s.validateDramaImageAspect(im, p.ImageAspect)
			}
			if e == nil || !errors.Is(e, errDramaImageAspect) {
				break
			}
			if attempt == calls-1 && calls == 2 {
				e = fmt.Errorf("已达本镜已确认的最多2次调用范围，停止生成：%w", e)
			}
		}
		if e == nil && p.Sheet {
			views, err := s.cropSheet(im)
			if err != nil {
				return err
			}
			for i, v := range views {
				child := m
				child.ID = m.ID + fmt.Sprint(i)
				child.File = v.File
				child.Hash = v.Hash
				child.Path = s.dramaMediaPath(child.WorkID, filepath.Base(v.File))
				child.Parent = m.ID
				if e = s.saveDramaMedia(child); e != nil {
					return e
				}
			}
		}
	case "video":
		q := Shot{ID: strings.TrimPrefix(p.Entry, "MOTION-"), Duration: int(p.Duration), Title: p.Entry, Selected: 0}
		v := VideoInput{ShotID: q.ID, Mode: p.Mode}
		if p.Mode == "edit_video" {
			if p.VideoEditInput == nil || p.Source == nil {
				return errors.New("视频编辑缺少已确认的原视频输入快照")
			}
			v = *p.VideoEditInput
			m.Parent = p.Source.ID
		}
		for _, b := range p.Bindings {
			role := "reference_image"
			if b.Use == "起始帧" {
				role = "first_frame"
			}
			if b.Use == "结束帧" {
				role = "last_frame"
			}
			v.Images = append(v.Images, VideoImage{Name: b.Slot, Role: role, Image: Image{File: b.Media.File, Hash: b.Media.Hash, Approved: true}})
		}
		if len(refs) > 0 {
			q.Candidates = refs
		} else if t.Mode == "mock" {
			// The local silent mock renderer needs a placeholder frame; this is
			// not an image model call or an input sent to the video provider.
			im, err := s.makeImage(ctx, t, "shot-mock-start", p.Prompt, nil, false)
			if err != nil {
				return err
			}
			q.Candidates = []Image{im}
		}
		if p.Source != nil && !hasAttempt && p.Mode != "edit_video" {
			v.SourceVideo = p.Source.File
			v.SourceHash = p.Source.Hash
			v.SourceTaskID = p.Source.TaskID
			old, err := s.dramaJob(p.Source.TaskID)
			if err != nil || old.Production == nil {
				return errors.New("续接缺少原任务")
			}
			sourceEntry := old.Production.Entry
			if old.Production.Kind == "batch" {
				for _, entry := range old.Production.Items {
					if entry.Entry == p.Source.Entry {
						sourceEntry = entry.Entry
						break
					}
				}
			}
			sourceID := strings.TrimPrefix(sourceEntry, "MOTION-")
			if t.Mode != "mock" {
				if e = s.store.db.QueryRow("SELECT remote_id FROM attempts WHERE id=? AND status='received'", p.Source.TaskID+":video-"+sourceID).Scan(&v.SourceRemoteID); e != nil {
					return errors.New("续接缺少原供应商回执")
				}
			}
		}
		t.VideoInputs = []VideoInput{v}
		t.CreativePrompt = p.Prompt
		if t.Mode == "mock" {
			file, e = s.makeVideo(ctx, t, q)
		} else {
			file, e = s.remoteVideo(ctx, t, q)
		}
	case "voice":
		t.Input.Shots = []Shot{{Narration: p.Prompt}}
		file, e = s.makeVoice(ctx, t)
	case "music":
		if t.Mode == "mock" {
			file, e = s.makeVoice(ctx, t)
		} else {
			ep, err := resolveEndpoint(t.Config, "music")
			if err != nil {
				return err
			}
			b, err := s.paid(ctx, t, "music", ep, "/music_generation", "application/json", jsonBytes(map[string]any{"model": "music-3.0", "prompt": p.Prompt, "is_instrumental": true, "output_format": "hex", "stream": false, "audio_setting": map[string]any{"sample_rate": 44100, "bitrate": 256000, "format": "mp3"}}))
			if err != nil {
				return err
			}
			var response struct {
				Data struct {
					Audio string `json:"audio"`
				} `json:"data"`
				Base *struct {
					Code int `json:"status_code"`
				} `json:"base_resp"`
			}
			if json.Unmarshal(b, &response) != nil || response.Base == nil || response.Base.Code != 0 {
				return errors.New("音乐供应商未返回成功结果，回执已保留")
			}
			audio, err := hex.DecodeString(response.Data.Audio)
			if err != nil || len(audio) == 0 {
				return errors.New("音乐内容无效，回执已保留")
			}
			file, e = s.writeMedia(t.ProjectID, t.ID+"-music.mp3", audio)
		}
	}
	if e != nil {
		return e
	}
	if file == "" {
		return errors.New("生产没有输出文件")
	}
	m.File = file
	m.Path = s.dramaMediaPath(m.WorkID, filepath.Base(file))
	b, e := os.ReadFile(s.media(file))
	if e != nil {
		return e
	}
	m.Hash = digest(b)
	if p.Kind == "voice" || p.Kind == "music" {
		if e = s.requireAudio(ctx, file); e != nil {
			return e
		}
		probe, err := s.probeDrama(ctx, file)
		if err != nil {
			return err
		}
		m.Duration = dramaSeconds(probe.Format.Duration)
	}
	if p.Kind == "video" {
		m.VideoInfo = s.dramaVideoInfo(t.Config, p)
		m.VideoInfo.Created = t.Created
		if p.CompareSource != nil {
			m.Parent = p.CompareSource.ID
		}
		probe, err := s.probeDrama(ctx, file)
		if err != nil {
			return err
		}
		for _, stream := range probe.Streams {
			if stream.Type == "video" {
				m.VideoInfo.Width = stream.Width
				m.VideoInfo.Height = stream.Height
			}
		}
	}
	return s.saveDramaMedia(m)
}

func (s *Studio) dramaSubmissionReady(id string, p DramaProduction, cfg Settings) error {
	if p.Kind == "batch" {
		jobs, e := s.store.tasks(id)
		if e != nil {
			return e
		}
		for _, t := range jobs {
			if t.Status != "running" && t.Status != "queued" {
				continue
			}
			j, e := s.dramaJob(t.ID)
			if e != nil || j.Production == nil {
				continue
			}
			for _, item := range p.Items {
				if j.Production.Kind == item.Kind || j.Production.Kind == "batch" {
					return errors.New("同类任务执行中，请先等待当前批次")
				}
			}
		}
		for _, item := range p.Items {
			if e := s.dramaSubmissionReady(id, item, cfg); e != nil {
				return e
			}
		}
		return nil
	}

	var n int
	if e := s.store.db.QueryRow("SELECT count(*) FROM attempts a JOIN tasks t ON a.task_id=t.id WHERE t.project_id=? AND a.status='submitting'", id).Scan(&n); e != nil {
		return e
	}
	if n > 0 {
		return errors.New("存在未知提交，请只恢复原任务")
	}
	jobs, e := s.store.tasks(id)
	if e != nil {
		return e
	}
	active := 0
	for _, t := range jobs {
		if t.Status != "queued" && t.Status != "running" {
			continue
		}
		j, e := s.dramaJob(t.ID)
		if e != nil || j.Production == nil {
			continue
		}
		if j.Production.Kind == "batch" {
			for _, item := range j.Production.Items {
				if item.Kind == p.Kind {
					return errors.New("同类批量任务执行中，请等待后再重生成")
				}
			}
		}
		if j.Production.Entry == p.Entry && j.Production.Kind == p.Kind {
			return errors.New("当前条目已有执行任务，请勿重复提交")
		}
		if j.Production.Kind == p.Kind {
			active++
		}
	}
	limit := 1
	if p.Kind == "image" {
		limit = cfg.ImageConcurrency
	}
	if p.Kind == "video" {
		limit = cfg.VideoConcurrency
	}
	if active >= limit {
		return errors.New("已达到此类媒体的并发上限，请等待当前任务")
	}
	return nil
}
