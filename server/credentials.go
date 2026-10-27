package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (s *Studio) providerKey(p Provider) (string, error) {
	if p.CredentialSource != "file" {
		key := os.Getenv(p.KeyEnv)
		if key == "" {
			return "", fmt.Errorf("%s 缺少环境变量 %s", p.Name, p.KeyEnv)
		}
		return key, nil
	}
	s.secretsMu.Lock()
	defer s.secretsMu.Unlock()
	if !nameRE.MatchString(p.ID) || !nameRE.MatchString("v"+p.CredentialVersion) {
		return "", errors.New("凭据未保存")
	}
	key, err := os.ReadFile(filepath.Join(s.data, "secrets", "master.key"))
	if err != nil || len(key) != 32 {
		return "", errors.New("凭据主密钥缺失或损坏，请恢复备份或重新配置")
	}
	aead, err := credentialCipher(key)
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(filepath.Join(s.data, "secrets", p.ID+"-"+p.CredentialVersion+".enc"))
	if err != nil || len(raw) < aead.NonceSize() {
		return "", errors.New("凭据密文缺失或损坏")
	}
	plain, err := aead.Open(nil, raw[:aead.NonceSize()], raw[aead.NonceSize():], []byte(p.ID+":"+p.CredentialVersion))
	if err != nil {
		return "", errors.New("凭据解密失败，请恢复匹配的主密钥和密文")
	}
	return string(plain), nil
}
func credentialCipher(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errors.New("凭据主密钥无效")
	}
	return cipher.NewGCM(block)
}
func (s *Studio) writeCredential(id, secret string) (string, error) {
	s.secretsMu.Lock()
	defer s.secretsMu.Unlock()
	if !nameRE.MatchString(id) || strings.TrimSpace(secret) == "" || len(secret) > 8192 || strings.ContainsAny(secret, "\r\n") {
		return "", errors.New("API Key 无效")
	}
	dir := filepath.Join(s.data, "secrets")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", errors.New("无法创建凭据目录")
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return "", err
	}
	keyPath := filepath.Join(dir, "master.key")
	key, err := os.ReadFile(keyPath)
	if os.IsNotExist(err) {
		entries, e := os.ReadDir(dir)
		if e != nil {
			return "", e
		}
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), ".enc") {
				return "", errors.New("已有密文但主密钥缺失，请先恢复主密钥")
			}
		}
		key = make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return "", err
		}
		f, e := os.OpenFile(keyPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return "", e
		}
		_, err = f.Write(key)
		if err == nil {
			err = f.Sync()
		}
		ce := f.Close()
		if err == nil {
			err = ce
		}
	}
	if err != nil || len(key) != 32 {
		return "", errors.New("凭据主密钥不可用")
	}
	if err = os.Chmod(keyPath, 0600); err != nil {
		return "", err
	}
	aead, err := credentialCipher(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", err
	}
	version := uid()
	raw := aead.Seal(nonce, nonce, []byte(strings.TrimSpace(secret)), []byte(id+":"+version))
	if err = atomicWrite(filepath.Join(dir, id+"-"+version+".enc"), raw); err != nil {
		return "", errors.New("无法保存加密凭据")
	}
	return version, nil
}
func providerByID(settings Settings, id string) (Provider, error) {
	for _, p := range settings.Providers {
		if p.ID == id {
			return p, nil
		}
	}
	return Provider{}, errors.New("供应商不存在")
}
func noRunning(st *State) error {
	for _, p := range st.Projects {
		if p.Status == "running" {
			return errors.New("存在运行中任务，请结束后修改服务配置")
		}
	}
	for _, p := range st.Probes {
		if p.State == "running" {
			return errors.New("生图验证进行中，请结束后修改服务配置")
		}
	}
	return nil
}
