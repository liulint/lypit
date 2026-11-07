//go:build !windows

package main

import (
	"os"
	"os/exec"
	"syscall"
)

func lockStoreFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}
func unlockStoreFile(f *os.File) error { return syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }
func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
}
func protectCredentialDir(path string) error                     { return os.Chmod(path, 0700) }
func protectCredentialFile(path string) error                    { return os.Chmod(path, 0600) }
func credentialPermissionsOK(path string, info os.FileInfo) bool { return info.Mode().Perm()&0077 == 0 }
func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
