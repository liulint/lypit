package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	_ "golang.org/x/image/webp"
	"image"
	_ "image/jpeg"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

var nameRE = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,63}$`)
var envRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func selectModel(s Settings, kind string) (Provider, Model, error) {
	b, ok := s.Bindings[kind]
	if !ok {
		return Provider{}, Model{}, fmt.Errorf("请配置 %s 模型", kind)
	}
	for _, p := range s.Providers {
		if p.ID == b.Provider {
			for _, m := range p.Models {
				if m.ID == b.Model && slices.Contains(m.Capabilities, kind) {
					return p, m, nil
				}
			}
		}
	}
	return Provider{}, Model{}, fmt.Errorf("%s 绑定缺少对应能力", kind)
}
func validateSettings(s Settings) error {
	ids := map[string]bool{}
	for _, p := range s.Providers {
		if !nameRE.MatchString(p.ID) || ids[p.ID] {
			return errors.New("供应商 ID 无效或重复")
		}
		ids[p.ID] = true
		if !slices.Contains([]string{"mock", "openai", "seedance", "anthropic", "gemini", "cli"}, p.Adapter) {
			return errors.New("不支持的协议适配器")
		}
		if p.Adapter == "cli" {
			if p.CLI != nil && (!slices.Contains([]string{"", "tool", "codex"}, p.CLI.ImageMode) || p.CLI.ImageMode == "codex" && p.CLI.Kind != "codex") {
				return errors.New("Codex 生图方式仅支持 Codex CLI")
			}
			if p.CLI == nil || !slices.Contains([]string{"codex", "claude", "gemini"}, p.CLI.Kind) || p.CLI.TimeoutSeconds < 0 || p.CLI.TimeoutSeconds > 1800 {
				return errors.New("CLI 配置无效，超时须为0～1800秒")
			}
			if p.CLI.Path != "" && !filepath.IsAbs(p.CLI.Path) {
				return errors.New("CLI 路径须为绝对路径")
			}
			if p.CLI.SkillPath != "" && !filepath.IsAbs(p.CLI.SkillPath) {
				return errors.New("Skill 路径须为绝对路径")
			}
		} else if p.Adapter != "mock" {
			if p.CredentialSource != "" && p.CredentialSource != "env" && p.CredentialSource != "file" {
				return errors.New("凭据来源无效")
			}
			if p.CredentialSource != "file" && !envRE.MatchString(p.KeyEnv) {
				return errors.New("凭据必须填写环境变量名")
			}
			u, e := url.Parse(p.BaseURL)
			if e != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || u.RawQuery != "" || u.Fragment != "" {
				return errors.New("供应商地址须为不含凭据与查询参数的 HTTP(S) 地址")
			}
			if p.Adapter == "seedance" && p.BaseURL != "https://ark.cn-beijing.volces.com/api/v3" {
				return errors.New("Seedance 仅支持方舟官方地址")
			}
		}
		seen := map[string]bool{}
		for _, m := range p.Models {
			if m.CLIOptions != nil {
				if p.Adapter != "cli" || p.CLI == nil {
					return errors.New("CLI 参数仅用于本地 Codex")
				}
				if e := validateCLIOptions(p.CLI.Kind, m.CLIOptions); e != nil {
					return e
				}
			}
			if m.ID == "" || seen[m.ID] || len(m.ID) > 200 {
				return errors.New("模型 ID 为空、过长或重复")
			}
			seen[m.ID] = true
			if slices.Contains(m.Capabilities, "image") && len(m.Sizes) == 0 {
				return errors.New("生图模型需要声明支持的尺寸")
			}
			if nativeCodexImage(p.CLI) && slices.Contains(m.Capabilities, "image") {
				if e := validateNativeImageModel(m); e != nil {
					return e
				}
			}
			if slices.Contains(m.Capabilities, "video") && (m.MinDuration < 4 || m.MaxDuration > 15 || m.MinDuration > m.MaxDuration) {
				return errors.New("视频模型时长范围须在4～15秒内")
			}
			if p.Adapter == "seedance" && (!m.Async || !slices.Contains(m.Sizes, "480p")) {
				return errors.New("Seedance首版需要异步任务和480p能力")
			}

			if m.Price != nil && (*m.Price < 0 || m.Unit == "" || m.PriceSource == "" || m.Verified == "") {
				return errors.New("价格需要非负单价、计价单位、来源和核实日期")
			}
			for _, k := range m.Capabilities {
				if !slices.Contains([]string{"text", "vision", "image", "video", "tts"}, k) {
					return errors.New("未知模型能力")
				}
				if p.Adapter == "openai" && k == "video" || p.Adapter == "seedance" && k != "video" || (p.Adapter == "anthropic" || p.Adapter == "gemini") && k != "text" && k != "vision" || p.Adapter == "cli" && k != "text" && k != "image" {
					return errors.New("协议与模型能力不匹配")
				}
			}
			for _, format := range m.OutputFormats {
				if !slices.Contains([]string{"png", "jpeg", "webp"}, format) {
					return errors.New("图片输出格式仅支持png、jpeg、webp")
				}
			}
			if m.VideoReference != "" && m.VideoReference != "image_reference" {
				return errors.New("当前适配器只支持图片参考视频，不支持首尾帧")
			}
			if m.ReferenceImages && m.MaxReferences < 1 {
				return errors.New("参考图能力需设置最大张数")
			}
		}
	}
	for _, k := range []string{"text", "vision", "image", "video", "tts"} {
		if p, m, e := selectModel(s, k); e != nil {
			return e
		} else if k == "image" && p.Adapter == "cli" && !cliImageReady(p, m) {
			return errors.New("CLI 生图能力尚未验证或配置已变化")
		}
	}
	for _, role := range []string{"character_image", "storyboard_image"} {
		if _, ok := s.Bindings[role]; ok {
			if p, m, e := selectModel(imageSettings(s, role), "image"); e != nil {
				return fmt.Errorf("%s: %w", role, e)
			} else if p.Adapter == "cli" && !cliImageReady(p, m) {
				return errors.New("CLI 生图能力尚未验证或配置已变化")
			}
		}
	}
	return nil
}
func (studio *Studio) ready(s Settings, kinds ...string) error {
	for _, k := range kinds {
		p, m, e := selectModel(s, k)
		if e != nil {
			return e
		}
		if p.Adapter == "cli" {
			if _, e := cliExecutable(p.CLI); e != nil {
				return e
			}
			if e := validateCLISkill(p.CLI); e != nil {
				return e
			}
			if k == "image" && !cliImageReady(p, m) {
				return errors.New("CLI 生图能力尚未验证或配置已变化")
			}
		} else if p.Adapter != "mock" {
			if _, e := studio.providerKey(p); e != nil {
				return e
			}
		}
		if k == "text" && !m.Structured {
			return errors.New("剧本模型必须声明结构化输出能力")
		}
		if k == "tts" && p.Adapter != "mock" && m.Voice == "" {
			return errors.New("请设置旁白模型 voice")
		}
	}
	return nil
}
func checkReferences(m Model, refs []string) error {
	if len(refs) > 0 && (!m.ReferenceImages || len(refs) > m.MaxReferences) {
		return fmt.Errorf("模型 %s 不支持本次 %d 张参考图，请更换模型", m.ID, len(refs))
	}
	return nil
}
func httpClient() *http.Client {
	return &http.Client{Timeout: 180 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
func providerRequest(ctx context.Context, p Provider, path string, body io.Reader, contentType string) ([]byte, error) {
	req, e := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(p.BaseURL, "/")+path, body)
	if e != nil {
		return nil, e
	}
	setProviderAuth(req, p)
	req.Header.Set("Content-Type", contentType)
	res, e := httpClient().Do(req)
	if e != nil {
		return nil, errors.New("模型请求中断，提交结果未知；禁止自动重试")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("模型服务 HTTP %d；请核对服务端记录，不自动重试", res.StatusCode)
	}
	b, e := io.ReadAll(io.LimitReader(res.Body, 50<<20))
	if e != nil {
		return nil, e
	}
	if len(b) >= 50<<20 {
		return nil, errors.New("模型响应过大")
	}
	return b, nil
}
func postJSON(ctx context.Context, p Provider, path string, payload any) ([]byte, error) {
	b, e := json.Marshal(payload)
	if e != nil {
		return nil, e
	}
	return providerRequest(ctx, p, path, bytes.NewReader(b), "application/json")
}

// Only public media URLs, including after DNS resolution. API clients and media clients are separate.
func mediaClient() *http.Client {
	dial := &net.Dialer{Timeout: 15 * time.Second}
	return &http.Client{Timeout: 180 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Transport: &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, e := net.SplitHostPort(addr)
		if e != nil {
			return nil, e
		}
		ips, e := net.DefaultResolver.LookupIPAddr(ctx, host)
		if e != nil {
			return nil, e
		}
		for _, a := range ips {
			ip := a.IP
			if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
				return nil, errors.New("禁止读取内网媒体地址")
			}
		}
		if len(ips) == 0 {
			return nil, errors.New("媒体 DNS 无结果")
		}
		return dial.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
	}}}
}
func downloadMedia(ctx context.Context, raw string) ([]byte, error) {
	u, e := url.Parse(raw)
	if e != nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, errors.New("无效媒体 URL")
	}
	req, e := http.NewRequestWithContext(ctx, "GET", raw, nil)
	if e != nil {
		return nil, e
	}
	res, e := mediaClient().Do(req)
	if e != nil {
		return nil, e
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("媒体下载 HTTP %d", res.StatusCode)
	}
	b, e := io.ReadAll(io.LimitReader(res.Body, 40<<20))
	if len(b) >= 40<<20 {
		return nil, errors.New("媒体超过40MB")
	}
	return b, e
}
func dataURL(path string) (string, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return "", e
	}
	return "data:" + http.DetectContentType(b) + ";base64," + base64.StdEncoding.EncodeToString(b), nil
}
func (s *Studio) request(ctx context.Context, pid string, settings Settings, kind, prompt string, refs []string, duration int) (string, error) {
	return s.requestIntent(ctx, pid, settings, kind, prompt, refs, duration, "", kind)
}
func (s *Studio) requestIntent(ctx context.Context, pid string, settings Settings, kind, prompt string, refs []string, duration int, intent, role string) (string, error) {
	provider, model, e := selectModel(settings, kind)
	if e != nil {
		return "", e
	}
	if e = checkReferences(model, refs); e != nil {
		return "", e
	}
	if kind == "image" && len(refs) > 0 && model.ImageEdit != nil && !*model.ImageEdit {
		return "", errors.New("生图模型未声明图片编辑能力，不能使用参考图")
	}
	digests := []string{}
	for _, ref := range refs {
		value, err := fileHash(s.abs(ref))
		if err != nil {
			return "", err
		}
		digests = append(digests, value)
	}
	key := hash([]any{provider, model, kind, prompt, refs, digests, duration, intent})
	p, e := s.store.project(pid)
	if e != nil {
		return "", e
	}
	if intent == "" {
		legacyKey := hash([]any{provider, model, kind, prompt, refs, duration})
		if _, ok := p.Attempts[legacyKey]; ok {
			key = legacyKey
		}
	}
	old, exists := p.Attempts[key]
	if exists && old.State == "succeeded" {
		if _, e = os.Stat(s.abs(old.File)); e == nil {
			return old.File, nil
		}
		return "", errors.New("已完成素材丢失，请明确创建重做任务")
	}
	if exists && (old.State == "submission_unknown" || old.State == "interrupted") && kind != "video" {
		return "", errors.New("该请求结果未知；请核对供应商记录，不能自动重复扣费")
	}
	if !exists {
		if provider.Adapter == "cli" {
			if _, err := cliExecutable(provider.CLI); err != nil {
				return "", err
			}
			if e := validateCLISkill(provider.CLI); e != nil {
				return "", e
			}
			if kind == "image" && !cliImageReady(provider, model) {
				return "", errors.New("CLI 生图能力尚未验证或配置已变化")
			}
		} else if provider.Adapter != "mock" {
			provider.credential, e = s.providerKey(provider)
			if e != nil {
				return "", e
			}
		}
	}
	dir := filepath.Join(s.data, "media", pid, key)
	if e = os.MkdirAll(dir, 0700); e != nil {
		return "", e
	}
	responsePath := filepath.Join(dir, "response.bin")
	if !exists {
		e = s.store.change(func(st *State) error {
			p := st.Projects[pid]
			if _, exists := p.Attempts[key]; exists {
				return errors.New("相同请求正在执行或已记录，请等待后恢复")
			}
			if intent != "" && p.ActiveQuote != "" {
				sq, ok := st.Quotes[p.ActiveQuote]
				if !ok || !sq.Quote.Used || sq.Quote.Schema < 2 {
					return errors.New("当前生成范围未确认，请重新报价")
				}
				for _, ref := range actualInputHashes(refs, digests) {
					if expected, ok := sq.Quote.RefHashes[ref.File]; ok && expected != ref.Hash {
						return errors.New("任务引用图片与报价不一致")
					}
				}
				limit := sq.Quote.Scope[role]
				used := 0
				for _, a := range p.Attempts {
					if a.QuoteID == p.ActiveQuote && a.Role == role {
						used++
					}
				}
				if used >= limit {
					return fmt.Errorf("%s请求数量超过已确认范围，未提交", role)
				}
			} else if intent != "" && provider.Adapter != "mock" {
				return errors.New("真实生成需要已确认的任务范围")
			}
			actualRefs := []ReferenceUse{}
			for i, file := range refs {
				actualRefs = append(actualRefs, ReferenceUse{File: file, Hash: digests[i], Purpose: "实际模型输入"})
			}
			p.Attempts[key] = Attempt{Prompt: prompt, References: actualRefs, Duration: duration, ID: key, Kind: kind, State: "submission_unknown", Provider: provider.ID, Model: model.ID, Intent: intent, Role: role, QuoteID: p.ActiveQuote}
			return nil
		})
		if e != nil {
			return "", e
		}
	}
	if provider.Adapter == "mock" {
		return s.mockAsset(ctx, pid, key, kind, prompt, duration)
	}
	if provider.Adapter == "seedance" {
		return s.seedance(ctx, pid, key, provider, model, prompt, refs, duration, dir, exists)
	}
	if provider.Adapter == "cli" {
		if kind == "image" && !cliImageReady(provider, model) {
			return "", errors.New("CLI 生图能力尚未验证或配置已变化")
		}
		return s.cliAsset(ctx, pid, key, provider, model, kind, prompt, refs, dir, exists)
	}
	if provider.Adapter != "openai" && provider.Adapter != "anthropic" && provider.Adapter != "gemini" {
		return "", errors.New("供应商协议不支持本操作")
	}
	var raw []byte
	if exists && old.State == "response_saved" {
		raw, e = os.ReadFile(responsePath)
		if os.IsNotExist(e) && (provider.Adapter == "anthropic" || provider.Adapter == "gemini") {
			raw, e = s.nativeText(ctx, provider, model, prompt, refs, responsePath, true)
		}
	} else {
		if provider.credential == "" {
			provider.credential, e = s.providerKey(provider)
			if e != nil {
				return "", e
			}
		}
		switch kind {
		case "text", "vision":
			if provider.Adapter != "openai" {
				raw, e = s.nativeText(ctx, provider, model, prompt, refs, responsePath, false)
				break
			}
			var content any = prompt
			if len(refs) > 0 {
				items := []any{map[string]any{"type": "text", "text": prompt}}
				for _, ref := range refs {
					u, err := dataURL(s.abs(ref))
					if err != nil {
						return "", err
					}
					items = append(items, map[string]any{"type": "image_url", "image_url": map[string]string{"url": u}})
				}
				content = items
			}
			payload := map[string]any{"model": model.ID, "messages": []any{map[string]any{"role": "user", "content": content}}}
			if model.Structured {
				payload["response_format"] = map[string]string{"type": "json_object"}
			}
			raw, e = postJSON(ctx, provider, "/chat/completions", payload)
		case "image":
			size := "1024x1536"
			if len(model.Sizes) > 0 {
				size = model.Sizes[0]
			}
			if len(refs) == 0 {
				raw, e = postJSON(ctx, provider, "/images/generations", map[string]any{"model": model.ID, "prompt": prompt, "n": 1, "size": size, "response_format": "b64_json"})
			} else {
				var body bytes.Buffer
				w := multipart.NewWriter(&body)
				_ = w.WriteField("model", model.ID)
				_ = w.WriteField("prompt", prompt)
				_ = w.WriteField("n", "1")
				_ = w.WriteField("size", size)
				_ = w.WriteField("response_format", "b64_json")
				for _, ref := range refs {
					b, err := os.ReadFile(s.abs(ref))
					if err != nil {
						return "", err
					}
					part, err := w.CreateFormFile("image[]", filepath.Base(ref))
					if err != nil {
						return "", err
					}
					_, _ = part.Write(b)
				}
				_ = w.Close()
				raw, e = providerRequest(ctx, provider, "/images/edits", &body, w.FormDataContentType())
			}
		case "tts":
			raw, e = postJSON(ctx, provider, "/audio/speech", map[string]any{"model": model.ID, "voice": model.Voice, "input": prompt, "response_format": "mp3"})
		default:
			return "", errors.New("操作不支持")
		}
		if e != nil {
			if _, savedErr := os.Stat(responsePath + ".native"); savedErr == nil {
				_ = s.store.update(pid, func(p *Project) { a := p.Attempts[key]; a.State = "response_saved"; p.Attempts[key] = a })
			}
			return "", e
		}
		if e = atomicWrite(responsePath, raw); e != nil {
			return "", e
		}
		if e = s.store.update(pid, func(p *Project) { a := p.Attempts[key]; a.State = "response_saved"; p.Attempts[key] = a }); e != nil {
			return "", e
		}
	}
	if e != nil {
		return "", e
	}
	var output []byte
	ext := "json"
	var usage json.RawMessage
	switch kind {
	case "text", "vision":
		var r struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
			Usage json.RawMessage `json:"usage"`
		}
		if e = json.Unmarshal(raw, &r); e != nil || len(r.Choices) == 0 {
			return "", errors.New("文本响应无有效内容；已保留响应，不会重提")
		}
		output = []byte(strings.TrimSpace(r.Choices[0].Message.Content))
		usage = r.Usage
	case "image":
		var r struct {
			Data []struct {
				B64 string `json:"b64_json"`
				URL string `json:"url"`
			} `json:"data"`
			Usage json.RawMessage `json:"usage"`
		}
		if e = json.Unmarshal(raw, &r); e != nil || len(r.Data) == 0 {
			return "", errors.New("图片响应缺少 data")
		}
		usage = r.Usage
		if r.Data[0].B64 != "" {
			output, e = base64.StdEncoding.DecodeString(r.Data[0].B64)
		} else {
			output, e = downloadMedia(ctx, r.Data[0].URL)
		}
		if e == nil {
			config, format, err := image.DecodeConfig(bytes.NewReader(output))
			if err != nil || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 40_000_000 {
				return "", errors.New("图片结果不可解码或尺寸超限；响应已保留，不重发生成")
			}
			if len(model.OutputFormats) > 0 && !slices.Contains(model.OutputFormats, format) {
				return "", errors.New("返回图片格式不符合模型配置；请核对输出格式")
			}
			ext = format
			if format == "jpeg" {
				ext = "jpg"
			}
		}
	case "tts":
		output = raw
		ext = "mp3"
	}
	if e != nil {
		return "", e
	}
	rel := filepath.Join(pid, key, "result."+ext)
	if e = atomicWrite(s.abs(rel), output); e != nil {
		return "", e
	}
	e = s.finishAttempt(pid, key, rel, usage, nil, "")
	return rel, e
}
func atomicWrite(path string, b []byte) error {
	tmp := path + ".part"
	f, e := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	_, e = f.Write(b)
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	return os.Rename(tmp, path)
}
func (s *Studio) finishAttempt(pid, key, file string, usage json.RawMessage, fee *float64, detail string) error {
	return s.store.update(pid, func(p *Project) {
		a := p.Attempts[key]
		a.State = "succeeded"
		a.File = file
		a.Usage = usage
		a.Fee = fee
		a.Detail = detail
		p.Attempts[key] = a
	})
}

func actualInputHashes(files, digests []string) []ReferenceUse {
	out := []ReferenceUse{}
	for i, f := range files {
		out = append(out, ReferenceUse{File: f, Hash: digests[i]})
	}
	return out
}
