package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUserDataDirectoryIsIndependentOfProgram(t *testing.T) {
	base := t.TempDir()
	if runtime.GOOS == "windows" {
		t.Setenv("LOCALAPPDATA", base)
	} else {
		t.Setenv("HOME", base)
		t.Setenv("XDG_CONFIG_HOME", base)
	}
	got, err := userDataDir()
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(got) || !strings.HasPrefix(got, base+string(os.PathSeparator)) || filepath.Base(got) != "Lypit Studio" {
		t.Fatal(got)
	}
}

func TestUserDataBackupReopensProjectsAssetsAndCredentials(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "个人数据 空格")
	s, err := newStudio(context.Background(), filepath.Join(parent, "程序1"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.store.close()
	p, err := s.store.create("保留的作品", "故事", "电影感", "9:16", 60)
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.encryptCredential("vendor", "private-test-secret")
	if err != nil {
		t.Fatal(err)
	}
	asset := filepath.Join(dir, "media", "画面.png")
	if err = os.MkdirAll(filepath.Dir(asset), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(asset, []byte("original pixels"), 0600); err != nil {
		t.Fatal(err)
	}
	backup, err := backupUserData(s.store.db, dir)
	if err != nil {
		t.Fatal(err)
	}
	manifestBytes, err := os.ReadFile(filepath.Join(backup, "backup-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest dataBackupManifest
	if err = json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	for name, want := range manifest.Files {
		b, e := os.ReadFile(filepath.Join(backup, filepath.FromSlash(name)))
		if e != nil {
			t.Fatal(e)
		}
		sum := sha256.Sum256(b)
		if hex.EncodeToString(sum[:]) != want {
			t.Fatal("backup hash mismatch", name)
		}
	}
	restored, err := newStudio(context.Background(), filepath.Join(parent, "程序2"), backup)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.store.close()
	got, err := restored.store.project(p.ID)
	if err != nil || got.Title != p.Title {
		t.Fatal(got, err)
	}
	key, err := restored.credential(Endpoint{KeyMode: "local", VendorID: "vendor", CredentialID: id})
	if err != nil || key != "private-test-secret" {
		t.Fatal("credential not restored", err)
	}
	b, err := os.ReadFile(filepath.Join(backup, "media", "画面.png"))
	if err != nil || string(b) != "original pixels" {
		t.Fatal("asset missing", err)
	}
	if _, err = backupUserData(restored.store.db, backup); err != nil {
		t.Fatal("backup after restoration failed", err)
	}
}

func TestUserDataNewerSchemaRefusedWithoutReset(t *testing.T) {
	dir := t.TempDir()
	st, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	p, err := st.create("不可重建", "故事", "电影感", "9:16", 60)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.db.Exec("PRAGMA user_version=999"); err != nil {
		t.Fatal(err)
	}
	st.close()
	if reopened, err := openStore(dir); err == nil {
		reopened.close()
		t.Fatal("opened newer schema")
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "studio.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version int
	var title string
	if err = db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 999 {
		t.Fatal(version, err)
	}
	if err = db.QueryRow("SELECT body FROM projects WHERE id=?", p.ID).Scan(&title); err != nil || !strings.Contains(title, p.Title) {
		t.Fatal("data changed", err)
	}
}

func TestUserDataBackupBlocksWhileWorkerActive(t *testing.T) {
	s := testStudio(t)
	s.running["busy"] = func() {}
	response := request(t, s, "POST", "/api/system/data/backup", map[string]any{}, 400)
	if !strings.Contains(string(response), "任务结束") {
		t.Fatal(string(response))
	}
	delete(s.running, "busy")
	request(t, s, "POST", "/api/system/data/backup", map[string]any{}, 200)
}

func TestUserDataSchemaUpgradeMakesBackupFirst(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	st, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	p, err := st.create("升级前", "故事", "电影感", "9:16", 60)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.db.Exec("PRAGMA user_version=0"); err != nil {
		t.Fatal(err)
	}
	st.close()
	st, err = openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.close()
	entries, err := os.ReadDir(dir + " Backups")
	if err != nil || len(entries) != 1 {
		t.Fatal(entries, err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir+" Backups", entries[0].Name(), "studio.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version int
	var count int
	if err = db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 0 {
		t.Fatal(version, err)
	}
	if err = db.QueryRow("SELECT count(*) FROM projects WHERE id=?", p.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}

func TestUserDataBackupFailurePreventsSchemaUpgrade(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	st, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.db.Exec("PRAGMA user_version=0"); err != nil {
		t.Fatal(err)
	}
	st.close()
	// A non-directory at the backup destination forces a real filesystem failure.
	if err = os.WriteFile(dir+" Backups", []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if reopened, err := openStore(dir); err == nil {
		reopened.close()
		t.Fatal("upgrade continued without backup")
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "studio.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version int
	if err = db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 0 {
		t.Fatal(version, err)
	}
}
