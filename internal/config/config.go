package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Config struct {
	OutputDir        string   `json:"output_dir"`
	VaultRoot        string   `json:"vault_root"`
	ImageSearchPaths []string `json:"image_search_paths"`
	Settings         struct {
		DefaultTheme    string `json:"default_theme"`
		AutoOpenBrowser bool   `json:"auto_open_browser"`
	} `json:"settings"`
	Cover struct {
		OutputDir             string `json:"output_dir"`
		ImageGenerationScript string `json:"image_generation_script"`
	} `json:"cover"`
}

func Load(root string) (*Config, error) {
	configPath := filepath.Join(root, "config.json")
	b, err := os.ReadFile(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: 读取配置 %s 失败, 使用默认配置 - %v\n", configPath, err)
		return &Config{}, nil
	}
	var cfg Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("解析配置 %s 失败: %w", configPath, err)
	}
	return &cfg, nil
}

func ExpandPath(p string) string {
	if p == "" {
		return ""
	}
	if p[0] == '~' {
		home, _ := os.UserHomeDir()
		if len(p) == 1 {
			return home
		}
		return filepath.Join(home, p[2:])
	}
	return p
}
