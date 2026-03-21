package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/yuuki/slurm-gpu-node-guard/internal/model"
	"github.com/yuuki/slurm-gpu-node-guard/internal/notify"
	"github.com/yuuki/slurm-gpu-node-guard/internal/policy"
)

const defaultSocketPath = "/tmp/slurm-gpu-node-guard.sock"

// Config is the top-level configuration loaded from a YAML policy file.
type Config struct {
	SocketPath    string             `yaml:"socket_path"`
	Plugins       []model.PluginSpec `yaml:"plugins"`
	Policy        policy.Policy
	Notifications notify.Config `yaml:"notifications"`
}

type rawConfig struct {
	SocketPath    string                                      `yaml:"socket_path"`
	Plugins       []model.PluginSpec                          `yaml:"plugins"`
	CheckTimeouts map[model.Phase]string                      `yaml:"check_timeouts"`
	Domains       map[model.FailureDomain]policy.DomainPolicy `yaml:"domains"`
	Notifications notify.Config                               `yaml:"notifications"`
}

// Load reads and parses a YAML configuration file from the given path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var raw rawConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	cfg := &Config{
		SocketPath: raw.SocketPath,
		Plugins:    raw.Plugins,
		Policy: policy.Policy{
			CheckTimeouts: raw.CheckTimeouts,
			Domains:       raw.Domains,
		},
		Notifications: raw.Notifications,
	}
	if cfg.SocketPath == "" {
		cfg.SocketPath = defaultSocketPath
	}
	return cfg, nil
}

// PhaseTimeout returns the configured timeout for the given phase, or fallback if unset or invalid.
func (c *Config) PhaseTimeout(phase model.Phase, fallback time.Duration) time.Duration {
	raw := c.Policy.CheckTimeouts[phase]
	if raw == "" {
		return fallback
	}
	timeout, err := time.ParseDuration(raw)
	if err != nil {
		return fallback
	}
	return timeout
}
