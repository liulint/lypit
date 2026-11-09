//go:build !darwin && !windows

package main

import (
	"os"
	"path/filepath"
)

func userDataDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "Lypit Studio"), nil
}

func directoryOpener(path string) (string, []string) { return "xdg-open", []string{path} }

func browserOpener(url string) (string, []string) { return "xdg-open", []string{url} }
