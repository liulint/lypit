package main

import (
	"errors"
	"path/filepath"
	"strings"
)

const cliImageReferenceLimit = 5

// Only allowlisted diagnostics enter task messages; never expose raw CLI output.
func cliImageFailureReason(dir string, fallback error) error {
	b, err := readCLIFile(filepath.Join(dir, "cli-execution.json"))
	if err != nil {
		return fallback
	}
	log, err := decodeBody[struct {
		Stdout string `json:"stdout"`
		Stderr string `json:"stderr"`
	}](b)
	if err != nil {
		return fallback
	}
	if strings.Contains(log.Stderr, "`referenced_image_paths` must contain at most 5 paths") || strings.Contains(log.Stdout, "`referenced_image_paths` must contain at most 5 paths") {
		return errors.New("生图工具参数校验失败：一次最多5张参考图，原执行传入数量超过上限，因此没有生成图片。当前已支持自动参考板兼容，请仅为当前缺图重新报价并确认后生成；恢复不会重跑模型，原CLI用量可能已计费")
	}
	return fallback
}
