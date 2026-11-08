//go:build windows

package main

import (
	"os"
	"path/filepath"
)

func dramaFontPaths() []string {
	root := os.Getenv("WINDIR")
	if root == "" {
		root = `C:\Windows`
	}
	return []string{filepath.Join(root, "Fonts", "msyh.ttc"), filepath.Join(root, "Fonts", "simhei.ttf"), filepath.Join(root, "Fonts", "simsun.ttc")}
}
