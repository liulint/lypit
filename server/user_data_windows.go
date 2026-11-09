package main

import (
	"errors"
	"os"
	"path/filepath"
)

func userDataDir() (string, error) {
	base := os.Getenv("LOCALAPPDATA")
	if !filepath.IsAbs(base) {
		return "", errors.New("无法确定 LOCALAPPDATA 用户数据目录")
	}
	return filepath.Join(base, "Lypit Studio"), nil
}

func directoryOpener(path string) (string, []string) {
	return filepath.Join(os.Getenv("SystemRoot"), "explorer.exe"), []string{path}
}

func browserOpener(url string) (string, []string) {
	return filepath.Join(os.Getenv("SystemRoot"), "System32", "rundll32.exe"), []string{"url.dll,FileProtocolHandler", url}
}
