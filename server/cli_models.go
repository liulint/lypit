package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Read metadata only. Never start a CLI, inference request, or login flow here.
func localCLIModels(v Vendor) ModelCatalog {
	cat := ModelCatalog{Models: []CatalogModel{}, Updated: time.Now().UTC()}
	if v.CLI == "claude" {
		cat.Source = "Claude Code 标准模型别名（非账户模型目录）"
		for _, id := range []string{"sonnet", "opus", "haiku"} {
			cat.Models = append(cat.Models, CatalogModel{ID: id, Name: id, Capabilities: []string{"text"}, Services: []string{"text"}})
		}
		return cat
	}
	cat.Source = "Codex 本机模型缓存（只读）"
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			cat.Error = "无法定位 Codex 配置目录，可手动填写模型 ID"
			return cat
		}
		home = filepath.Join(userHome, ".codex")
	}
	f, err := os.Open(filepath.Join(home, "models_cache.json"))
	if err != nil {
		cat.Error = "未读到 Codex 模型缓存，请在 CLI 更新模型列表后刷新，或手动填写模型 ID"
		return cat
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 8<<20 {
		cat.Error = "Codex 模型缓存文件无效，可手动填写模型 ID"
		return cat
	}
	b, err := io.ReadAll(io.LimitReader(f, (8<<20)+1))
	var cache struct {
		FetchedAt time.Time `json:"fetched_at"`
		Models    []struct {
			Slug       string `json:"slug"`
			Name       string `json:"display_name"`
			Visibility string `json:"visibility"`
		} `json:"models"`
	}
	if err != nil || len(b) > 8<<20 || json.Unmarshal(b, &cache) != nil || cache.Models == nil {
		cat.Error = "Codex 模型缓存格式不兼容，可手动填写模型 ID"
		return cat
	}
	cat.Updated = cache.FetchedAt
	if cat.Updated.IsZero() {
		cat.Updated = info.ModTime().UTC()
	}
	cat.Stale = time.Since(cat.Updated) > 24*time.Hour
	seen := map[string]bool{}
	for _, m := range cache.Models {
		if m.Visibility != "list" || strings.TrimSpace(m.Slug) == "" || len(m.Slug) > 300 || strings.ContainsAny(m.Slug, "\r\n\x00") || seen[m.Slug] {
			continue
		}
		seen[m.Slug] = true
		if m.Name == "" {
			m.Name = m.Slug
		}
		// These are Codex execution models, including when invoking the imagegen skill.
		cat.Models = append(cat.Models, CatalogModel{ID: m.Slug, Name: m.Name, Capabilities: []string{"text"}, Services: vendorCapabilities(v)})
	}
	return cat
}
