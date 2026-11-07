package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type VideoImage struct {
	Name  string `json:"name"`
	Role  string `json:"role"`
	Image Image  `json:"image"`
}
type VideoInput struct {
	ShotID         string       `json:"shot_id"`
	Mode           string       `json:"mode"`
	EndShotID      string       `json:"end_shot_id,omitempty"`
	Images         []VideoImage `json:"images"`
	SourceVideo    string       `json:"source_video,omitempty"`
	SourceHash     string       `json:"source_hash,omitempty"`
	SourceShotID   string       `json:"source_shot_id,omitempty"`
	SourceTaskID   string       `json:"source_task_id,omitempty"`
	SourceRemoteID string       `json:"source_remote_id,omitempty"`
}

var videoModeNames = map[string]string{"first_frame": "独立镜头·严格首帧", "continue": "延续上一镜", "first_last": "首尾帧控制", "reference": "多参考·开场引导", "edit_video": "基于本镜原视频修改"}

func (s *Studio) videoInput(p Project, q Shot, cfg Settings, mode, end string) (VideoInput, error) {
	if mode == "" {
		mode = "first_frame"
	}
	v := VideoInput{ShotID: q.ID, Mode: mode, EndShotID: end, Images: []VideoImage{}}
	if videoModeNames[mode] == "" {
		return v, errors.New("未知视频生成方式")
	}
	if mode != "first_last" && end != "" {
		return v, errors.New("仅首尾帧模式可设置结束画面")
	}
	if mode == "edit_video" {
		return s.videoEditInput(p, q, cfg)
	}
	add := func(name, role string, im Image) error {
		if e := s.verifyImage(im); e != nil {
			return e
		}
		if role == "first_frame" || role == "last_frame" {
			b, e := os.ReadFile(s.media(im.File))
			if e != nil {
				return e
			}
			info, _, e := image.DecodeConfig(bytes.NewReader(b))
			if e != nil {
				return e
			}
			w, h := frameSize(p)
			difference := float64(info.Width)/float64(info.Height) - float64(w)/float64(h)
			if difference < 0 {
				difference = -difference
			}
			if difference > 0.02 {
				return errors.New("严格首尾帧图片画幅与项目不符，请先重新生成正确画幅的分镜图")
			}
		}
		v.Images = append(v.Images, VideoImage{Name: name, Role: role, Image: im})
		return nil
	}
	if q.Selected < 0 || q.Selected >= len(q.Candidates) {
		return v, errors.New("本镜图片未完成")
	}
	role := "first_frame"
	if mode == "reference" || mode == "continue" {
		role = "reference_image"
	}
	if mode != "continue" {
		if e := add("本镜分镜图", role, q.Candidates[q.Selected]); e != nil {
			return v, e
		}
	}
	if mode == "first_last" {
		found := false
		for _, tail := range p.Shots {
			if tail.ID == end && tail.Approved && tail.Selected >= 0 && tail.Selected < len(tail.Candidates) {
				if e := add("结束画面："+tail.Title, "last_frame", tail.Candidates[tail.Selected]); e != nil {
					return v, e
				}
				found = true
				break
			}
		}
		if !found {
			return v, errors.New("请选择一张已确认分镜图作为结束画面")
		}
	}
	if mode == "reference" || mode == "continue" {
		seen := map[string]bool{}
		for _, im := range v.Images {
			seen[im.Image.Hash] = true
		}
		for _, id := range q.References {
			for _, a := range p.Assets {
				if a.ID != id {
					continue
				}
				if a.Master == nil || !a.Master.Approved {
					return v, errors.New("固定参考素材未确认")
				}
				if seen[a.Master.Hash] {
					continue
				}
				if e := add(a.Name, "reference_image", *a.Master); e != nil {
					return v, e
				}
				seen[a.Master.Hash] = true
			}
		}
		if len(v.Images) > 9 {
			return v, errors.New("参考图片超出当前适配上限9张，请精简本镜引用")
		}
	}
	if mode == "continue" {
		index := -1
		for i, shot := range p.Shots {
			if shot.ID == q.ID {
				index = i
				break
			}
		}
		if index < 1 {
			return v, errors.New("第一镜不能延续上一镜")
		}
		prev := p.Shots[index-1]
		if prev.Video == "" || !prev.Approved || !videoDependencyCurrent(p, index-1) {
			return v, errors.New("请先完成并检查上一镜视频；续接镜头必须按顺序制作")
		}
		b, e := readVideoFile(s.media(prev.Video))
		if e != nil {
			return v, e
		}
		v.SourceVideo, v.SourceHash, v.SourceShotID = prev.Video, digest(b), prev.ID
		if cfg.Mode != "mock" {
			taskID := prev.VideoTaskID
			if taskID == "" {
				return v, errors.New("上一镜缺少供应商任务记录，不能原生延续；可改用独立镜头")
			}
			old, e := s.store.task(taskID)
			if e != nil || old.ProjectID != p.ID {
				return v, errors.New("上一镜任务记录不可用")
			}
			matched := false
			for _, x := range old.Input.Shots {
				if x.ID == prev.ID && x.Video == prev.Video {
					matched = true
				}
			}
			if !matched {
				return v, errors.New("上一镜视频与原任务快照不一致")
			}
			v.SourceTaskID = taskID
			if e = s.store.db.QueryRow("SELECT remote_id FROM attempts WHERE id=? AND task_id=? AND status='received'", taskID+":video-"+prev.ID, taskID).Scan(&v.SourceRemoteID); e != nil || v.SourceRemoteID == "" {
				return v, errors.New("上一镜缺少已接收的供应商任务ID")
			}
		}
	}
	if cfg.Mode == "remote" {
		ep, e := resolveEndpoint(cfg, "video")
		if e != nil {
			return v, e
		}
		lower := strings.ToLower(ep.Model)
		if (mode == "reference" || mode == "continue") && (strings.Contains(lower, "seedance-1") || strings.Contains(lower, "seedance-1.")) {
			return v, errors.New("多参考与视频延续需支持全模态输入的Seedance 2.0/2.5模型")
		}
	}
	return v, nil
}
func readVideoFile(path string) ([]byte, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, 100<<20))
	if e != nil {
		return nil, e
	}
	if len(b) == 0 || len(b) >= 100<<20 {
		return nil, errors.New("参考视频为空或过大")
	}
	return b, nil
}
func videoDependencyCurrent(p Project, index int) bool {
	q := p.Shots[index]
	if q.VideoMode == "first_last" {
		for _, tail := range p.Shots {
			if tail.ID == q.VideoEndShotID && tail.Selected >= 0 && tail.Selected < len(tail.Candidates) {
				return tail.Approved && tail.Candidates[tail.Selected].Hash == q.VideoEndImageHash
			}
		}
		return false
	}
	if q.VideoMode != "continue" {
		return true
	}
	return index > 0 && q.VideoSourceFile == p.Shots[index-1].Video && p.Shots[index-1].Video != "" && videoDependencyCurrent(p, index-1)
}
func inputFor(t Task, id string) (VideoInput, bool) {
	for _, v := range t.VideoInputs {
		if v.ShotID == id {
			return v, true
		}
	}
	return VideoInput{}, false
}
func recordVideoInput(t *Task, index int) {
	q := &t.Input.Shots[index]
	q.VideoTaskID = t.ID
	if v, ok := inputFor(*t, q.ID); ok {
		q.VideoMode = v.Mode
		q.VideoEndShotID = v.EndShotID
		q.VideoEndImageHash = ""
		for _, ref := range v.Images {
			if ref.Role == "last_frame" {
				q.VideoEndImageHash = ref.Image.Hash
			}
		}
		q.VideoSourceFile = v.SourceVideo
	}
}

