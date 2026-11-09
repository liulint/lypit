package main

import (
	"os"
	"path/filepath"
)

func userDataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "Application Support", "Lypit Studio"), nil
}

func directoryOpener(path string) (string, []string) { return "/usr/bin/open", []string{path} }

func browserOpener(url string) (string, []string) { return "/usr/bin/open", []string{url} }
