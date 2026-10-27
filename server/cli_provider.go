package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type CLIStatus struct {
	Kind      string      `json:"kind"`
	Installed bool        `json:"installed"`
	Path      string      `json:"path"`
	Version   string      `json:"version"`
	Auth      string      `json:"auth"`
	Message   string      `json:"message"`
	Models    []string    `json:"models"`
	Catalog   *CLICatalog `json:"catalog,omitempty"`
}

func cliExecutable(c *CLIConfig) (string, error) {
	if c == nil || !slices.Contains([]string{"codex", "claude", "gemini"}, c.Kind) {
		return "", errors.New("请选择支持的 CLI")
	}
	path := c.Path
	if path == "" {
		path = c.Kind
	} else if !filepath.IsAbs(path) {
		return "", errors.New("CLI 自定义路径必须为绝对路径")
	}
	found, err := exec.LookPath(path)
	if err != nil {
		return "", errors.New("CLI 未安装或指定路径不可执行")
	}
	return filepath.Abs(found)
}

func readCLISkill(path string) ([]byte, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", errors.New("Skill 文件不可读")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 256<<10 {
		return nil, "", errors.New("Skill 须为不超过256KB的普通文件")
	}
	data, err := io.ReadAll(io.LimitReader(f, (256<<10)+1))
	if err != nil || len(data) > 256<<10 {
		return nil, "", errors.New("Skill 文件不可读或过大")
	}
	digest := sha256.Sum256(data)
	return data, hex.EncodeToString(digest[:]), nil
}
func validateCLISkill(c *CLIConfig) error {
	if c == nil {
		return errors.New("CLI 配置缺失")
	}
	if nativeCodexImage(c) {
		return nil
	}
	if c.SkillPath == "" {
		return nil
	}
	_, actual, err := readCLISkill(c.SkillPath)
	if err != nil || actual != c.SkillDigest {
		return errors.New("Skill 文件已变化或不可读，请重新保存配置并验证")
	}
	return nil
}

type boundedBuffer struct {
	bytes.Buffer
	max int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.max {
		return 0, errors.New("CLI 输出超限")
	}
	return b.Buffer.Write(p)
}
func runProcess(ctx context.Context, bin, dir string, args []string, input []byte) ([]byte, error) {
	return runProcessLimit(ctx, bin, dir, args, input, 20<<20)
}
func runProcessLimit(ctx context.Context, bin, dir string, args []string, input []byte, limit int) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	cmd.Stdin = bytes.NewReader(input)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
	cmd.WaitDelay = 2 * time.Second
	output := &boundedBuffer{max: limit}
	cmd.Stdout = output
	cmd.Stderr = io.Discard
	err := cmd.Run()
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	if err != nil {
		return output.Bytes(), errors.New("CLI 未完成或已取消；请核对工具记录，不自动重复提交")
	}
	return output.Bytes(), nil
}
func detectCLI(ctx context.Context, c *CLIConfig) CLIStatus {
	status := CLIStatus{Kind: c.Kind, Auth: "unknown", Models: []string{}}
	bin, err := cliExecutable(c)
	if err != nil {
		status.Message = err.Error()
		return status
	}
	status.Installed = true
	status.Path = bin
	probe, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	raw, err := runProcess(probe, bin, "", []string{"--version"}, nil)
	if err != nil {
		status.Message = "版本检测失败，请检查可执行文件"
		return status
	}
	status.Version = strings.TrimSpace(string(raw))
	if len(status.Version) > 180 {
		status.Version = status.Version[:180]
	}
	var args []string
	switch c.Kind {
	case "codex":
		args = []string{"login", "status"}
	case "claude":
		args = []string{"auth", "status"}
	default:
		status.Message = "CLI 已安装；登录状态需在本机终端核实"
		return status
	}
	authCtx, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	// Some CLIs report status on stderr; never return raw status/account details to the client.
	authRaw, authErr := runProcess(authCtx, bin, "", args, nil)
	err = authErr
	var authInfo struct {
		LoggedIn *bool `json:"loggedIn"`
	}
	if json.Unmarshal(authRaw, &authInfo) == nil && authInfo.LoggedIn != nil && !*authInfo.LoggedIn {
		err = errors.New("not authenticated")
	}
	if err == nil {
		status.Auth = "ready"
		status.Message = "本机登录状态检测通过，不代表生图能力已验证"
	} else {
		status.Auth = "unavailable"
		status.Message = "未登录或当前版本无法检测，请在本机终端登录后重试"
	}
	return status
}
func cliImageFingerprint(p Provider, m Model) string { return hash([]any{p.CLI, m}) }
func cliImageReady(p Provider, m Model) bool {
	return p.ImageVerified[m.ID] != "" && p.ImageVerified[m.ID] == cliImageFingerprint(p, m)
}
func cliTaskArgs(c CLIConfig, model, dir string, options ...*CLIOptions) ([]string, error) {
	var args []string
	switch c.Kind {
	case "codex":
		args = []string{"exec", "--json", "--skip-git-repo-check", "--sandbox", "workspace-write", "-c", "sandbox_workspace_write.network_access=true", "--output-last-message", filepath.Join(dir, "answer.json")}
	case "claude":
		args = []string{"-p", "--output-format", "json", "--permission-mode", "auto"}
	case "gemini":
		args = []string{"--output-format", "json"}
	default:
		return nil, errors.New("不支持的 CLI")
	}
	if model != "" && model != "default" {
		args = append(args, "--model", model)
	}
	if len(options) > 0 && options[0] != nil {
		o := options[0]
		if e := validateCLIOptions(c.Kind, o); e != nil {
			return nil, e
		}
		if o.Reasoning != "" && o.Reasoning != "default" {
			args = append(args, "-c", "model_reasoning_effort="+strconv.Quote(o.Reasoning))
		}
		if o.ServiceTier != "" && o.ServiceTier != "default" {
			args = append(args, "-c", "service_tier="+strconv.Quote(o.ServiceTier))
		}
	}
	return args, nil
}

