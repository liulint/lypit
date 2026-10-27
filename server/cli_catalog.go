package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

type CLIChoice struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}
type CLIModelOption struct {
	ID               string      `json:"id"`
	Label            string      `json:"label"`
	Description      string      `json:"description,omitempty"`
	Reasoning        []CLIChoice `json:"reasoning"`
	DefaultReasoning string      `json:"default_reasoning,omitempty"`
	ServiceTiers     []CLIChoice `json:"service_tiers"`
}
type CLICatalog struct {
	Models    []string         `json:"models"`
	Options   []CLIModelOption `json:"options"`
	Source    string           `json:"source"`
	FetchedAt string           `json:"fetched_at,omitempty"`
	Stale     bool             `json:"stale"`
	Error     string           `json:"error,omitempty"`
}
type catalogEntry struct {
	Result CLICatalog
	At     time.Time
}
type cliCatalogCache struct {
	mu       sync.Mutex
	entries  map[string]catalogEntry
	pending  map[string]chan struct{}
	versions map[string]string
}

func unavailableCatalog(message string) CLICatalog {
	return CLICatalog{Models: []string{}, Options: []CLIModelOption{}, Source: "unavailable", Error: message}
}
func validateCLIOptions(kind string, o *CLIOptions) error {
	if o == nil {
		return nil
	}
	if kind != "codex" {
		return errors.New("推理强度与服务档位仅支持 Codex")
	}
	if !slices.Contains([]string{"", "default", "none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra"}, o.Reasoning) {
		return errors.New("Codex 推理强度无效")
	}
	if !slices.Contains([]string{"", "default", "priority"}, o.ServiceTier) {
		return errors.New("Codex 服务档位无效")
	}
	return nil
}
func parseCodexCatalog(raw []byte) (CLICatalog, error) {
	var rows []struct {
		ID               string `json:"id"`
		Slug             string `json:"slug"`
		DisplayName      string `json:"display_name"`
		Name             string `json:"name"`
		Description      string `json:"description"`
		Visibility       string `json:"visibility"`
		DefaultReasoning string `json:"default_reasoning_level"`
		Reasoning        []struct {
			Effort      string `json:"effort"`
			Description string `json:"description"`
		} `json:"supported_reasoning_levels"`
		ServiceTiers []struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Label string `json:"label"`
		} `json:"service_tiers"`
		SpeedTiers []string `json:"additional_speed_tiers"`
	}
	raw = []byte(strings.TrimSpace(string(raw)))
	if len(raw) == 0 {
		return CLICatalog{}, errors.New("模型目录为空")
	}
	if raw[0] != '[' {
		var envelope struct {
			Models json.RawMessage `json:"models"`
		}
		if json.Unmarshal(raw, &envelope) != nil || len(envelope.Models) == 0 {
			return CLICatalog{}, errors.New("模型目录格式无效")
		}
		raw = envelope.Models
	}
	if json.Unmarshal(raw, &rows) != nil || rows == nil {
		return CLICatalog{}, errors.New("模型目录格式无效")
	}
	result := CLICatalog{Models: []string{}, Options: []CLIModelOption{}, Source: "live"}
	seen := map[string]bool{}
	for _, r := range rows {
		id := strings.TrimSpace(r.Slug)
		if id == "" {
			id = strings.TrimSpace(r.ID)
		}
		if id == "" || len(id) > 200 || id == "default" || seen[id] || r.Visibility == "hide" || r.Visibility == "hidden" {
			continue
		}
		seen[id] = true
		label := strings.TrimSpace(r.DisplayName)
		if label == "" {
			label = strings.TrimSpace(r.Name)
		}
		if label == "" {
			label = id
		}
		m := CLIModelOption{ID: id, Label: label, Description: r.Description, DefaultReasoning: r.DefaultReasoning, Reasoning: []CLIChoice{}, ServiceTiers: []CLIChoice{}}
		for _, e := range r.Reasoning {
			if e.Effort != "" && validateCLIOptions("codex", &CLIOptions{Reasoning: e.Effort}) == nil && !slices.ContainsFunc(m.Reasoning, func(c CLIChoice) bool { return c.ID == e.Effort }) {
				m.Reasoning = append(m.Reasoning, CLIChoice{ID: e.Effort, Label: e.Effort})
			}
		}
		for _, tier := range r.ServiceTiers {
			if tier.ID != "priority" || slices.ContainsFunc(m.ServiceTiers, func(c CLIChoice) bool { return c.ID == tier.ID }) {
				continue
			}
			label := tier.Name
			if label == "" {
				label = tier.Label
			}
			if label == "" {
				label = tier.ID
			}
			m.ServiceTiers = append(m.ServiceTiers, CLIChoice{ID: tier.ID, Label: label})
		}
		if len(m.ServiceTiers) == 0 && slices.Contains(r.SpeedTiers, "fast") {
			m.ServiceTiers = append(m.ServiceTiers, CLIChoice{ID: "priority", Label: "Fast"})
		}
		result.Models = append(result.Models, id)
		result.Options = append(result.Options, m)
	}
	return result, nil
}

