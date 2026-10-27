package main

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
)

type studioConfig struct {
	Server struct {
		Host string `yaml:"host"`
		Port int    `yaml:"port"`
	} `yaml:"server"`
	Frontend struct {
		Port int `yaml:"port"`
	} `yaml:"frontend"`
}

func (c studioConfig) listenAddress() string {
	return net.JoinHostPort(c.Server.Host, strconv.Itoa(c.Server.Port))
}

// loadStudioConfig reads the shared configuration before opening the database.
func loadStudioConfig(path string) (studioConfig, error) {
	var config studioConfig
	config.Frontend.Port = 5173 // Keep existing configurations without frontend compatible.
	b, err := os.ReadFile(path)
	if err != nil {
		return config, fmt.Errorf("读取配置 %s: %w", path, err)
	}
	if err := yaml.UnmarshalWithOptions(b, &config, yaml.Strict()); err != nil {
		return config, fmt.Errorf("解析配置 %s: %w", path, err)
	}
	host := strings.TrimSpace(config.Server.Host)
	if host == "" || strings.ContainsAny(host, "/[] \t\r\n") {
		return config, fmt.Errorf("配置 %s: server.host 须为有效主机名或 IP 地址", path)
	}
	if config.Server.Port < 1 || config.Server.Port > 65535 {
		return config, fmt.Errorf("配置 %s: server.port 须为 1～65535 的整数", path)
	}
	if config.Frontend.Port < 1 || config.Frontend.Port > 65535 {
		return config, fmt.Errorf("配置 %s: frontend.port 须为 1～65535 的整数", path)
	}
	config.Server.Host = host
	return config, nil
}
