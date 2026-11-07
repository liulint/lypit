package main

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func endpointFor(v Settings, capability string) Endpoint {
	switch capability {
	case "text":
		return v.Text
	case "image":
		return v.Image
	case "video":
		return v.Video
	case "voice":
		return v.Voice
	}
	return Endpoint{}
}
func setEndpoint(v *Settings, capability string, ep Endpoint) {
	switch capability {
	case "text":
		v.Text = ep
	case "image":
		v.Image = ep
	case "video":
		v.Video = ep
	case "voice":
		v.Voice = ep
	}
}

var capabilities = []string{"text", "image", "video", "voice"}

// Upgrade the old endpoint layout in memory. Existing task snapshots retain their exact old values.
func withVendorRegistry(v Settings) Settings {
	if v.RegistryVersion != 0 || len(v.Vendors) > 0 {
		return v
	}
	v.RegistryVersion = 1
	v.Vendors = []Vendor{}
	names := map[string]string{"text": "文本服务", "image": "图片服务", "video": "视频服务", "voice": "旁白服务"}
	for _, cap := range capabilities {
		ep := endpointFor(v, cap)
		protocol := "openai"
		if cap == "video" {
			protocol = "ark"
		}
		id := "legacy-" + cap
		for _, vendor := range v.Vendors {
			if vendor.Base == ep.Base && vendor.KeyEnv == ep.KeyEnv && vendor.Protocol == protocol {
				id = vendor.ID
				break
			}
		}
		if id == "legacy-"+cap {
			v.Vendors = append(v.Vendors, Vendor{ID: id, Name: names[cap] + "（原配置）", Type: "api", Protocol: protocol, Base: ep.Base, KeyEnv: ep.KeyEnv})
		}
		ep.VendorID = id
		setEndpoint(&v, cap, ep)
	}
	return v
}
func vendorCapabilities(v Vendor) []string {
	if v.Type == "cli" {
		if v.CLI == "codex" {
			return []string{"text", "image"}
		}
		return []string{"text"}
	}
	switch v.Kind {
	case "ark":
		return []string{"text", "video"}
	case "bailian":
		return []string{"text"}
	}
	// Preserve old task snapshots and explicitly configured compatible transports.
	if v.Protocol == "ark" {
		return []string{"video"}
	}
	return []string{"text", "image", "voice"}
}
func supports(v Vendor, cap string) bool {
	for _, c := range vendorCapabilities(v) {
		if c == cap {
			return true
		}
	}
	return false
}
func resolveEndpoint(v Settings, cap string) (Endpoint, error) {
	ep := endpointFor(v, cap)
	if ep.VendorID == "" {
		if v.RegistryVersion == 0 {
			return ep, nil
		}
		return ep, fmt.Errorf("请先为%s绑定厂商", cap)
	}
	for _, vendor := range v.Vendors {
		if vendor.ID != ep.VendorID {
			continue
		}
		if !supports(vendor, cap) || (vendor.Kind != "" && !modelAllows(vendor, ep.Model, cap)) {
			return ep, errors.New("所选厂商不支持该制作能力")
		}
		return Endpoint{VendorID: vendor.ID, Transport: vendor.Type, CLI: vendor.CLI, Executable: vendor.Executable, Base: vendor.Base, KeyEnv: vendor.KeyEnv, KeyMode: vendor.KeyMode, CredentialID: vendor.CredentialID, Model: ep.Model}, nil
	}
	return ep, errors.New("绑定的厂商不存在，请重新选择")
}
func validateRegistry(v *Settings) error {
	if v.RegistryVersion != 1 {
		return errors.New("厂商配置格式不受支持，请刷新页面")
	}
	if len(v.Vendors) > 50 {
		return errors.New("厂商数量不能超过50个")
	}
	seen := map[string]bool{}
	for i := range v.Vendors {
		p := &v.Vendors[i]
		p.Name = strings.TrimSpace(p.Name)
		if p.Kind != "" && p.Kind != "codex" && p.Kind != "claude" && p.Kind != "ark" && p.Kind != "bailian" && p.Kind != "custom" {
			return errors.New("未知厂商类型")
		}
		if p.Kind == "codex" || p.Kind == "claude" {
			if p.Type != "cli" || p.CLI != p.Kind {
				return errors.New("厂商与CLI类型不一致")
			}
		}
		if p.Kind == "ark" && (p.Type != "api" || p.Protocol != "ark") {
			return errors.New("方舟连接协议不一致")
		}
		if (p.Kind == "bailian" || p.Kind == "custom") && (p.Type != "api" || p.Protocol != "openai") {
			return errors.New("厂商连接协议不一致")
		}
		if p.Workspace != "" && !workspacePattern.MatchString(p.Workspace) {
			return errors.New("工作空间ID只能包含字母、数字和连字符")
		}
		if p.Region != "" && !regionPattern.MatchString(p.Region) {
			return errors.New("区域无效")
		}
		if p.ID == "" || len(p.ID) > 100 || seen[p.ID] || p.Name == "" || len([]rune(p.Name)) > 60 {
			return errors.New("厂商标识重复或名称无效")
		}
		seen[p.ID] = true
		switch p.Type {
		case "api":
			if p.Protocol != "openai" && p.Protocol != "ark" {
				return errors.New("不支持的远程API协议")
			}
			if p.KeyMode != "" && p.KeyMode != "local" && p.KeyMode != "env" {
				return errors.New("请选择本机加密保存或环境变量")
			}
			if p.KeyMode == "local" {
				p.KeyEnv = ""
			}
			if e := validEndpoint(Endpoint{Base: p.Base, KeyEnv: p.KeyEnv, KeyMode: p.KeyMode}); e != nil {
				return e
			}
			p.CLI = ""
			p.Executable = ""
		case "cli":
			if p.CLI != "codex" && p.CLI != "claude" {
				return errors.New("本地CLI当前支持Codex或Claude Code")
			}
			if p.Executable != "" && (!filepath.IsAbs(p.Executable) || strings.ContainsAny(p.Executable, "\n\r\x00") || len(p.Executable) > 4096) {
				return errors.New("CLI路径必须为绝对可执行文件路径，不支持命令参数")
			}
			p.Protocol = ""
			p.Base = ""
			p.KeyEnv = ""
			p.KeyMode = ""
			p.CredentialID = ""
		default:
			return errors.New("厂商类型请选择本地CLI或远程API")
		}
	}
	for _, cap := range capabilities {
		ep := endpointFor(*v, cap)
		if len(ep.Model) > 300 || strings.ContainsAny(ep.Model, "\n\r\x00") {
			return errors.New("模型ID无效")
		}
		clean := Endpoint{VendorID: ep.VendorID, Model: ep.Model}
		setEndpoint(v, cap, clean)
		if clean.VendorID != "" {
			if _, e := resolveEndpoint(*v, cap); e != nil {
				return e
			}
		}
	}
	return nil
}
func cliExecutable(ep Endpoint) (string, error) {
	if ep.CLI != "codex" && ep.CLI != "claude" {
		return "", errors.New("未知CLI适配器")
	}
	name := ep.Executable
	if name == "" {
		name = ep.CLI
	}
	var path string
	var e error
	if ep.Executable == "" {
		path, e = findLocalExecutable(name)
	} else {
		path, e = exec.LookPath(name)
	}
	if e != nil {
		return "", errors.New("CLI未安装或自定义路径不可执行")
	}
	if _, _, e := cliProgram(path, ep.CLI, runtime.GOOS, findLocalExecutable); e != nil {
		return "", e
	}
	return path, nil
}
func (s *Studio) vendorStatus(v Vendor) map[string]any {
	if v.Type == "api" {
		_, err := s.credential(Endpoint{VendorID: v.ID, KeyEnv: v.KeyEnv, KeyMode: v.KeyMode, CredentialID: v.CredentialID})
		return map[string]any{"ready": err == nil, "message": "仅检查本机密钥是否可读取，未调用模型"}
	}
	_, e := cliExecutable(Endpoint{CLI: v.CLI, Executable: v.Executable})
	msg := "可执行文件已找到；登录、模型权限与费用待实际制作验证"
	if e != nil {
		msg = e.Error()
	}
	return map[string]any{"ready": e == nil, "message": msg}
}
