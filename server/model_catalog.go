package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

var workspacePattern = regexp.MustCompile(`^[A-Za-z0-9-]{1,100}$`)
var regionPattern = regexp.MustCompile(`^[a-z0-9-]{1,60}$`)

type CatalogModel struct {
	ID           string        `json:"id"`
	Name         string        `json:"name"`
	Capabilities []string      `json:"capabilities"`
	Services     []string      `json:"services"`
	Verified     bool          `json:"verified"`
	Pricing      *ModelPricing `json:"pricing,omitempty"`
}
type ModelCatalog struct {
	Models  []CatalogModel `json:"models"`
	Source  string         `json:"source"`
	Updated time.Time      `json:"updated"`
	Stale   bool           `json:"stale"`
	Error   string         `json:"error,omitempty"`
}

// Only current settings are normalized. Never apply this to historical task configs.
func upgradeCurrentRegistry(v Settings) Settings {
	v.Vendors = append([]Vendor(nil), v.Vendors...)
	legacy := map[string]bool{}
	for i := range v.Vendors {
		p := &v.Vendors[i]
		if p.Kind == "" {
			if p.Type == "cli" {
				p.Kind = p.CLI
			} else if p.Protocol == "ark" {
				p.Kind = "ark"
			} else {
				p.Kind = "custom"
			}
		}
		for _, cap := range capabilities {
			names := map[string]string{"text": "文本服务", "image": "图片服务", "video": "视频服务", "voice": "旁白服务"}
			if p.ID == "legacy-"+cap && p.Name == names[cap]+"（原配置）" {
				legacy[p.ID] = true
				switch p.Kind {
				case "codex":
					p.Name = "Codex CLI"
				case "claude":
					p.Name = "Claude Code"
				case "ark":
					p.Name = "火山方舟（豆包）"
				default:
					p.Name = "兼容 API 连接"
				}
			}
		}
	}
	// Merge only exactly equal connection identities; names do not define connections.
	out := []Vendor{}
	for _, p := range v.Vendors {
		match := ""
		for _, q := range out {
			a, b := p, q
			a.ID, a.Name, b.ID, b.Name = "", "", "", ""
			if legacy[p.ID] && legacy[q.ID] && reflect.DeepEqual(a, b) {
				match = q.ID
				break
			}
		}
		if match == "" {
			out = append(out, p)
			continue
		}
		for _, cap := range capabilities {
			ep := endpointFor(v, cap)
			if ep.VendorID == p.ID {
				ep.VendorID = match
				setEndpoint(&v, cap, ep)
			}
		}
	}
	v.Vendors = out
	return v
}

