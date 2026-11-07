package main

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func executableDirs(goos string) []string {
	home, _ := os.UserHomeDir()
	if goos == "windows" {
		local, app := os.Getenv("LOCALAPPDATA"), os.Getenv("APPDATA")
		return absoluteDirs([]string{filepath.Join(app, "npm"), filepath.Join(home, ".local", "bin"), filepath.Join(local, "Microsoft", "WindowsApps"), filepath.Join(local, "Microsoft", "WinGet", "Links"), filepath.Join(os.Getenv("ProgramFiles"), "nodejs"), filepath.Join(local, "Programs", "nodejs"), filepath.Join(os.Getenv("ProgramFiles"), "ffmpeg", "bin")})
	}
	return absoluteDirs([]string{filepath.Join(home, ".local", "bin"), "/opt/homebrew/bin", "/usr/local/bin", "/home/linuxbrew/.linuxbrew/bin"})
}

func absoluteDirs(dirs []string) []string {
	out := []string{}
	for _, dir := range dirs {
		if filepath.IsAbs(dir) {
			out = append(out, dir)
		}
	}
	return out
}

func findLocalExecutable(name string) (string, error) {
	if path, err := exec.LookPath(name); err == nil {
		return filepath.Abs(path)
	}
	for _, dir := range executableDirs(runtime.GOOS) {
		if path, err := exec.LookPath(filepath.Join(dir, name)); err == nil {
			return filepath.Abs(path)
		}
	}
	if runtime.GOOS == "windows" && os.Getenv("LOCALAPPDATA") != "" && (name == "ffmpeg" || name == "ffprobe") {
		return findWinGetMedia(name, filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "WinGet", "Packages"))
	}
	return "", errors.New("未发现本机可执行文件")
}

// WinGet modifies the user's PATH, but a running server keeps its old environment.
// Restrict discovery to this known package's bin directories; do not scan user files.
func findWinGetMedia(name, root string) (string, error) {
	if name != "ffmpeg" && name != "ffprobe" {
		return "", errors.New("未知媒体工具")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(strings.ToLower(entry.Name()), "gyan.ffmpeg_") {
			continue
		}
		base := filepath.Join(root, entry.Name())
		found := ""
		count := 0
		_ = filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return fs.SkipDir
			}
			count++
			if count > 512 {
				return fs.SkipAll
			}
			rel, _ := filepath.Rel(base, path)
			if d.IsDir() && len(strings.Split(rel, string(filepath.Separator))) > 3 {
				return fs.SkipDir
			}
			if d.Type().IsRegular() && strings.EqualFold(d.Name(), name+".exe") && strings.EqualFold(filepath.Base(filepath.Dir(path)), "bin") {
				found = path
				return fs.SkipAll
			}
			return nil
		})
		if found != "" {
			return found, nil
		}
	}
	return "", errors.New("未找到 WinGet 安装的媒体工具")
}

func cliProgram(path, adapter, goos string, lookup func(string) (string, error)) (string, []string, error) {
	if goos != "windows" {
		return path, nil, nil
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".exe" {
		return path, nil, nil
	}
	// Execute the official npm JS entry point through Node, not through cmd.exe.
	// This also keeps model IDs, reference paths and prompts out of shell parsing.
	if ext != ".cmd" || !strings.EqualFold(filepath.Base(path), adapter+".cmd") {
		return "", nil, errors.New("Windows CLI 请填写原生.exe或官方npm的codex.cmd/claude.cmd路径")
	}
	entry := ""
	switch adapter {
	case "codex":
		entry = filepath.Join(filepath.Dir(path), "node_modules", "@openai", "codex", "bin", "codex.js")
	case "claude":
		entry = filepath.Join(filepath.Dir(path), "node_modules", "@anthropic-ai", "claude-code", "cli.js")
	default:
		return "", nil, errors.New("未知CLI适配器")
	}
	if info, err := os.Stat(entry); err != nil || !info.Mode().IsRegular() {
		return "", nil, errors.New("未找到官方npm CLI入口，请重新安装CLI或选择原生.exe")
	}
	node, err := exec.LookPath(filepath.Join(filepath.Dir(path), "node.exe"))
	if err != nil {
		node, err = lookup("node")
	}
	if err != nil {
		return "", nil, errors.New("npm CLI需要Node.js，请安装Node.js后重新检测")
	}
	return node, []string{entry}, nil
}

func newCLICommand(ctx context.Context, path, adapter string, args ...string) (*exec.Cmd, error) {
	program, prefix, err := cliProgram(path, adapter, runtime.GOOS, findLocalExecutable)
	if err != nil {
		return nil, err
	}
	return exec.CommandContext(ctx, program, append(prefix, args...)...), nil
}