func (cache *cliCatalogCache) get(ctx context.Context, key string, force bool, fetch func(context.Context) (CLICatalog, error)) CLICatalog {
	for {
		cache.mu.Lock()
		if cache.entries == nil {
			cache.entries = map[string]catalogEntry{}
			cache.pending = map[string]chan struct{}{}
		}
		old, ok := cache.entries[key]
		if ok && !force && time.Since(old.At) < 5*time.Minute {
			r := clone(old.Result)
			r.Source = "cache"
			cache.mu.Unlock()
			return r
		}
		if ch := cache.pending[key]; ch != nil {
			cache.mu.Unlock()
			select {
			case <-ctx.Done():
				return unavailableCatalog("模型目录读取已取消或超时")
			case <-ch:
				force = false
				continue
			}
		}
		ch := make(chan struct{})
		cache.pending[key] = ch
		cache.mu.Unlock()
		result, err := fetch(ctx)
		cache.mu.Lock()
		if err == nil {
			result.FetchedAt = now()
			cache.entries[key] = catalogEntry{Result: clone(result), At: time.Now()}
		} else if ok {
			result = clone(old.Result)
			result.Source = "cache"
			result.Stale = true
			result.Error = "模型目录读取失败，显示上次成功目录；可重新刷新"
			cache.entries[key] = catalogEntry{Result: clone(result), At: old.At}
		} else {
			result = unavailableCatalog("模型目录读取失败或超时，请核对 CLI 路径、登录及版本后重试")
		}
		delete(cache.pending, key)
		close(ch)
		cache.mu.Unlock()
		return result
	}
}

func (s *Studio) cliModels(ctx context.Context, c *CLIConfig, force bool) CLICatalog {
	if c == nil || c.Kind != "codex" {
		return unavailableCatalog("当前 CLI 尚未实现模型目录读取，可使用默认模型或手动填写")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	bin, err := cliExecutable(c)
	if err != nil {
		return unavailableCatalog(err.Error())
	}
	info, err := os.Stat(bin)
	if err != nil {
		return unavailableCatalog("CLI 文件不可读取")
	}
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		userHome, _ := os.UserHomeDir()
		home = filepath.Join(userHome, ".codex")
	}
	cwd, _ := os.Getwd()
	identity := []any{bin, info.Size(), info.ModTime().UnixNano(), home, cwd, os.Environ()}
	for _, p := range []string{filepath.Join(home, "config.toml"), filepath.Join(home, "auth.json"), filepath.Join(cwd, ".codex", "config.toml")} {
		if stat, e := os.Stat(p); e == nil {
			identity = append(identity, []any{p, stat.Size(), stat.ModTime().UnixNano()})
		}
	}
	baseKey := hash(identity)
	version, versionErr := runProcessLimit(ctx, bin, "", []string{"--version"}, nil, 4096)
	s.catalogs.mu.Lock()
	if s.catalogs.versions == nil {
		s.catalogs.versions = map[string]string{}
	}
	if versionErr == nil {
		s.catalogs.versions[baseKey] = string(version)
	} else {
		version = []byte(s.catalogs.versions[baseKey])
	}
	s.catalogs.mu.Unlock()
	if len(version) == 0 {
		return unavailableCatalog("CLI 版本读取失败或超时")
	}
	key := hash([]string{baseKey, string(version)})
	if versionErr != nil {
		return s.catalogs.get(ctx, key, true, func(context.Context) (CLICatalog, error) { return CLICatalog{}, versionErr })
	}
	return s.catalogs.get(ctx, key, force, func(ctx context.Context) (CLICatalog, error) {
		raw, e := runProcessLimit(ctx, bin, "", []string{"debug", "models"}, nil, 8<<20)
		if e != nil {
			return CLICatalog{}, e
		}
		return parseCodexCatalog(raw)
	})
}
