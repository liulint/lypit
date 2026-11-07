//go:build !windows

package main

import (
	"os/exec"
	"strconv"
	"strings"
)

func testProcessRunning(pid int) bool {
	b, err := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	state := strings.TrimSpace(string(b))
	return err == nil && state != "" && !strings.HasPrefix(state, "Z")
}
