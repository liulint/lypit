//go:build !windows

package main

func dramaFontPaths() []string {
	return []string{"/System/Library/Fonts/Hiragino Sans GB.ttc", "/System/Library/Fonts/STHeiti Medium.ttc", "/System/Library/Fonts/PingFang.ttc", "/Library/Fonts/Arial Unicode.ttf", "/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc"}
}
