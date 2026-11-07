package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var credentialPattern = regexp.MustCompile(`^[a-f0-9]{24}$`)

// Only ciphertext is stored here; SQLite and task snapshots contain immutable references.
// The master key never leaves this directory. Losing it must not silently create a replacement.
func (s *Studio) credentialMaster(create bool) ([]byte, error) {
	dir := filepath.Join(s.data, "credentials")
	path := filepath.Join(dir, "master.key")
	if create {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, errors.New("无法创建本机密钥目录")
		}
		if err := protectCredentialDir(dir); err != nil {
			return nil, errors.New("无法保护本机密钥目录")
		}
	}
	b, err := privateCredentialFile(path, 32)
	if err == nil && len(b) == 32 {
		return b, nil
	}
	if !create || !errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("本机加密主密钥缺失或损坏，请恢复密钥目录备份")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, errors.New("无法读取本机密钥目录")
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".enc") {
			return nil, errors.New("已有密文但主密钥缺失，请恢复原主密钥，不会生成替代密钥")
		}
	}
	b = make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return nil, errors.New("无法创建加密主密钥")
	}
	if err = writeCredentialFile(path, b); err != nil {
		return nil, errors.New("无法保存加密主密钥")
	}
	return b, nil
}

func privateCredentialFile(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || !credentialPermissionsOK(path, info) || info.Size() > limit {
		return nil, errors.New("密钥文件类型、权限或大小无效")
	}
	return os.ReadFile(path)
}

// Publish a complete file without overwriting any existing version, even after a crash.
func writeCredentialFile(path string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".credential-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = protectCredentialFile(f.Name()); err != nil {
		f.Close()
		return err
	}
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Link(f.Name(), path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func (s *Studio) encryptCredential(vendorID, secret string) (string, error) {
	master, err := s.credentialMaster(true)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(master)
	if err != nil {
		return "", errors.New("无法初始化本机加密")
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", errors.New("无法初始化本机加密")
	}
	id := uid()
	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", errors.New("无法创建密钥随机数")
	}
	sealed := gcm.Seal(nonce, nonce, []byte(secret), []byte(vendorID+":"+id))
	if err = writeCredentialFile(filepath.Join(s.data, "credentials", id+".enc"), sealed); err != nil {
		return "", errors.New("本机密钥加密保存失败，旧配置保持不变")
	}
	return id, nil
}

func (s *Studio) credential(ep Endpoint) (string, error) {
	if ep.KeyMode != "local" {
		key := os.Getenv(ep.KeyEnv)
		if key == "" {
			return "", errors.New("厂商未配置API Key，请在厂商管理中设置")
		}
		return key, nil
	}
	if !credentialPattern.MatchString(ep.CredentialID) {
		return "", errors.New("厂商尚未保存本机API Key")
	}
	master, err := s.credentialMaster(false)
	if err != nil {
		return "", err
	}
	sealed, err := privateCredentialFile(filepath.Join(s.data, "credentials", ep.CredentialID+".enc"), 16<<10)
	if err != nil {
		return "", errors.New("本机API Key密文无法读取，请恢复备份或重新设置")
	}
	block, _ := aes.NewCipher(master)
	gcm, _ := cipher.NewGCM(block)
	if len(sealed) < gcm.NonceSize() {
		return "", errors.New("本机API Key密文损坏")
	}
	plain, err := gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], []byte(ep.VendorID+":"+ep.CredentialID))
	if err != nil {
		return "", errors.New("本机API Key解密失败，请恢复备份或重新设置")
	}
	return string(plain), nil
}

func (s *Studio) prepareCredentials(v *Settings, old Settings, keys map[string]string) error {
	for id, key := range keys {
		found := false
		for _, vendor := range v.Vendors {
			if vendor.ID == id && vendor.Type == "api" && vendor.KeyMode == "local" {
				found = true
			}
		}
		if !found || len(key) > 8192 || strings.ContainsAny(key, "\r\n\x00") || (key != "" && strings.TrimSpace(key) != key) {
			return errors.New("API Key或目标厂商无效，请检查输入")
		}
	}
	// Validate every unchanged reference before writing any new immutable versions.
	for i := range v.Vendors {
		vendor := &v.Vendors[i]
		vendor.CredentialID = "" // Never trust a client-supplied credential reference.
		for _, prev := range old.Vendors {
			if prev.ID == vendor.ID {
				vendor.CredentialID = prev.CredentialID
				break
			}
		}
		if vendor.Type != "api" {
			vendor.CredentialID = ""
			continue
		}
		if vendor.KeyMode == "local" && keys[vendor.ID] == "" {
			if _, err := s.credential(Endpoint{VendorID: vendor.ID, KeyMode: "local", CredentialID: vendor.CredentialID}); err != nil {
				return err
			}
		}
	}
	for i := range v.Vendors {
		vendor := &v.Vendors[i]
		if key := keys[vendor.ID]; key != "" {
			id, err := s.encryptCredential(vendor.ID, key)
			if err != nil {
				return err
			}
			vendor.CredentialID = id
		}
	}
	return nil
}
