package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// resetDramaData archives, never deletes. Both stores are closed before renaming,
// including on Windows; ciphertext and immutable credential IDs are preserved.
func resetDramaData(data string) (string, error) {
	if _, e := os.Stat(filepath.Join(data, "studio.db")); os.IsNotExist(e) {
		return "", nil
	}
	old, e := openStore(data)
	if e != nil {
		return "", e
	}
	defer func() {
		if old != nil {
			old.close()
		}
	}()
	var unknown int
	if e = old.db.QueryRow("SELECT count(*) FROM attempts a JOIN tasks t ON t.id=a.task_id WHERE a.status IN ('submitting','unknown') AND t.status NOT IN ('superseded','abandoned')").Scan(&unknown); e != nil {
		return "", e
	}
	if unknown > 0 {
		return "", errors.New("旧数据含结果未知的外部提交，禁止切换；请先核对和恢复原任务")
	}
	cfg, e := old.settings()
	if e != nil {
		return "", e
	}
	cfg.PromptTemplates = nil
	stamp := time.Now().Format("20060102-150405.000")
	stage := data + "-drama-new-" + stamp
	archive := data + "-archive-" + stamp
	fresh, e := openStore(stage)
	if e != nil {
		return "", e
	}
	closed := false
	defer func() {
		if !closed {
			fresh.close()
		}
	}()
	if _, e = fresh.db.Exec("UPDATE settings SET body=? WHERE id=1", jsonBytes(cfg)); e != nil {
		return "", e
	}
	sourceDir := filepath.Join(data, "credentials")
	entries, e := os.ReadDir(sourceDir)
	if e != nil && !os.IsNotExist(e) {
		return "", e
	}
	if len(entries) > 0 {
		targetDir := filepath.Join(stage, "credentials")
		if e = os.MkdirAll(targetDir, 0700); e != nil {
			return "", e
		}
		if e = protectCredentialDir(targetDir); e != nil {
			return "", e
		}
		for _, entry := range entries {
			if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
				return "", errors.New("密钥目录含非普通文件")
			}
			if entry.Name() != "master.key" && !strings.HasSuffix(entry.Name(), ".enc") {
				return "", errors.New("密钥目录含未知文件，停止迁移")
			}
			b, err := privateCredentialFile(filepath.Join(sourceDir, entry.Name()), 16<<10)
			if err != nil {
				return "", err
			}
			if err = writeCredentialFile(filepath.Join(targetDir, entry.Name()), b); err != nil {
				return "", err
			}
		}
	}
	probe := &Studio{data: stage}
	for _, v := range cfg.Vendors {
		if v.KeyMode == "local" && v.CredentialID != "" {
			if _, e = probe.credential(Endpoint{VendorID: v.ID, KeyMode: v.KeyMode, CredentialID: v.CredentialID}); e != nil {
				return "", e
			}
		}
	}
	if _, e = old.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); e != nil {
		return "", e
	}
	if e = old.close(); e != nil {
		return "", e
	}
	old = nil
	if e = fresh.close(); e != nil {
		return "", e
	}
	closed = true
	if e = os.Rename(data, archive); e != nil {
		return "", e
	}
	if e = os.Rename(stage, data); e != nil {
		restore := os.Rename(archive, data)
		return "", fmt.Errorf("新数据区切换失败：%v；恢复旧目录：%v", e, restore)
	}
	report, _ := json.MarshalIndent(map[string]any{"workflow": "drama-skills", "version": dramaVersion, "created": now(), "archive": filepath.Base(archive), "migrated": "vendors, model bindings, concurrency, immutable encrypted credentials", "content": "empty"}, "", "  ")
	if e = os.WriteFile(filepath.Join(data, "workflow-reset.json"), report, 0600); e != nil {
		return archive, e
	}
	return archive, nil
}
