package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Config struct {
	Token        string   `json:"token"`
	Host         string   `json:"host"`
	Port         int      `json:"port"`
	AllowedRoots []string `json:"allowed_roots"`
	LogFile      string   `json:"log_file"`
	ThrottleKbps int      `json:"throttle_kbps"`
	CertFile     string   `json:"cert_file"`
	KeyFile      string   `json:"key_file"`
}

func loadConfig(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("missing config file at %s: copy config.example.json to config.json and set a token before starting the agent", path)
	}

	cfg := &Config{Host: "0.0.0.0", Port: 8765}
	if err := json.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("invalid config file at %s: %w", path, err)
	}

	if cfg.Token == "" {
		return nil, fmt.Errorf("config file at %s has no token set", path)
	}

	if cfg.Host == "" {
		cfg.Host = "0.0.0.0"
	}
	if cfg.Port == 0 {
		cfg.Port = 8765
	}

	return cfg, nil
}

func configPath() string {
	if p := os.Getenv("LANHUB_AGENT_CONFIG"); p != "" {
		return p
	}

	exe, err := os.Executable()
	if err == nil {
		return filepath.Join(filepath.Dir(exe), "config.json")
	}

	return "config.json"
}
