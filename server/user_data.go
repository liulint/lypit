package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const dataSchemaVersion = 1

type dataBackupManifest struct {
	Schema  int               `json:"schema"`
	Created string            `json:"created"`
	Files   map[string]string `json:"sha256"`
}

// Caller must hold the data directory process lock and exclude writers/workers.
// The backup is outside the live directory, and only a completed snapshot is renamed.
// Failed partial backups are retained for inspection, never used as complete backups.
func backupUserData(db *sql.DB, dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	parent := filepath.Join(filepath.Dir(dir), filepath.Base(dir)+" Backups")
	if err = os.MkdirAll(parent, 0700); err != nil {
		return "", err
	}
	if err = protectCredentialDir(parent); err != nil {
		return "", err
	}
	dest, err := os.MkdirTemp(parent, ".incomplete-"+time.Now().Format("20060102-150405")+"-")
	if err != nil {
		return "", err
	}
	if err = protectCredentialDir(dest); err != nil {
		return "", err
	}
	failed := func(e error) (string, error) {
		return "", fmt.Errorf("备份未完成，原数据保持不变；不完整副本位于 %s：%w", dest, e)
	}
	snapshot := filepath.Join(dest, "studio.db")
	if _, err = db.Exec("VACUUM INTO ?", snapshot); err != nil {
		return failed(err)
	}
	if err = protectCredentialFile(snapshot); err != nil {
		return failed(err)
	}
	manifest := dataBackupManifest{Schema: dataSchemaVersion, Created: time.Now().UTC().Format(time.RFC3339), Files: map[string]string{}}
	if err = db.QueryRow("PRAGMA user_version").Scan(&manifest.Schema); err != nil {
		return failed(err)
	}
	directories := []string{dest}
	err = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, e := filepath.Rel(dir, path)
		if e != nil || rel == "." {
			return e
		}
		if rel == "backup-manifest.json" || rel == "studio.lock" || rel == "studio.db" || rel == "studio.db-wal" || rel == "studio.db-shm" {
			return nil
		}
		target := filepath.Join(dest, rel)
		if entry.IsDir() {
			if e = os.Mkdir(target, 0700); e != nil {
				return e
			}
			directories = append(directories, target)
			return protectCredentialDir(target)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("不能备份非普通文件：%s", rel)
		}
		in, e := os.Open(path)
		if e != nil {
			return e
		}
		defer in.Close()
		out, e := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return e
		}
		if e = protectCredentialFile(target); e != nil {
			out.Close()
			return e
		}
		hash := sha256.New()
		_, e = io.Copy(io.MultiWriter(out, hash), in)
		if e == nil {
			e = out.Sync()
		}
		closeErr := out.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
		manifest.Files[filepath.ToSlash(rel)] = hex.EncodeToString(hash.Sum(nil))
		return nil
	})
	if err != nil {
		return failed(err)
	}
	f, err := os.OpenFile(snapshot, os.O_RDWR, 0600)
	if err != nil {
		return failed(err)
	}
	hash := sha256.New()
	_, err = io.Copy(hash, f)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return failed(err)
	}
	if closeErr != nil {
		return failed(closeErr)
	}
	for i := len(directories) - 1; i >= 0; i-- {
		if err = syncDirectory(directories[i]); err != nil {
			return failed(err)
		}
	}
	manifest.Files["studio.db"] = hex.EncodeToString(hash.Sum(nil))
	content, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return failed(err)
	}
	if err = writeCredentialFile(filepath.Join(dest, "backup-manifest.json"), content); err != nil {
		return failed(err)
	}
	complete := filepath.Join(parent, strings.TrimPrefix(filepath.Base(dest), ".incomplete-"))
	if err = os.Rename(dest, complete); err != nil {
		return failed(err)
	}
	return complete, syncDirectory(parent)
}

func (s *Studio) dataRoutes(g *gin.RouterGroup) {
	g.GET("/system/data", func(c *gin.Context) {
		result(c, gin.H{"directory": s.data, "backup_directory": filepath.Join(filepath.Dir(s.data), filepath.Base(s.data)+" Backups"), "schema": dataSchemaVersion}, nil)
	})
	g.POST("/system/data/open", func(c *gin.Context) {
		program, args := directoryOpener(s.data)
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()
		err := exec.CommandContext(ctx, program, args...).Run()
		result(c, gin.H{"ok": err == nil}, err)
	})
	g.POST("/system/data/backup", func(c *gin.Context) {
		// The router holds the exclusive gate: no request can start a new worker.
		s.mu.Lock()
		active := len(s.running) > 0
		s.mu.Unlock()
		if active {
			result(c, nil, errors.New("请等待当前任务结束后再备份，备份期间暂停编辑"))
			return
		}
		path, err := backupUserData(s.store.db, s.data)
		result(c, gin.H{"directory": path}, err)
	})
}