func (s *Studio) previousVideoURL(ctx context.Context, pid string, v VideoInput) (string, error) {
	old, e := s.store.task(v.SourceTaskID)
	if e != nil || old.ProjectID != pid {
		return "", errors.New("原视频任务不可用")
	}
	ep, e := resolveEndpoint(old.Config, "video")
	if e != nil {
		return "", e
	}
	key, e := s.credential(ep)
	if e != nil {
		return "", e
	}
	req, e := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(ep.Base, "/")+"/contents/generations/tasks/"+url.PathEscape(v.SourceRemoteID), nil)
	if e != nil {
		return "", e
	}
	req.Header.Set("Authorization", "Bearer "+key)
	client := &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, e := client.Do(req)
	if e != nil {
		return "", errors.New("查询参考视频的供应商任务失败，未提交新生成")
	}
	defer resp.Body.Close()
	var status struct {
		Status  string `json:"status"`
		Content struct {
			URL string `json:"video_url"`
		} `json:"content"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&status) != nil || status.Status != "succeeded" {
		return "", errors.New("参考视频的供应商结果不可用，未提交新生成")
	}
	if e = publicURL(status.Content.URL); e != nil {
		return "", e
	}
	return status.Content.URL, nil
}

func (s *Studio) plannedVideoRequest(ctx context.Context, t Task, q Shot, model string) (map[string]any, error) {
	v, ok := inputFor(t, q.ID)
	if !ok { // Historical confirmed tasks keep their original single-reference request.
		b, e := os.ReadFile(s.media(q.Candidates[q.Selected].File))
		if e != nil {
			return nil, e
		}
		body := taskVideoRequest(t, q, model, b)
		if t.PromptTemplatesVersion > 0 {
			promptShot := q
			if t.Stage == "shot_video" && t.Feedback != "" {
				promptShot.Description += "。本次修改建议：" + t.Feedback
			}
			prompt, err := taskVideoPrompt(t, promptShot)
			if err != nil {
				return nil, err
			}
			body["content"].([]map[string]any)[0]["text"] = prompt
		}
		return body, nil
	}
	content := []map[string]any{}
	promptShot := q
	if t.Stage == "shot_video" && t.Feedback != "" {
		promptShot.Description += "。本次修改建议：" + t.Feedback
	}
	basePrompt, err := taskVideoPrompt(t, promptShot)
	if err != nil {
		return nil, err
	}
	prompt := fmt.Sprintf("本镜输出仅%d秒，原描述中的全片时间位置不延长本段时长。", q.Duration) + basePrompt + "。始终保持角色身份、毛色体型或服装、场景布局和道具特征一致，不新增角色或多余肢体。"
	if v.Mode == "reference" {
		prompt += "。以图片1作为开场构图引导；本模式不是严格首帧。"
	}
	for i, ref := range v.Images {
		if e := s.verifyImage(ref.Image); e != nil {
			return nil, e
		}
		b, e := os.ReadFile(s.media(ref.Image.File))
		if e != nil {
			return nil, e
		}
		content = append(content, map[string]any{"type": "image_url", "image_url": map[string]string{"url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(b)}, "role": ref.Role})
		prompt += fmt.Sprintf("。图片%d：%s", i+1, ref.Name)
		if ref.Role == "reference_image" && !(v.Mode == "reference" && i == 0) {
			prompt += "，只固定身份、毛色/服装、场景或道具特征，不复制参考图的机位、纯色背景或拼图布局"
		}
	}
	if v.Mode == "continue" || v.Mode == "edit_video" {
		b, e := readVideoFile(s.media(v.SourceVideo))
		if e != nil || digest(b) != v.SourceHash {
			return nil, errors.New("参考视频已变化，请重新报价")
		}
		u, e := s.previousVideoURL(ctx, t.ProjectID, v)
		if e != nil {
			return nil, e
		}
		remote, e := download(ctx, u, 100<<20)
		if e != nil || digest(remote) != v.SourceHash {
			return nil, errors.New("参考视频的供应商链接已过期或内容变化，未提交新生成")
		}
		content = append(content, map[string]any{"type": "video_url", "video_url": map[string]string{"url": u}, "role": "reference_video"})
		if v.Mode == "edit_video" {
			prompt = "编辑视频1：仅按本次修改建议修改当前镜头，保留未要求改变的画面、角色身份、镜头时长和既定台词。输出修改后的完整本镜视频，不延长、不续写、不拼接原片。" + prompt
		} else {
			prompt = "向后延长视频1，仅输出接在原视频之后的新片段，延续末段的动作速度、机位、位置、光照和声音，不重播原片。" + prompt
		}
	}
	content = append([]map[string]any{{"type": "text", "text": prompt}}, content...)
	body := videoTaskOptions(t.Input, q, model, v.Mode)
	body["content"] = content
	if len(jsonBytes(body)) >= 64<<20 {
		return nil, errors.New("视频请求超过64MB，请减少本镜参考图片")
	}
	return body, nil
}

func videoTaskOptions(p Project, q Shot, model, mode string) map[string]any {
	ratio := projectAspect(p)
	lower := strings.ToLower(model)
	is25 := strings.Contains(lower, "seedance-2-5") || strings.Contains(lower, "seedance-2.5")
	if mode == "first_frame" || mode == "first_last" || mode == "continue" || mode == "edit_video" {
		ratio = "adaptive"
	}
	body := map[string]any{"model": model, "duration": q.Duration, "ratio": ratio, "resolution": "480p", "generate_audio": dialogueMode(p)}
	if is25 && mode == "edit_video" {
		body["duration"] = -1 // Provider edit mode preserves the bound source duration.
		body["omni_reference_task_type"] = "edit"
	}
	if is25 && mode == "continue" {
		body["omni_reference_task_type"] = "extend"
	}
	if is25 && mode == "reference" {
		body["omni_reference_task_type"] = "reference"
	}
	return body
}
