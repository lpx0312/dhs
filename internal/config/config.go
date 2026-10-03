package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config holds the persistent configuration loaded from file.
// Flag values are passed separately to EffectiveProxy/EffectiveNoColor
// so merge priority stays explicit and testable.
type Config struct {
	Proxy   string `yaml:"proxy"`
	NoColor bool   `yaml:"no_color"`
}

// Load reads a YAML config file. A missing file is not an error:
// a zero-value Config is returned so flag/env fallbacks can apply.
func Load(path string) (*Config, error) {
	cfg := &Config{}
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// EffectiveProxy returns the proxy to use, applying priority:
// flag > file > HTTPS_PROXY env > HTTP_PROXY env > "" (direct).
func (c *Config) EffectiveProxy(flagProxy string, _ string) string {
	if flagProxy != "" {
		return flagProxy
	}
	if c.Proxy != "" {
		return c.Proxy
	}
	if v := os.Getenv("HTTPS_PROXY"); v != "" {
		return v
	}
	if v := os.Getenv("HTTP_PROXY"); v != "" {
		return v
	}
	return ""
}

// EffectiveNoColor returns whether color should be disabled,
// applying priority: flag > file.
func (c *Config) EffectiveNoColor(flagNoColor bool) bool {
	if flagNoColor {
		return true
	}
	return c.NoColor
}

// Save writes cfg to path as YAML, creating parent directories as needed.
// Mode 0600: a proxy URL may embed credentials.
func Save(path string, cfg *Config) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("creating config dir: %w", err)
		}
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("writing config file: %w", err)
	}
	return nil
}

// DefaultConfigPath returns the platform-standard config path:
// os.UserConfigDir() + /dhs/config.yaml.
func DefaultConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "dhs", "config.yaml"), nil
}
