package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"
)

type ToolInstallState struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

type toolPlan struct {
	Manager, Name, HelpURL, Command string
	Args                            []string
}

func toolInstallPlan(goos string) toolPlan {
	if goos == "windows" {
		return toolPlan{Manager: "winget", Name: "WinGet", HelpURL: "https://learn.microsoft.com/windows/package-manager/winget/", Command: "winget install --id Gyan.FFmpeg --exact --source winget --scope user", Args: []string{"install", "--id", "Gyan.FFmpeg", "--exact", "--source", "winget", "--scope", "user", "--silent", "--accept-package-agreements", "--accept-source-agreements", "--disable-interactivity"}}
	}
	return toolPlan{Manager: "brew", Name: "Homebrew", HelpURL: "https://brew.sh/zh-cn/", Command: "brew install ffmpeg", Args: []string{"install", "ffmpeg"}}
}
func localTool(name string) (string, error) {
	if name != "ffmpeg" && name != "ffprobe" && name != "brew" && name != "winget" {
		return "", errors.New("不支持的本机工具")
	}
	return findLocalExecutable(name)
}

func mediaToolsReady(lookup func(string) (string, error)) bool {
	_, a := lookup("ffmpeg")
	_, b := lookup("ffprobe")
	return a == nil && b == nil
}

func (s *Studio) systemSnapshot() map[string]any {
	found := map[string]bool{}
	for _, name := range []string{"ffmpeg", "ffprobe"} {
		_, err := localTool(name)
		found[name] = err == nil
	}
	plan := toolInstallPlan(runtime.GOOS)
	_, managerErr := localTool(plan.Manager)
	s.toolMu.Lock()
	state := s.toolInstall
	s.toolMu.Unlock()
	if state.Status == "" {
		state.Status = "idle"
	}
	return map[string]any{"tools": found, "installer": map[string]any{"available": managerErr == nil, "manager": plan.Name, "help_url": plan.HelpURL, "command": plan.Command, "status": state.Status, "message": state.Message}, "database": "SQLite · WAL", "auth": "无登录 · 仅本机", "version": "0.1.0"}
}

func (s *Studio) installTools(c *gin.Context) {
	_, err := s.startToolInstall(localTool, installFFmpeg)
	if err != nil {
		result(c, nil, err)
		return
	}
	c.JSON(202, s.systemSnapshot())
}

// Exactly one installer per server, independent of browser disconnects and drafts.
func (s *Studio) startToolInstall(lookup func(string) (string, error), install func(context.Context, string) error) (ToolInstallState, error) {
	s.toolMu.Lock()
	defer s.toolMu.Unlock()
	if s.toolInstall.Status == "running" {
		return s.toolInstall, nil
	}
	if mediaToolsReady(lookup) {
		s.toolInstall = ToolInstallState{Status: "succeeded", Message: "FFmpeg 与 ffprobe 已就绪，无需重复安装"}
		return s.toolInstall, nil
	}
	plan := toolInstallPlan(runtime.GOOS)
	manager, err := lookup(plan.Manager)
	if err != nil {
		return ToolInstallState{}, fmt.Errorf("未找到 %s，请先安装后再点击安装缺少的工具", plan.Name)
	}
	if s.ctx.Err() != nil {
		return ToolInstallState{}, errors.New("服务正在关闭，请稍后重试")
	}
	s.toolInstall = ToolInstallState{Status: "running", Message: fmt.Sprintf("正在通过 %s 安装 FFmpeg 和 ffprobe，请稍候…", plan.Name)}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ctx, cancel := context.WithTimeout(s.ctx, 20*time.Minute)
		defer cancel()
		err := install(ctx, manager)
		state := ToolInstallState{Status: "succeeded", Message: "FFmpeg 与 ffprobe 安装完成，已重新检测，可以直接制作"}
		if err != nil || !mediaToolsReady(lookup) {
			state = ToolInstallState{Status: "failed", Message: fmt.Sprintf("安装未完成或工具仍不可用，请检查网络与 %s 权限；也可在终端运行 %s 后重新检测", plan.Name, plan.Command)}
		}
		s.toolMu.Lock()
		s.toolInstall = state
		s.toolMu.Unlock()
	}()
	return s.toolInstall, nil
}

func installFFmpeg(ctx context.Context, manager string) error {
	// Fixed package and arguments, no shell, no sudo, and no implicit installation on startup.
	plan := toolInstallPlan(runtime.GOOS)
	cmd := exec.CommandContext(ctx, manager, plan.Args...)
	cmd.Env = os.Environ()
	if plan.Manager == "brew" {
		cmd.Env = append(cmd.Env, "HOMEBREW_NO_AUTO_UPDATE=1", "HOMEBREW_NO_INSTALL_CLEANUP=1", "HOMEBREW_NO_ANALYTICS=1", "NONINTERACTIVE=1")
	}
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	configureProcess(cmd)
	cmd.WaitDelay = 3 * time.Second
	if err := cmd.Run(); err != nil {
		return err
	}
	for _, name := range []string{"ffmpeg", "ffprobe"} {
		path, err := localTool(name)
		if err != nil {
			return err
		}
		check, cancel := context.WithTimeout(ctx, 10*time.Second)
		err = exec.CommandContext(check, path, "-version").Run()
		cancel()
		if err != nil {
			return err
		}
	}
	return nil
}
