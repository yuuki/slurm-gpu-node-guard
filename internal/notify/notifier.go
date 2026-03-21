package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"

	"github.com/yuuki/slurm-gpu-node-guard/internal/model"
)

type Config struct {
	Receivers map[string]Receiver `yaml:"receivers"`
}

type Receiver struct {
	Webhook WebhookConfig `yaml:"webhook"`
	Command CommandConfig `yaml:"command"`
}

type WebhookConfig struct {
	URL string `yaml:"url"`
}

type CommandConfig struct {
	Path string   `yaml:"path"`
	Args []string `yaml:"args"`
}

type Manager struct {
	cfg    Config
	client *http.Client
	extra  map[string]string
}

func NewManager(cfg Config, extraEnv map[string]string) *Manager {
	return &Manager{
		cfg:    cfg,
		client: http.DefaultClient,
		extra:  extraEnv,
	}
}

func (m *Manager) Notify(ctx context.Context, event model.NotificationEvent) error {
	if len(event.ReceiverNames) == 0 {
		return nil
	}

	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal notification event: %w", err)
	}

	var errs []error
	for _, name := range event.ReceiverNames {
		receiver, ok := m.cfg.Receivers[name]
		if !ok {
			errs = append(errs, fmt.Errorf("receiver %q not found", name))
			continue
		}
		if receiver.Webhook.URL != "" {
			if err := m.sendWebhook(ctx, receiver.Webhook.URL, body); err != nil {
				errs = append(errs, err)
			}
		}
		if receiver.Command.Path != "" {
			if err := m.runCommand(ctx, receiver.Command, body); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func (m *Manager) sendWebhook(ctx context.Context, url string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("post webhook: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}
	return nil
}

func (m *Manager) runCommand(ctx context.Context, cfg CommandConfig, body []byte) error {
	cmd := exec.CommandContext(ctx, cfg.Path, cfg.Args...)
	cmd.Stdin = bytes.NewReader(body)
	cmd.Env = append(os.Environ(), flattenEnv(m.extra)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("run notification command: %w: %s", err, bytes.TrimSpace(output))
	}
	return nil
}

func flattenEnv(env map[string]string) []string {
	items := make([]string, 0, len(env))
	for key, value := range env {
		items = append(items, key+"="+value)
	}
	return items
}