func unwrapCLI(kind string, raw []byte) ([]byte, error) {
	if kind == "codex" {
		if !json.Valid(raw) {
			return nil, errors.New("CLI 未输出有效 JSON")
		}
		return raw, nil
	}
	var response struct {
		Result     string          `json:"result"`
		Response   string          `json:"response"`
		Structured json.RawMessage `json:"structured_output"`
		IsError    bool            `json:"is_error"`
		Error      json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(raw, &response); err != nil || response.IsError || len(response.Error) > 0 && string(response.Error) != "null" {
		return nil, errors.New("CLI 响应无效，原始响应已保存")
	}
	if len(response.Structured) > 0 && json.Valid(response.Structured) {
		return response.Structured, nil
	}
	text := response.Result
	if kind == "gemini" {
		text = response.Response
	}
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "```json") {
		text = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(text, "```json"), "```"))
	}
	if !json.Valid([]byte(text)) {
		return nil, errors.New("CLI 未输出有效 JSON，原始响应已保存")
	}
	return []byte(text), nil
}
func (s *Studio) executeCLI(ctx context.Context, p Provider, m Model, kind, prompt string, refs []string, dir string, recovering bool) ([]byte, error) {
	if kind == "image" && nativeCodexImage(p.CLI) {
		return s.executeCodexImage(ctx, p, m, prompt, refs, dir, recovering)
	}
	responsePath := filepath.Join(dir, "response.bin")
	var raw []byte
	var err error
	if recovering {
		raw, err = os.ReadFile(responsePath)
		if err != nil {
			return nil, errors.New("CLI 结果未知，没有可恢复响应；请核对工具记录")
		}
	} else {
		if e := validateCLISkill(p.CLI); e != nil {
			return nil, e
		}
		bin, e := cliExecutable(p.CLI)
		if e != nil {
			return nil, e
		}
		inputs := filepath.Join(dir, "inputs")
		if err = os.MkdirAll(inputs, 0700); err != nil {
			return nil, err
		}
		paths := []string{}
		for i, ref := range refs {
			data, e := os.ReadFile(s.abs(ref))
			if e != nil {
				return nil, e
			}
			dst := filepath.Join(inputs, fmt.Sprintf("ref-%d%s", i, filepath.Ext(ref)))
			if e = atomicWrite(dst, data); e != nil {
				return nil, e
			}
			paths = append(paths, dst)
		}
		instructions := "只输出严格 JSON 对象，不要 Markdown。"
		if kind == "image" {
			instructions = "使用已配置生图工具生成且只生成一张真实图片，保存到当前任务目录。不要用代码绘图、占位图、SVG或只返回提示词。最终只输出 JSON：{\"file\":\"相对图片路径\"}。不得自行重试或新增付费任务。\n" + p.CLI.ImageInstructions
		}
		skill := ""
		if p.CLI.SkillPath != "" && !nativeCodexImage(p.CLI) {
			b, digest, e := readCLISkill(p.CLI.SkillPath)
			if e != nil || digest != p.CLI.SkillDigest {
				return nil, errors.New("Skill 文件已变化或不可读，请重新保存配置")
			}
			skill = string(b)
		}
		task, _ := json.Marshal(map[string]any{"task": kind, "prompt": prompt, "references": paths, "output_directory": dir, "sizes": m.Sizes, "skill": skill})
		args, e := cliTaskArgs(*p.CLI, m.ID, dir, m.CLIOptions)
		if e != nil {
			return nil, e
		}
		timeout := p.CLI.TimeoutSeconds
		if timeout == 0 {
			timeout = 600
		}
		taskCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
		defer cancel()
		raw, err = runProcess(taskCtx, bin, dir, args, []byte(instructions+"\n"+string(task)))
		if err != nil {
			return nil, err
		}
		if p.CLI.Kind == "codex" {
			raw, err = os.ReadFile(filepath.Join(dir, "answer.json"))
			if err != nil {
				return nil, errors.New("Codex 没有保存最终 JSON；结果未知，不重发")
			}
		}
		if len(raw) > 20<<20 {
			return nil, errors.New("CLI 响应过大")
		}
		if err = atomicWrite(responsePath, raw); err != nil {
			return nil, err
		}
	}
	return unwrapCLI(p.CLI.Kind, raw)
}
func cliImageOutput(dir string, raw []byte, m Model) ([]byte, string, error) {
	var result struct {
		File string `json:"file"`
	}
	if json.Unmarshal(raw, &result) != nil || result.File == "" {
		return nil, "", errors.New("CLI 生图未返回真实图片路径")
	}
	path := result.File
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	actual, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, "", errors.New("CLI 图片文件不存在")
	}
	base, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, "", err
	}
	rel, err := filepath.Rel(base, actual)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return nil, "", errors.New("CLI 图片必须位于当前任务目录内")
	}
	f, err := os.Open(actual)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 40<<20 {
		return nil, "", errors.New("CLI 图片文件类型或大小无效")
	}
	b, err := io.ReadAll(io.LimitReader(f, 40<<20))
	if err != nil {
		return nil, "", err
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil || !slices.Contains([]string{"png", "jpeg", "webp"}, format) || cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 40_000_000 {
		return nil, "", errors.New("CLI 图片不可解码或尺寸超限")
	}
	if len(m.OutputFormats) > 0 && !slices.Contains(m.OutputFormats, format) {
		return nil, "", errors.New("CLI 图片格式不符合配置")
	}
	validSize := len(m.Sizes) == 0
	for _, size := range m.Sizes {
		if size == fmt.Sprintf("%dx%d", cfg.Width, cfg.Height) {
			validSize = true
		}
	}
	if !validSize {
		return nil, "", errors.New("CLI 图片尺寸不符合模型配置")
	}
	if _, _, err = image.Decode(bytes.NewReader(b)); err != nil {
		return nil, "", errors.New("CLI 图片数据损坏")
	}
	if format == "jpeg" {
		format = "jpg"
	}
	return b, format, nil
}
func (s *Studio) cliAsset(ctx context.Context, pid, key string, p Provider, m Model, kind, prompt string, refs []string, dir string, exists bool) (string, error) {
	raw, err := s.executeCLI(ctx, p, m, kind, prompt, refs, dir, exists)
	if _, e := os.Stat(filepath.Join(dir, "response.bin")); e == nil {
		_ = s.store.update(pid, func(project *Project) {
			a := project.Attempts[key]
			a.State = "response_saved"
			project.Attempts[key] = a
		})
	}
	if err != nil {
		return "", err
	}
	output := raw
	ext := "json"
	if kind == "image" {
		output, ext, err = cliImageOutput(dir, raw, m)
		if err != nil {
			return "", err
		}
	} else {
		var obj map[string]json.RawMessage
		if json.Unmarshal(output, &obj) != nil || obj == nil {
			return "", errors.New("CLI 文本必须为 JSON 对象")
		}
	}
	rel := filepath.Join(pid, key, "result."+ext)
	if err = atomicWrite(s.abs(rel), output); err != nil {
		return "", err
	}
	return rel, s.finishAttempt(pid, key, rel, nil, nil, "CLI 内部用量与费用未知；以工具账户记录为准")
}
