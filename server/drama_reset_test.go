package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDramaResetArchivesAndRetainsCiphertext(t *testing.T) {
	root, _ := filepath.Abs("..")
	dir := filepath.Join(t.TempDir(), "私有 数据")
	s, e := newStudio(context.Background(), root, dir)
	if e != nil {
		t.Fatal(e)
	}
	w := dramaTestWork(t, s)
	credential, e := s.encryptCredential("vendor", "test-secret-not-exported")
	if e != nil {
		t.Fatal(e)
	}
	cfg, _ := s.store.settings()
	cfg.Vendors = []Vendor{{ID: "vendor", Name: "音乐", Type: "api", Kind: "minimax", Protocol: "minimax", Base: "https://api.minimax.io/v1", KeyMode: "local", CredentialID: credential}}
	cfg.Music = Endpoint{VendorID: "vendor", Model: "music-3.0"}
	cfg.PromptTemplates = map[string]string{"analyze": "old"}
	s.store.db.Exec("UPDATE settings SET body=?", jsonBytes(cfg))
	s.store.close()
	archive, e := resetDramaData(dir)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(archive, "studio.db")); e != nil {
		t.Fatal(e)
	}
	fresh, e := newStudio(context.Background(), root, dir)
	if e != nil {
		t.Fatal(e)
	}
	defer fresh.store.close()
	if _, e = fresh.dramaWork(w.ID); e == nil {
		t.Fatal("old work migrated")
	}
	got, _ := fresh.store.settings()
	if len(got.PromptTemplates) != 0 || got.Music.Model != "music-3.0" {
		t.Fatal("configuration migration failed")
	}
	secret, e := fresh.credential(Endpoint{VendorID: "vendor", KeyMode: "local", CredentialID: credential})
	if e != nil || secret != "test-secret-not-exported" {
		t.Fatal("ciphertext unusable")
	}
}
