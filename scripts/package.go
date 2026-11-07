// Build distributable archives. Run: npm --prefix web run package
package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func main() {
	rootFlag := flag.String("root", ".", "repository root")
	outFlag := flag.String("out", "", "empty output directory (default .cache/releases/timestamp)")
	targetFlag := flag.String("targets", "darwin/arm64,darwin/amd64,windows/amd64", "release targets")
	flag.Parse()
	if err := buildPackages(*rootFlag, *outFlag, *targetFlag); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(dir, program string, args []string, env []string) error {
	cmd := exec.Command(program, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %w", program, err)
	}
	return nil
}
func buildPackages(rootArg, outArg, targets string) error {
	root, err := filepath.Abs(rootArg)
	if err != nil {
		return err
	}
	npm := os.Getenv("npm_execpath")
	if npm == "" {
		return fmt.Errorf("run through npm: npm --prefix web run package")
	}
	node := os.Getenv("npm_node_execpath")
	if node == "" {
		node, err = exec.LookPath("node")
		if err != nil {
			return err
		}
	}
	out := outArg
	if out == "" {
		out = filepath.Join(root, ".cache", "releases", time.Now().Format("20060102-150405.000"))
	}
	out, err = filepath.Abs(out)
	if err != nil {
		return err
	}
	if entries, err := os.ReadDir(out); err == nil && len(entries) > 0 {
		return fmt.Errorf("output directory must be empty; existing archives are preserved: %s", out)
	}
	if err = os.MkdirAll(out, 0755); err != nil {
		return err
	}
	if err = run(filepath.Join(root, "web"), node, []string{npm, "run", "build"}, nil); err != nil {
		return err
	}
	licenses, err := dependencyLicenses(root)
	if err != nil {
		return err
	}
	for _, target := range strings.Split(targets, ",") {
		parts := strings.Split(target, "/")
		if len(parts) != 2 {
			return fmt.Errorf("invalid target %s", target)
		}
		goos, arch := parts[0], parts[1]
		if !(goos == "darwin" && (arch == "arm64" || arch == "amd64") || goos == "windows" && arch == "amd64") {
			return fmt.Errorf("unsupported target %s", target)
		}
		osName := goos
		if goos == "darwin" {
			osName = "macos"
		}
		name := "lypit-" + osName + "-" + arch
		dir := filepath.Join(out, name)
		if err = os.MkdirAll(dir, 0755); err != nil {
			return err
		}
		if err = copyTree(filepath.Join(root, "web", "dist"), filepath.Join(dir, "web", "dist")); err != nil {
			return err
		}
		binary := "lypit-studio"
		launcher := "start.command"
		if goos == "windows" {
			binary += ".exe"
			launcher = "start.cmd"
		}
		if err = run(filepath.Join(root, "server"), "go", []string{"build", "-trimpath", "-ldflags=-s -w", "-o", filepath.Join(dir, binary), "."}, []string{"CGO_ENABLED=0", "GOWORK=off", "GOOS=" + goos, "GOARCH=" + arch}); err != nil {
			return err
		}
		launch := `#!/bin/sh
set -eu
APP_ROOT=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$APP_ROOT"
exec "$APP_ROOT/lypit-studio" -root "$APP_ROOT" "$@"
`
		if goos == "windows" {
			launch = "@echo off\r\nsetlocal DisableDelayedExpansion\r\ncd /d \"%~dp0\"\r\n\"%~dp0lypit-studio.exe\" -root \"%~dp0.\" %*\r\nexit /b %errorlevel%\r\n"
		}
		if err = os.WriteFile(filepath.Join(dir, launcher), []byte(launch), 0755); err != nil {
			return err
		}
		readme := "映序：AI 短片工作台\n\n运行 " + launcher + "，保持终端开启，在浏览器访问 http://127.0.0.1:8080/ 。\n应用无需 Go、Node.js、C 编译器或登录；整个文件夹需放在可写位置。\n前端位于 web/dist，后台程序位于根目录，运行数据自动保存在 data。\nFFmpeg 缺失时在模型服务页安装：macOS 使用 Homebrew，Windows 使用 WinGet。\n本地CLI需自行安装并登录；npm版CLI还需其自身要求的Node.js环境。\n密钥只在本机加密保存；不要把含 data 的个人运行目录发给别人。\n本包未签名/公证，首次启动可能需要在系统安全提示中允许该程序。\n"
		if err = os.WriteFile(filepath.Join(dir, "README.txt"), []byte(readme), 0644); err != nil {
			return err
		}
		if err = os.WriteFile(filepath.Join(dir, "THIRD_PARTY_NOTICES.txt"), licenses, 0644); err != nil {
			return err
		}
		archive := filepath.Join(out, name+".zip")
		if err = archiveTree(dir, archive); err != nil {
			return err
		}
		fmt.Println("Created:", archive)
	}
	return nil
}

func dependencyLicenses(root string) ([]byte, error) {
	cmd := exec.Command("go", "list", "-deps", "-json", ".")
	cmd.Dir = filepath.Join(root, "server")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOWORK=off")
	b, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	modules := map[string]struct{ Version, Dir string }{}
	decoder := json.NewDecoder(bytes.NewReader(b))
	for {
		var item struct {
			Module *struct{ Path, Version, Dir string }
		}
		if err := decoder.Decode(&item); err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}
		if item.Module != nil && item.Module.Path != "lypit/studio" {
			modules[item.Module.Path] = struct{ Version, Dir string }{item.Module.Version, item.Module.Dir}
		}
	}
	var result bytes.Buffer
	result.WriteString("Third-party licenses for this distribution.\n\n")
	paths := []string{}
	for path := range modules {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		module := modules[path]
		fmt.Fprintf(&result, "\n=== %s %s ===\n", path, module.Version)
		entries, err := os.ReadDir(module.Dir)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			name := strings.ToUpper(entry.Name())
			if entry.IsDir() || !(strings.Contains(name, "LICENSE") || strings.HasPrefix(name, "COPYING") || strings.HasPrefix(name, "NOTICE")) {
				continue
			}
			b, err := os.ReadFile(filepath.Join(module.Dir, entry.Name()))
			if err != nil {
				return nil, err
			}
			result.Write(b)
			result.WriteByte('\n')
		}
	}
	cmd = exec.Command("go", "env", "GOROOT")
	b, err = cmd.Output()
	if err != nil {
		return nil, err
	}
	goRoot := strings.TrimSpace(string(b))
	b, err = os.ReadFile(filepath.Join(goRoot, "LICENSE"))
	if os.IsNotExist(err) {
		b, err = os.ReadFile(filepath.Join(filepath.Dir(goRoot), "LICENSE"))
	}
	if err != nil {
		return nil, err
	}
	result.WriteString("\n=== Go runtime ===\n")
	result.Write(b)
	lockBytes, err := os.ReadFile(filepath.Join(root, "web", "package-lock.json"))
	if err != nil {
		return nil, err
	}
	var lock struct {
		Packages map[string]struct {
			Version string
			Dev     bool
		}
	}
	if err = json.Unmarshal(lockBytes, &lock); err != nil {
		return nil, err
	}
	packages := []string{}
	for path, item := range lock.Packages {
		if strings.HasPrefix(path, "node_modules/") && !item.Dev {
			packages = append(packages, path)
		}
	}
	sort.Strings(packages)
	for _, path := range packages {
		dir := filepath.Join(root, "web", filepath.FromSlash(path))
		entries, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&result, "\n=== frontend %s %s ===\n", strings.TrimPrefix(path, "node_modules/"), lock.Packages[path].Version)
		for _, entry := range entries {
			name := strings.ToUpper(entry.Name())
			if entry.IsDir() || !(strings.Contains(name, "LICENSE") || strings.HasPrefix(name, "COPYING") || strings.HasPrefix(name, "NOTICE")) {
				continue
			}
			b, err := os.ReadFile(filepath.Join(dir, entry.Name()))
			if err != nil {
				return nil, err
			}
			result.Write(b)
			result.WriteByte('\n')
		}
	}
	return result.Bytes(), nil
}
func copyTree(source, dest string) error {
	return filepath.WalkDir(source, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("non-regular frontend asset: %s", path)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err != nil {
			return err
		}
		_, err = io.Copy(out, in)
		closeErr := out.Close()
		if err != nil {
			return err
		}
		return closeErr
	})
}
func archiveTree(source, dest string) error {
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	z := zip.NewWriter(f)
	err = filepath.WalkDir(source, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		h, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(filepath.Dir(source), path)
		if err != nil {
			return err
		}
		h.Name = filepath.ToSlash(rel)
		h.Method = zip.Deflate
		// Preserve executable modes even when packaging macOS releases from Windows.
		if info.Name() == "lypit-studio" || info.Name() == "start.command" {
			h.SetMode(0755)
		}
		writer, err := z.CreateHeader(h)
		if err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		_, err = io.Copy(writer, in)
		return err
	})
	zipErr := z.Close()
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if zipErr != nil {
		return zipErr
	}
	return closeErr
}
