package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func lockStoreFile(f *os.File) error {
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{})
}
func unlockStoreFile(f *os.File) error {
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &windows.Overlapped{})
}
func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &windows.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP, HideWindow: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		// Use the Windows system utility with fixed arguments, never cmd.exe or user-supplied shell text.
		kill := exec.CommandContext(ctx, filepath.Join(os.Getenv("SystemRoot"), "System32", "taskkill.exe"), "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F")
		kill.SysProcAttr = &windows.SysProcAttr{HideWindow: true}
		if err := kill.Run(); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
}
func currentUserSID() (*windows.SID, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	return user.User.Sid, nil
}
func protectCredentialPath(path string, directory bool) error {
	sid, err := currentUserSID()
	if err != nil {
		return err
	}
	flags := ""
	if directory {
		flags = "OICI"
	}
	sd, err := windows.SecurityDescriptorFromString(fmt.Sprintf("D:P(A;%s;FA;;;%s)(A;%s;FA;;;SY)", flags, sid.String(), flags))
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
}
func protectCredentialDir(path string) error  { return protectCredentialPath(path, true) }
func protectCredentialFile(path string) error { return protectCredentialPath(path, false) }
func credentialPermissionsOK(path string, _ os.FileInfo) bool {
	sid, err := currentUserSID()
	if err != nil {
		return false
	}
	system, err := windows.StringToSid("S-1-5-18")
	if err != nil {
		return false
	}
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return false
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil || acl.AceCount == 0 {
		return false
	}
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if windows.GetAce(acl, i, &ace) != nil || ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return false
		}
		allowed := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if !allowed.Equals(sid) && !allowed.Equals(system) {
			return false
		}
	}
	return true
}

// Windows does not allow FlushFileBuffers on directory handles. Files are synced before publication.
func syncDirectory(string) error { return nil }
