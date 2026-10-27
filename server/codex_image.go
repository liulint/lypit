package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

func nativeCodexImage(c *CLIConfig) bool {
	return c != nil && c.Kind == "codex" && c.ImageMode == "codex"
}

type codexImageIntent struct {
	Root    string    `json:"root"`
	Started time.Time `json:"started"`
}

var codexThreadRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)
var codexImageSizeRE = regexp.MustCompile(`^[1-9][0-9]{0,4}x[1-9][0-9]{0,4}$`)

func validateNativeImageModel(m Model) error {
	if m.ReferenceImages && (m.MaxReferences < 1 || m.MaxReferences > 9) {
		return errors.New("Codex 生图参考图数量须为1～9")
	}
	for _, size := range m.Sizes {
		var w, h int
		if !codexImageSizeRE.MatchString(size) {
			return errors.New("Codex 生图尺寸须为明确像素值，例如1024x1536")
		}
		fmt.Sscanf(size, "%dx%d", &w, &h)
		if int64(w)*int64(h) > 40_000_000 {
			return errors.New("Codex 生图像素数量超限")
		}
	}
	return nil
}

func codexImageThread(raw []byte) (string, error) {
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 4096), 20<<20)
	id := ""
	for scanner.Scan() {
		var event struct {
			Type     string `json:"type"`
			ThreadID string `json:"thread_id"`
		}
		if json.Unmarshal(scanner.Bytes(), &event) != nil || event.Type != "thread.started" {
			continue
		}
		if !codexThreadRE.MatchString(event.ThreadID) || id != "" && id != event.ThreadID {
			return "", errors.New("Codex 生图任务标识无效，不能收取图片")
		}
		id = event.ThreadID
	}
	if scanner.Err() != nil || id == "" {
		return "", errors.New("Codex 未返回生图任务标识，结果未知，不会自动重试")
	}
	return id, nil
}

func codexImagesRoot() (string, error) {
	home := strings.TrimSpace(os.Getenv("CODEX_HOME"))
	userHome, e := os.UserHomeDir()
	if e != nil && home == "" {
		return "", errors.New("无法定位 Codex 图片目录")
	}
	if home == "" {
		home = filepath.Join(userHome, ".codex")
	}
	if strings.HasPrefix(home, "~/") {
		home = filepath.Join(userHome, strings.TrimPrefix(home, "~/"))
	}
	root, e := filepath.Abs(filepath.Join(home, "generated_images"))
	if e != nil {
		return "", e
	}
	if e = os.MkdirAll(root, 0700); e != nil {
		return "", errors.New("Codex 图片目录不可写")
	}
	return filepath.EvalSymlinks(root)
}

// Only files belonging to this invocation's thread are eligible. Never scan
// neighboring threads or pick the newest image from the shared output root.
func collectCodexImage(dir string, intent codexImageIntent, thread string) ([]byte, error) {
	if !codexThreadRE.MatchString(thread) {
		return nil, errors.New("Codex 生图任务标识无效")
	}
	threadDir := filepath.Join(intent.Root, thread)
	info, e := os.Lstat(threadDir)
	if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("Codex 未保存本次任务的图片；只有预览或文本不算生图成功，不会自动重试")
	}
	actual, e := filepath.EvalSymlinks(threadDir)
	if e != nil || actual != threadDir {
		return nil, errors.New("Codex 图片目录不能通过符号链接跳转")
	}
	entries, e := os.ReadDir(threadDir)
	if e != nil {
		return nil, errors.New("Codex 本次任务图片目录不可读")
	}
	var primary, fallback []string
	for _, entry := range entries {
		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if !slices.Contains([]string{".png", ".jpg", ".jpeg", ".webp"}, ext) {
			continue
		}
		if strings.HasPrefix(strings.ToLower(name), "ig_") {
			primary = append(primary, name)
		} else if strings.HasPrefix(strings.ToLower(name), "call_") {
			fallback = append(fallback, name)
		}
	}
	if len(primary) == 0 {
		primary = fallback
	}
	if len(primary) != 1 {
		return nil, errors.New("Codex 未返回唯一一张本次任务图片，请核对输出；不会选择历史图片或自动重试")
	}
	source := filepath.Join(threadDir, primary[0])
	info, e = os.Lstat(source)
	if e != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > 40<<20 || info.ModTime().Before(intent.Started) {
		return nil, errors.New("Codex 图片文件无效、过大或属于旧任务")
	}
	f, e := os.Open(source)
	if e != nil {
		return nil, errors.New("Codex 图片文件不可读")
	}
	defer f.Close()
	opened, e := f.Stat()
	if e != nil || !os.SameFile(info, opened) {
		return nil, errors.New("Codex 图片文件在收取时已变化")
	}
	data, e := io.ReadAll(io.LimitReader(f, (40<<20)+1))
	if e != nil || len(data) > 40<<20 {
		return nil, errors.New("Codex 图片读取失败或过大")
	}
	filename := "codex-image" + strings.ToLower(filepath.Ext(source))
	if e = atomicWrite(filepath.Join(dir, filename), data); e != nil {
		return nil, e
	}
	result, _ := json.Marshal(map[string]string{"file": filename})
	return result, nil
}

