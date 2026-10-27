package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

type ImageProbe struct {
	ID           string   `json:"id"`
	Provider     Provider `json:"provider"`
	Model        Model    `json:"model"`
	ProviderHash string   `json:"provider_hash"`
	State        string   `json:"state"`
	Quote        Quote    `json:"quote"`
	Image        string   `json:"image"`
	Error        string   `json:"error"`
}

func (s *Studio) modelRoutes(api *gin.RouterGroup) {
	api.GET("/settings", func(c *gin.Context) {
		settings := s.store.settings()
		present := map[string]bool{}
		credentialErrors := map[string]string{}
		for _, p := range settings.Providers {
			if p.Adapter == "mock" {
				present[p.ID] = true
			} else if p.Adapter != "cli" {
				_, e := s.providerKey(p)
				present[p.ID] = e == nil
				if e != nil {
					credentialErrors[p.ID] = e.Error()
				}
			}
		}
		c.Header("Cache-Control", "no-store")
		c.JSON(200, gin.H{"settings": settings, "credentials_present": present, "credential_errors": credentialErrors})
	})
	api.PUT("/settings", func(c *gin.Context) {
		var settings Settings
		if e := c.ShouldBindJSON(&settings); e != nil {
			fail(c, errors.New("设置格式无效"))
			return
		}
		e := s.store.change(func(st *State) error {
			if e := noRunning(st); e != nil {
				return e
			}
			for i := range settings.Providers {
				p := &settings.Providers[i]
				old, _ := providerByID(st.Settings, p.ID)
				if p.CredentialVersion != old.CredentialVersion {
					return errors.New("凭据版本已变化，请重新加载配置")
				}
				p.ImageVerified = old.ImageVerified
				if p.Adapter == "cli" && p.CLI != nil && !nativeCodexImage(p.CLI) {
					if p.CLI.SkillPath == "" {
						p.CLI.SkillDigest = ""
					} else {
						_, digest, e := readCLISkill(p.CLI.SkillPath)
						if e != nil {
							return e
						}
						p.CLI.SkillDigest = digest
					}
				}
				if p.Adapter != "mock" && p.Adapter != "cli" && p.CredentialSource == "" {
					p.CredentialSource = "env"
				}
			}
			if e := validateSettings(settings); e != nil {
				return e
			}
			st.Settings = settings
			return nil
		})
		if e != nil {
			fail(c, e)
			return
		}
		c.JSON(200, settings)
	})
	api.PUT("/providers/:id/credential", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
		var body struct {
			Key     string `json:"key"`
			Version string `json:"version"`
		}
		if c.ShouldBindJSON(&body) != nil {
			fail(c, errors.New("凭据请求无效"))
			return
		}
		e := s.store.change(func(st *State) error {
			if e := noRunning(st); e != nil {
				return e
			}
			for i := range st.Settings.Providers {
				p := &st.Settings.Providers[i]
				if p.ID != c.Param("id") {
					continue
				}
				if p.Adapter == "mock" || p.Adapter == "cli" {
					return errors.New("此服务不使用远程 API Key")
				}
				if body.Version != p.CredentialVersion {
					return errors.New("凭据已变化，请重新加载")
				}
				version, e := s.writeCredential(p.ID, body.Key)
				if e != nil {
					return e
				}
				p.CredentialSource = "file"
				p.CredentialVersion = version
				return nil
			}
			return errors.New("供应商不存在")
		})
		body.Key = ""
		if e != nil {
			fail(c, e)
			return
		}
		c.JSON(200, gin.H{"message": "Key 已加密保存，不再回显"})
	})
	api.DELETE("/providers/:id/credential", func(c *gin.Context) {
		var body struct {
			Version string `json:"version"`
		}
		if c.ShouldBindJSON(&body) != nil {
			fail(c, errors.New("凭据版本无效"))
			return
		}
		e := s.store.change(func(st *State) error {
			if e := noRunning(st); e != nil {
				return e
			}
			for i := range st.Settings.Providers {
				p := &st.Settings.Providers[i]
				if p.ID == c.Param("id") {
					if p.CredentialVersion != body.Version {
						return errors.New("凭据已变化，请重新加载")
					}
					p.CredentialVersion = ""
					return nil
				}
			}
			return errors.New("供应商不存在")
		})
		if e != nil {
			fail(c, e)
			return
		}
		c.JSON(200, gin.H{"message": "已解除当前 Key 绑定；历史密文保留供已确认任务恢复"})
	})
	api.GET("/cli/status", func(c *gin.Context) {
		statuses := []CLIStatus{}
		for _, kind := range []string{"codex", "claude", "gemini"} {
			config := &CLIConfig{Kind: kind}
			for _, p := range s.store.settings().Providers {
				if p.Adapter == "cli" && p.CLI != nil && p.CLI.Kind == kind {
					config = p.CLI
					break
				}
			}
			status := detectCLI(c.Request.Context(), config)
			if kind == "codex" && status.Installed {
				catalog := s.cliModels(c.Request.Context(), config, false)
				status.Catalog = &catalog
				status.Models = catalog.Models
			}
			statuses = append(statuses, status)
		}
		c.JSON(200, statuses)
	})
	api.POST("/providers/:id/test", func(c *gin.Context) {
		p, e := providerByID(s.store.settings(), c.Param("id"))
		if e != nil {
			fail(c, e)
			return
		}
		if p.Adapter == "mock" {
			c.JSON(200, gin.H{"message": "模拟适配器就绪，不产生模型费用"})
			return
		}
		if p.Adapter == "cli" {
			status := detectCLI(c.Request.Context(), p.CLI)
			if p.CLI.Kind == "codex" && status.Installed {
				catalog := s.cliModels(c.Request.Context(), p.CLI, true)
				status.Catalog = &catalog
				status.Models = catalog.Models
			}
			c.JSON(200, gin.H{"message": status.Message, "cli": status})
			return
		}
		if _, e = s.providerKey(p); e != nil {
			fail(c, e)
			return
		}
		if p.Adapter == "seedance" {
			c.JSON(200, gin.H{"message": "凭据可用；未发起生成，视频权限需报价确认后验证"})
			return
		}
		models, e := s.providerModels(c.Request.Context(), p)
		if e != nil {
			fail(c, e)
			return
		}
		c.JSON(200, gin.H{"message": fmt.Sprintf("模型目录连接成功，读取 %d 个模型；未发起生成，不代表全部能力可用", len(models)), "models": models})
	})
	api.POST("/cli/models", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<10)
		var body struct {
			Kind    string `json:"kind"`
			Path    string `json:"path"`
			Refresh bool   `json:"refresh"`
		}
		if c.ShouldBindJSON(&body) != nil || !slices.Contains([]string{"codex", "claude", "gemini"}, body.Kind) {
			fail(c, errors.New("CLI 目录请求无效"))
			return
		}
		c.Header("Cache-Control", "no-store")
		c.JSON(200, s.cliModels(c.Request.Context(), &CLIConfig{Kind: body.Kind, Path: body.Path}, body.Refresh))
	})
	api.GET("/providers/:id/models", func(c *gin.Context) {
		p, e := providerByID(s.store.settings(), c.Param("id"))
		if e != nil {
			fail(c, e)
			return
		}
		if p.Adapter == "cli" {
			c.Header("Cache-Control", "no-store")
			c.JSON(200, s.cliModels(c.Request.Context(), p.CLI, c.Query("refresh") == "true"))
			return
		}
		models, e := s.providerModels(c.Request.Context(), p)
		if e != nil {
			fail(c, e)
			return
		}
		c.JSON(200, gin.H{"models": models})
	})
	api.GET("/providers/:id/image-verifications", func(c *gin.Context) {
		probes := []ImageProbe{}
		s.store.mu.Lock()
		for _, probe := range s.store.state.Probes {
			if probe.Provider.ID == c.Param("id") {
				probes = append(probes, clone(probe))
			}
		}
		s.store.mu.Unlock()
		sort.Slice(probes, func(i, j int) bool { return probes[i].Quote.Created > probes[j].Quote.Created })
		c.JSON(200, probes)
	})
	api.POST("/providers/:id/image-verification/quote", func(c *gin.Context) {
		var body struct {
			Model string `json:"model"`
		}
		if c.ShouldBindJSON(&body) != nil {
			fail(c, errors.New("请选择验证模型"))
			return
		}
		probe, e := s.quoteImageProbe(c.Param("id"), body.Model)
		if e != nil {
			fail(c, e)
			return
		}
		c.JSON(200, probe)
	})
	api.POST("/image-verifications/:id/confirm", func(c *gin.Context) {
		var body struct {
			AllowUnknown bool `json:"allow_unknown"`
		}
		if c.ShouldBindJSON(&body) != nil || !body.AllowUnknown {
			fail(c, errors.New("需要明确接受单次 CLI 验证费用未知"))
			return
		}
		probe, e := s.startImageProbe(c.Param("id"))
		if e != nil {
			fail(c, e)
			return
		}
		s.launchImageProbe(probe, false)
		c.JSON(202, probe)
	})
	api.GET("/image-verifications/:id", func(c *gin.Context) {
		s.store.mu.Lock()
		probe, ok := s.store.state.Probes[c.Param("id")]
		s.store.mu.Unlock()
		if !ok {
			c.Status(404)
			return
		}
		c.JSON(200, probe)
	})
	api.POST("/image-verifications/:id/resume", func(c *gin.Context) {
		var probe ImageProbe
		e := s.store.change(func(st *State) error {
			var ok bool
			probe, ok = st.Probes[c.Param("id")]
			if !ok || probe.State == "running" || probe.State == "pending" || probe.State == "succeeded" {
				return errors.New("当前验证不可恢复")
			}
			if e := noRunning(st); e != nil {
				return e
			}
			if _, e := os.Stat(filepath.Join(s.probeDir(probe.ID), "response.bin")); e != nil {
				return errors.New("没有已保存响应，不能重提；请先核对 CLI 记录")
			}
			probe.State = "running"
			st.Probes[probe.ID] = probe
			return nil
		})
		if e != nil {
			fail(c, e)
			return
		}
		s.launchImageProbe(probe, true)
		c.JSON(202, probe)
	})
}
func (s *Studio) providerModels(ctx context.Context, p Provider) ([]string, error) {
	models := []string{}
	if p.Adapter == "cli" {
		result := s.cliModels(ctx, p.CLI, false)
		return result.Models, nil
	}
	if p.Adapter == "seedance" || p.Adapter == "mock" {
		for _, m := range p.Models {
			models = append(models, m.ID)
		}
		return models, nil
	}
	key, e := s.providerKey(p)
	if e != nil {
		return nil, e
	}
	p.credential = key
	req, e := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(p.BaseURL, "/")+"/models", nil)
	if e != nil {
		return nil, errors.New("模型目录地址无效")
	}
	setProviderAuth(req, p)
	resp, e := httpClient().Do(req)
	if e != nil {
		return nil, errors.New("模型目录连接失败")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("模型目录 HTTP %d", resp.StatusCode)
	}
	raw, e := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if e != nil {
		return nil, errors.New("无法读取模型目录")
	}
	var response struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if json.Unmarshal(raw, &response) != nil {
		return nil, errors.New("模型目录格式无效，请手动填写模型 ID")
	}
	for _, m := range response.Data {
		if m.ID != "" {
			models = append(models, m.ID)
		}
	}
	for _, m := range response.Models {
		if m.Name != "" {
			models = append(models, strings.TrimPrefix(m.Name, "models/"))
		}
	}
	return models, nil
}
func (s *Studio) quoteImageProbe(id, modelID string) (ImageProbe, error) {
	var probe ImageProbe
	e := s.store.change(func(st *State) error {
		if e := noRunning(st); e != nil {
			return e
		}
		p, e := providerByID(st.Settings, id)
		if e != nil {
			return e
		}
		if p.Adapter != "cli" || p.CLI == nil || !nativeCodexImage(p.CLI) && strings.TrimSpace(p.CLI.ImageInstructions) == "" {
			return errors.New("请先保存 CLI 生图工具或 Skill 说明")
		}
		if _, e = cliExecutable(p.CLI); e != nil {
			return e
		}
		if e = validateCLISkill(p.CLI); e != nil {
			return e
		}
		var m Model
		for _, v := range p.Models {
			if v.ID == modelID {
				m = v
			}
		}
		if m.ID == "" || !slices.Contains(m.Capabilities, "image") {
			return errors.New("模型不存在")
		}
		p = clone(p)
		m = clone(m)
		q := Quote{Schema: 2, ID: uid(), Stage: "cli_image_verification", Created: now(), SettingsHash: hash(p), Notice: "只启动一次 CLI 验证任务，要求生成一张真实图片。CLI 内部模型调用次数与费用未知，不能承诺金额封顶。失败不自动重试。", Requests: []RequestSpec{{Kind: "image", Provider: p.Name, Model: m.ID, Prompt: "生成一张无文字的蓝色陶瓷杯静物照片，柔和自然光。", Count: 1, References: []string{}, Parameters: map[string]any{"cli": p.CLI, "cli_options": m.CLIOptions, "sizes": m.Sizes}}}}
		probe = ImageProbe{ID: q.ID, Provider: p, Model: m, ProviderHash: hash(p), State: "pending", Quote: q}
		if st.Probes == nil {
			st.Probes = map[string]ImageProbe{}
		}
		st.Probes[probe.ID] = probe
		return nil
	})
	return probe, e
}
func (s *Studio) startImageProbe(id string) (ImageProbe, error) {
	var probe ImageProbe
	e := s.store.change(func(st *State) error {
		var ok bool
		probe, ok = st.Probes[id]
		if !ok || probe.State != "pending" || probe.Quote.Used {
			return errors.New("验证报价不存在或已使用")
		}
		if e := noRunning(st); e != nil {
			return e
		}
		p, e := providerByID(st.Settings, probe.Provider.ID)
		if e != nil || hash(p) != probe.ProviderHash {
			return errors.New("服务配置已变化，请重新报价")
		}
		probe.State = "running"
		probe.Quote.Used = true
		st.Probes[id] = probe
		return nil
	})
	return probe, e
}
func (s *Studio) probeDir(id string) string {
	return filepath.Join(s.data, "media", "verification", id)
}
func (s *Studio) launchImageProbe(probe ImageProbe, recovering bool) {
	ctx, cancel := context.WithCancel(s.ctx)
	s.mu.Lock()
	s.running["probe:"+probe.ID] = cancel
	s.wg.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.wg.Done()
		defer cancel()
		defer func() { s.mu.Lock(); delete(s.running, "probe:"+probe.ID); s.mu.Unlock() }()
		dir := s.probeDir(probe.ID)
		err := os.MkdirAll(dir, 0700)
		var raw, data []byte
		ext := ""
		if err == nil {
			raw, err = s.executeCLI(ctx, probe.Provider, probe.Model, "image", probe.Quote.Requests[0].Prompt, nil, dir, recovering)
		}
		if err == nil {
			data, ext, err = cliImageOutput(dir, raw, probe.Model)
		}
		imagePath := filepath.Join("verification", probe.ID, "result."+ext)
		if err == nil {
			err = atomicWrite(s.abs(imagePath), data)
		}
		_ = s.store.change(func(st *State) error {
			current := st.Probes[probe.ID]
			current.State = "failed"
			if err != nil {
				current.Error = err.Error()
			} else {
				current.State = "succeeded"
				current.Image = imagePath
				current.Error = ""
				for i := range st.Settings.Providers {
					p := &st.Settings.Providers[i]
					if p.ID == probe.Provider.ID && hash(*p) == probe.ProviderHash {
						if p.ImageVerified == nil {
							p.ImageVerified = map[string]string{}
						}
						p.ImageVerified[probe.Model.ID] = cliImageFingerprint(*p, probe.Model)
					}
				}
			}
			st.Probes[probe.ID] = current
			return nil
		})
	}()
}
