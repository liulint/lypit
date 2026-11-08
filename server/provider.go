package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Provider POSTs have durable intents. An interrupted POST without a response is never retried.
func (s *Studio) paid(ctx context.Context, t *Task, key string, ep Endpoint, path, ctype string, body []byte) ([]byte, error) {
	id := t.ID + ":" + key
	var status string
	var response []byte
	e := s.store.db.QueryRow("SELECT status,response FROM attempts WHERE id=?", id).Scan(&status, &response)
	if e == nil {
		if status == "received" {
			return response, nil
		}
		return nil, errors.New("供应商提交结果未知，已阻止重复付费请求；请在供应商控制台核对，勿反复新建任务")
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	if e = ctx.Err(); e != nil {
		return nil, e
	}
	secret, e := s.credential(ep)
	if e != nil {
		return nil, e
	}
	if _, e = s.store.db.Exec("INSERT INTO attempts(id,task_id,status) VALUES(?,?,'submitting')", id, t.ID); e != nil {
		return nil, e
	}
	req, e := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(ep.Base, "/")+path, bytes.NewReader(body))
	if e != nil {
		return nil, e
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Content-Type", ctype)
	client := &http.Client{Timeout: 8 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, e := client.Do(req)
	if e != nil {
		return nil, errors.New("供应商请求中断，结果未知。恢复时只处理已保存响应，不自动重发")
	}
	defer resp.Body.Close()
	b, e := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if e != nil || len(b) >= 32<<20 {
		return nil, errors.New("供应商响应未完整保存，结果待核对")
	}
	b = bytes.ReplaceAll(b, []byte(secret), []byte("[REDACTED]"))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = s.store.db.Exec("UPDATE attempts SET status='rejected',response=? WHERE id=?", b, id)
		return nil, fmt.Errorf("供应商返回 HTTP %d；不会自动重试，请核对模型、权限和参数", resp.StatusCode)
	}
	if _, e = s.store.db.Exec("UPDATE attempts SET status='received',response=? WHERE id=?", b, id); e != nil {
		return nil, errors.New("响应已返回但保存失败，请核对供应商任务，勿重新提交")
	}
	return b, nil
}
func (s *Studio) textJSON(ctx context.Context, t *Task, key, prompt string, out any) error {
	ep, e := resolveEndpoint(t.Config, "text")
	if e != nil {
		return e
	}
	if ep.Transport == "cli" {
		return s.cliTextJSON(ctx, t, key, prompt, ep, out)
	}
	body := map[string]any{"model": ep.Model, "messages": []map[string]string{{"role": "system", "content": "严格输出请求指定的JSON对象，不使用代码块。"}, {"role": "user", "content": prompt}}, "response_format": map[string]string{"type": "json_object"}}
	b, e := s.paid(ctx, t, key, ep, "/chat/completions", "application/json", jsonBytes(body))
	if e != nil {
		return e
	}
	var v struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if e = json.Unmarshal(b, &v); e != nil || len(v.Choices) == 0 {
		return errors.New("文本响应格式无效，原响应已保存")
	}
	content := strings.TrimSpace(v.Choices[0].Message.Content)
	if e = json.Unmarshal([]byte(content), out); e != nil {
		return errors.New("模型未返回有效JSON，原响应已保存；恢复不会再次推理")
	}
	return nil
}
func (s *Studio) remoteImage(ctx context.Context, t *Task, key, prompt string, refs []Image, sheet bool) ([]byte, error) {
	ep, e := resolveEndpoint(t.Config, "image")
	if e != nil {
		return nil, e
	}
	if ep.Transport == "cli" {
		return s.cliImage(ctx, t, key, prompt, refs, sheet, ep)
	}
	if strings.HasPrefix(t.Stage, "drama_") {
		var state string
		var receipt []byte
		err := s.store.db.QueryRow("SELECT status,response FROM attempts WHERE id=?", t.ID+":"+key).Scan(&state, &receipt)
		if err == nil {
			if state != "received" {
				return nil, errors.New("原图片提交结果未知，已阻止重新提交，请核对原回执")
			}
			return decodeImageReceipt(ctx, receipt)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}

	var referenceBytes [][]byte
	if t.Stage == "board_edit" || (t.ReferencePolicyVersion == referencePlanVersion || t.ReferencePolicyVersion == fourViewPolicyVersion) || t.ImageReferencePlan != nil {
		plan, prepared, err := s.prepareTaskReferences(t, key, refs)
		if err != nil {
			return nil, err
		}
		if t.ImageReferencePlan != nil && string(jsonBytes(plan)) != string(jsonBytes(t.ImageReferencePlan)) {
			return nil, errors.New("参考板或原图与已确认报价不一致，请重新报价；未调用模型")
		}
		referenceBytes = prepared
		if len(refs) > 0 {
			prompt = referencePlanPrompt(plan) + prompt
		}
	} else {
		// Historical confirmed API tasks retain their original reference payload.
		for _, im := range refs {
			if err := s.verifyImage(im); err != nil {
				return nil, err
			}
			b, err := os.ReadFile(s.media(im.File))
			if err != nil {
				return nil, err
			}
			referenceBytes = append(referenceBytes, b)
		}
	}
	size := "1024x1536"
	if landscapeImage(t.Input, key) {
		size = "1536x1024"
	}
	if sheet {
		size = "1024x1024"
	}
	path := "/images/generations"
	ctype := "application/json"
	body := jsonBytes(map[string]any{"model": ep.Model, "prompt": prompt, "n": 1, "size": size})
	if len(refs) > 0 {
		path = "/images/edits"
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		for k, v := range map[string]string{"model": ep.Model, "prompt": prompt, "n": "1", "size": size} {
			if e := w.WriteField(k, v); e != nil {
				return nil, e
			}
		}
		for index, b := range referenceBytes {
			name := fmt.Sprintf("reference-%d.png", index+1)
			if t.Stage == "board_edit" {
				if index == 0 {
					name = "edit-base.png"
				} else {
					name = fmt.Sprintf("auxiliary-%d.png", index)
				}
			}
			if t.ReferencePolicyVersion != referencePlanVersion && t.ReferencePolicyVersion != fourViewPolicyVersion && t.ImageReferencePlan == nil {
				name = filepath.Base(refs[index].File)
			}
			f, e := w.CreateFormFile("image[]", name)
			if e != nil {
				return nil, e
			}
			if _, e = f.Write(b); e != nil {
				return nil, e
			}
		}
		if e := w.Close(); e != nil {
			return nil, e
		}
		body = buf.Bytes()
		ctype = w.FormDataContentType()
	}
	b, e := s.paid(ctx, t, key, ep, path, ctype, body)
	if e != nil {
		return nil, e
	}
	return decodeImageReceipt(ctx, b)
}
func decodeImageReceipt(ctx context.Context, b []byte) ([]byte, error) {
	var v struct {
		Data []struct {
			B64 string `json:"b64_json"`
			URL string `json:"url"`
		} `json:"data"`
	}
	if e := json.Unmarshal(b, &v); e != nil || len(v.Data) != 1 {
		return nil, errors.New("生图响应必须包含唯一图片")
	}
	if v.Data[0].B64 != "" {
		return base64.StdEncoding.DecodeString(v.Data[0].B64)
	}
	if v.Data[0].URL != "" {
		return download(ctx, v.Data[0].URL, 24<<20)
	}
	return nil, errors.New("生图响应没有图片内容")
}
func publicURL(raw string) error {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.User != nil || u.Hostname() == "" {
		return errors.New("素材下载地址必须为公网HTTPS")
	}
	return nil
}

// Resolve every connection and validate every redirect before dialing; no internal network downloads.
func download(ctx context.Context, raw string, max int64) ([]byte, error) {
	if e := publicURL(raw); e != nil {
		return nil, e
	}
	dialer := &net.Dialer{Timeout: 20 * time.Second}
	transport := &http.Transport{DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, e := net.SplitHostPort(addr)
		if e != nil {
			return nil, e
		}
		ips, e := net.DefaultResolver.LookupIPAddr(ctx, host)
		if e != nil {
			return nil, e
		}
		for _, ip := range ips {
			if !ip.IP.IsGlobalUnicast() || ip.IP.IsPrivate() || ip.IP.IsLoopback() || ip.IP.IsLinkLocalUnicast() {
				return nil, errors.New("拒绝访问内部网络素材")
			}
		}
		if len(ips) == 0 {
			return nil, errors.New("素材地址无法解析")
		}
		return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 4 {
			return errors.New("素材重定向过多")
		}
		return publicURL(req.URL.String())
	}}
	req, e := http.NewRequestWithContext(ctx, "GET", raw, nil)
	if e != nil {
		return nil, e
	}
	resp, e := client.Do(req)
	if e != nil {
		return nil, e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("素材下载失败 HTTP %d", resp.StatusCode)
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if e != nil {
		return nil, e
	}
	if int64(len(b)) > max {
		return nil, errors.New("远程素材过大")
	}
	return b, nil
}
func (s *Studio) remoteVideo(ctx context.Context, t *Task, q Shot) (string, error) {
	ep, e := resolveEndpoint(t.Config, "video")
	if e != nil {
		return "", e
	}
	key := "video-" + q.ID
	attempt := t.ID + ":" + key
	var payload []byte
	var state string
	e = s.store.db.QueryRow("SELECT status FROM attempts WHERE id=?", attempt).Scan(&state)
	if errors.Is(e, sql.ErrNoRows) {
		body, buildErr := s.plannedVideoRequest(ctx, *t, q, ep.Model)
		if buildErr != nil {
			return "", buildErr
		}
		payload = jsonBytes(body)
	} else if e != nil {
		return "", e
	}
	// Existing receipts recover without depending on upstream reference availability.
	b, e := s.paid(ctx, t, key, ep, "/contents/generations/tasks", "application/json", payload)
	if e != nil {
		return "", e
	}
	var receipt struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(b, &receipt) != nil || receipt.ID == "" {
		return "", errors.New("供应商未返回任务ID，需控制台核对，禁止重提")
	}
	if _, e = s.store.db.Exec("UPDATE attempts SET remote_id=? WHERE id=?", receipt.ID, attempt); e != nil {
		return "", e
	}
	client := &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for {
		if e = ctx.Err(); e != nil {
			return "", e
		}
		req, e := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(ep.Base, "/")+"/contents/generations/tasks/"+url.PathEscape(receipt.ID), nil)
		if e != nil {
			return "", e
		}
		secret, credentialErr := s.credential(ep)
		if credentialErr != nil {
			return "", credentialErr
		}
		req.Header.Set("Authorization", "Bearer "+secret)
		resp, e := client.Do(req)
		if e != nil {
			return "", errors.New("查询失败；恢复任务将继续查询原供应商任务")
		}
		b, e = io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		resp.Body.Close()
		if e != nil || resp.StatusCode != 200 {
			return "", errors.New("查询任务失败，已保留供应商任务ID")
		}
		var status videoTaskResult
		if e = json.Unmarshal(b, &status); e != nil {
			return "", e
		}
		switch status.Status {
		case "succeeded":
			// Save usage before downloading: recovery must not lose or double-count it.
			if e = s.recordVideoUsage(t, q, ep, receipt.ID, status); e != nil {
				return "", e
			}
			clip, e := download(ctx, status.Content.URL, 100<<20)
			if e != nil {
				return "", e
			}
			file, e := s.writeMedia(t.ProjectID, t.ID+"-clip-"+q.ID+".mp4", clip)
			if e != nil {
				return "", e
			}
			if input, ok := inputFor(*t, q.ID); ok && (input.Mode == "continue" || input.Mode == "edit_video") {
				if e = s.checkClipDuration(ctx, file, q.Duration); e != nil {
					return "", e
				}
			}
			if dialogueMode(t.Input) {
				if e = s.requireAudio(ctx, file); e != nil {
					return "", e
				}
			}
			return file, nil
		case "failed", "expired", "cancelled":
			return "", fmt.Errorf("原供应商任务状态：%s；不会自动新建任务", status.Status)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

func videoRequest(p Project, q Shot, model string, img []byte) map[string]any {
	return map[string]any{"model": model, "content": []map[string]any{{"type": "text", "text": videoPrompt(p, q)}, {"type": "image_url", "image_url": map[string]string{"url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(img)}, "role": "reference_image"}}, "duration": q.Duration, "ratio": projectAspect(p), "resolution": videoResolution(p.VideoResolution), "generate_audio": dialogueMode(p)}
}

func taskVideoRequest(t Task, q Shot, model string, img []byte) map[string]any {
	if t.Stage == "shot_video" && t.Feedback != "" {
		q.Description += "。本次用户修改建议：" + t.Feedback + "。仅调整当前镜头，不更改已确认的角色、台词、画幅和时长。"
	}
	return videoRequest(t.Input, q, model, img)
}