func (s *Studio) executeCodexImage(ctx context.Context, p Provider, m Model, prompt string, refs []string, dir string, recovering bool) ([]byte, error) {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	manifestPath := filepath.Join(dir, "codex-image-intent.json")
	responsePath := filepath.Join(dir, "response.bin")
	resultPath := filepath.Join(dir, "codex-image-result.json")
	var intent codexImageIntent
	var raw []byte
	if recovering {
		if result, e := os.ReadFile(resultPath); e == nil {
			return result, nil
		}
		saved, e := os.ReadFile(manifestPath)
		if e != nil || json.Unmarshal(saved, &intent) != nil {
			return nil, errors.New("Codex 生图意图记录缺失，不能恢复或重提")
		}
		raw, e = os.ReadFile(responsePath)
		if e != nil {
			return nil, errors.New("Codex 生图结果未知，没有已保存响应，不能重提")
		}
	} else {
		// A durable intent prevents a second invocation, even if the caller loses
		// the response before the shared attempt state can be updated.
		if _, e := os.Stat(manifestPath); e == nil {
			return nil, errors.New("本次 Codex 生图已提交或结果未知，请恢复已有结果，不会重复调用")
		}
		bin, e := cliExecutable(p.CLI)
		if e != nil {
			return nil, e
		}
		root, e := codexImagesRoot()
		if e != nil {
			return nil, e
		}
		args, e := cliTaskArgs(*p.CLI, m.ID, dir, m.CLIOptions)
		if e != nil {
			return nil, e
		}
		args = append(args, "--add-dir", root)
		inputs := filepath.Join(dir, "inputs")
		if e = os.MkdirAll(inputs, 0700); e != nil {
			return nil, e
		}
		for i, ref := range refs {
			data, e := os.ReadFile(s.abs(ref))
			if e != nil {
				return nil, errors.New("生图参考图片不可读")
			}
			target := filepath.Join(inputs, fmt.Sprintf("ref-%d%s", i, filepath.Ext(ref)))
			if e = atomicWrite(target, data); e != nil {
				return nil, e
			}
			args = append(args, "-i", target)
		}
		intent = codexImageIntent{Root: root, Started: time.Now().UTC()}
		encoded, _ := json.Marshal(intent)
		intentFile, e := os.OpenFile(manifestPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return nil, errors.New("Codex 生图意图已存在或不可写，不会重复提交")
		}
		_, writeErr := intentFile.Write(encoded)
		syncErr := intentFile.Sync()
		closeErr := intentFile.Close()
		if writeErr != nil || syncErr != nil || closeErr != nil {
			return nil, errors.New("Codex 生图意图保存失败，不会提交生成")
		}
		text := "$imagegen\n只生成一张真实图片，必须使用图片生成工具，不要用代码绘图、占位图或仅返回提示词。将图片保存为 generated_images 下本次线程的 ig_* 或 call_* 图片文件。不要自行重试、追加候选或提交其他付费任务。\n"
		if len(refs) > 0 {
			text += "请编辑/引用附带图片，保持角色身份、服装和固定特征。\n"
		}
		text += "要求的输出像素尺寸（选择一种）：" + strings.Join(m.Sizes, ", ") + "。\n创作要求：\n" + prompt
		timeout := p.CLI.TimeoutSeconds
		if timeout == 0 {
			timeout = 600
		}
		runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
		defer cancel()
		raw, e = runProcess(runCtx, bin, dir, args, []byte(text))
		if saveErr := atomicWrite(responsePath, raw); saveErr != nil {
			return nil, saveErr
		}
		if e != nil {
			return nil, errors.New("Codex 生图未完成或已取消，响应已保存；请核对后恢复，不会自动重试")
		}
	}
	thread, e := codexImageThread(raw)
	if e != nil {
		return nil, e
	}
	result, e := collectCodexImage(dir, intent, thread)
	if e != nil {
		return nil, e
	}
	if _, _, e = cliImageOutput(dir, result, m); e != nil {
		return nil, e
	}
	if e = atomicWrite(resultPath, result); e != nil {
		return nil, e
	}
	return result, nil
}