func inferredCapabilities(id string) []string {
	id = strings.ToLower(id)
	switch {
	case strings.Contains(id, "embedding"), strings.Contains(id, "rerank"), strings.Contains(id, "whisper"), strings.Contains(id, "asr"):
		return []string{"other"}
	case strings.Contains(id, "seedance"), strings.HasPrefix(id, "wan"), strings.Contains(id, "video"):
		return []string{"video"}
	case strings.Contains(id, "tts"), strings.Contains(id, "speech"), strings.Contains(id, "audio"):
		return []string{"voice"}
	case strings.Contains(id, "seedream"), strings.Contains(id, "image"), strings.Contains(id, "dall-e"), strings.HasPrefix(id, "flux"):
		return []string{"image"}
	case strings.HasPrefix(id, "qwen"), strings.HasPrefix(id, "doubao"), strings.HasPrefix(id, "gpt-"), strings.HasPrefix(id, "chatgpt-"), strings.HasPrefix(id, "deepseek"), strings.HasPrefix(id, "o1"), strings.HasPrefix(id, "o3"), strings.HasPrefix(id, "o4"):
		return []string{"text"}
	}
	return nil
}
func contains(a []string, b string) bool {
	for _, x := range a {
		if x == b {
			return true
		}
	}
	return false
}
func modelAllows(v Vendor, model, cap string) bool {
	if v.Type == "cli" {
		return supports(v, cap)
	}
	kinds := inferredCapabilities(model)
	if len(kinds) > 0 && !contains(kinds, cap) {
		return false
	}
	// These families need native request formats, not the compatible image/audio adapters.
	if v.Kind == "ark" && cap == "video" && len(kinds) > 0 {
		return strings.Contains(strings.ToLower(model), "seedance")
	}
	if (v.Kind == "bailian" || v.Kind == "ark") && cap == "text" {
		lower := strings.ToLower(model)
		if strings.Contains(lower, "realtime") || strings.Contains(lower, "omni") || strings.Contains(lower, "live") {
			return false
		}
	}
	return supports(v, cap)
}
func catalogKey(v Vendor, secret string) string {
	b := append(jsonBytes(v), []byte(secret)...)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func describeModel(v Vendor, id, name string, caps []string) CatalogModel {
	if name == "" {
		name = id
	}
	verified := len(caps) > 0
	if len(caps) == 0 {
		caps = inferredCapabilities(id)
		verified = len(caps) > 0
	}
	services := []string{}
	// Metadata must positively identify a capability before catalog selection is enabled.
	for _, cap := range vendorCapabilities(v) {
		if contains(caps, cap) && modelAllows(v, id, cap) {
			services = append(services, cap)
		}
	}
	if caps == nil {
		caps = []string{}
	}
	return CatalogModel{ID: id, Name: name, Capabilities: caps, Services: services, Verified: verified, Pricing: officialModelPricing(v, id)}
}

func (s *Studio) vendorModels(c *gin.Context) {
	cfg, e := s.store.settings()
	if e != nil {
		result(c, nil, e)
		return
	}
	for _, v := range cfg.Vendors {
		if v.ID == c.Param("id") {
			if v.Type == "cli" {
				c.JSON(200, localCLIModels(v))
				return
			}
			secret, e := s.credential(Endpoint{VendorID: v.ID, KeyMode: v.KeyMode, KeyEnv: v.KeyEnv, CredentialID: v.CredentialID})
			if e != nil {
				result(c, nil, e)
				return
			}
			key := catalogKey(v, secret)
			s.catalogMu.Lock()
			old, ok := s.catalog[key]
			s.catalogMu.Unlock()
			if ok && c.Query("refresh") != "1" && time.Since(old.Updated) < 10*time.Minute {
				c.JSON(200, old)
				return
			}
			ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
			defer cancel()
			value, e := fetchModelCatalog(ctx, v, secret, http.DefaultTransport)
			if e != nil {
				if ok {
					old.Stale = true
					old.Error = e.Error()
					c.JSON(200, old)
				} else {
					result(c, nil, e)
				}
				return
			}
			s.catalogMu.Lock()
			if s.catalog == nil {
				s.catalog = map[string]ModelCatalog{}
			}
			if len(s.catalog) >= 100 {
				s.catalog = map[string]ModelCatalog{}
			}
			s.catalog[key] = value
			s.catalogMu.Unlock()
			c.JSON(200, value)
			return
		}
	}
	c.JSON(404, gin.H{"error": "厂商不存在，请先保存厂商"})
}

func fetchModelCatalog(ctx context.Context, v Vendor, secret string, transport http.RoundTripper) (ModelCatalog, error) {
	result := ModelCatalog{Models: []CatalogModel{}, Source: "厂商接口", Updated: time.Now().UTC()}
	if err := validEndpoint(Endpoint{Base: v.Base, KeyMode: "local"}); err != nil {
		return result, err
	}
	base := strings.TrimRight(v.Base, "/") + "/models"
	if v.Kind == "bailian" {
		if !workspacePattern.MatchString(v.Workspace) || !regionPattern.MatchString(v.Region) {
			return result, errors.New("百炼模型目录需要区域与工作空间ID，请编辑厂商补充")
		}
		base = "https://" + v.Workspace + "." + v.Region + ".maas.aliyuncs.com/api/v1/models"
	}
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	seen := map[string]bool{}
	for page := 1; page <= 20; page++ {
		address := base
		if v.Kind == "bailian" {
			address += fmt.Sprintf("?page_no=%d&page_size=100", page)
		}
		req, e := http.NewRequestWithContext(ctx, "GET", address, nil)
		if e != nil {
			return result, errors.New("模型目录地址无效")
		}
		req.Header.Set("Authorization", "Bearer "+secret)
		resp, e := client.Do(req)
		if e != nil {
			return result, errors.New("获取模型列表失败或超时，请检查地址和网络")
		}
		b, e := io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
		resp.Body.Close()
		if e != nil || len(b) > 4<<20 {
			return result, errors.New("模型列表响应过大或不完整")
		}
		if resp.StatusCode != 200 {
			return result, fmt.Errorf("模型列表接口返回 HTTP %d，请检查凭据、权限和区域", resp.StatusCode)
		}
		b = bytes.ReplaceAll(b, []byte(secret), []byte("[REDACTED]"))
		var raw struct {
			Data    []json.RawMessage `json:"data"`
			HasMore bool              `json:"has_more"`
			Output  struct {
				Models []json.RawMessage `json:"models"`
				Total  int               `json:"total"`
			} `json:"output"`
		}
		if json.Unmarshal(b, &raw) != nil {
			return result, errors.New("模型列表响应格式无效")
		}
		rows := raw.Data
		if v.Kind == "bailian" {
			rows = raw.Output.Models
		}
		if rows == nil {
			return result, errors.New("模型列表响应缺少列表字段")
		}
		for _, row := range rows {
			var m map[string]json.RawMessage
			if json.Unmarshal(row, &m) != nil {
				continue
			}
			str := func(k string) string { var x string; _ = json.Unmarshal(m[k], &x); return x }
			id := str("id")
			if id == "" {
				id = str("model")
			}
			if id == "" {
				id = str("model_id")
			}
			if id == "" || len(id) > 300 || strings.ContainsAny(id, "\r\n\x00") || seen[id] {
				continue
			}
			seen[id] = true
			caps := []string{}
			// Both providers expose modality/capability information; ignore unknown values.
			for _, k := range []string{"domain", "capabilities", "modalities"} {
				var values []string
				if json.Unmarshal(m[k], &values) != nil {
					if x := str(k); x != "" {
						values = []string{x}
					}
				}
				for _, value := range values {
					switch strings.ToLower(value) {
					case "tg", "text", "llm", "vlm", "textgeneration":
						caps = append(caps, "text")
					case "ig", "imagegeneration":
						caps = append(caps, "image")
					case "vg", "videogeneration":
						caps = append(caps, "video")
					case "tts", "texttospeech":
						caps = append(caps, "voice")
					case "embedding", "asr", "tr", "rerank":
						caps = append(caps, "other")
					}
				}
			}
			model := describeModel(v, id, str("name"), caps)
			if v.Kind == "bailian" {
				model.Pricing = catalogModelPricing(m["prices"], result.Updated)
			}
			result.Models = append(result.Models, model)
			if len(result.Models) > 2000 {
				return result, errors.New("模型目录超过2000项，请缩小厂商模型范围")
			}
		}
		if v.Kind != "bailian" {
			if raw.HasMore {
				return result, errors.New("此兼容接口需要专用分页适配，请手动填写模型ID")
			}
			break
		}
		if page*100 >= raw.Output.Total {
			break
		}
		if page == 20 {
			return result, errors.New("模型目录分页超过限制")
		}
	}
	sort.Slice(result.Models, func(i, j int) bool { return result.Models[i].ID < result.Models[j].ID })
	return result, nil
}

func (s *Studio) validateCatalogBindings(cfg Settings, selected ...string) error {
	s.catalogMu.Lock()
	defer s.catalogMu.Unlock()
	for _, cap := range capabilities {
		if len(selected) > 0 && !contains(selected, cap) {
			continue
		}
		ep := endpointFor(cfg, cap)
		for _, v := range cfg.Vendors {
			if v.ID != ep.VendorID || v.Type == "cli" || ep.Model == "" {
				continue
			}
			secret, e := s.credential(Endpoint{VendorID: v.ID, KeyMode: v.KeyMode, KeyEnv: v.KeyEnv, CredentialID: v.CredentialID})
			if e != nil {
				continue
			}
			if cat, ok := s.catalog[catalogKey(v, secret)]; ok {
				for _, m := range cat.Models {
					if m.ID == ep.Model && m.Verified && !contains(m.Services, cap) {
						return errors.New("模型目录表明所选模型尚不支持该服务")
					}
				}
			}
		}
	}
	return nil
}
