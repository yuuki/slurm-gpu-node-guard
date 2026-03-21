package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yuuki/slurm-gpu-node-guard/internal/model"
)

func TestLoadParsesPolicyAndHooks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.yaml")
	content := `
check_timeouts:
  prolog: 1500ms
  epilog: 3s
domains:
  gpu:
    severity: critical
    require_infra_evidence: true
    drain_reason_template: "gpu unhealthy: {{ .Summary }}"
    on_warn:
      prolog: allow_alert
      epilog: drain_after_job
    on_fail:
      prolog: block_drain_requeue
      epilog: block_drain_requeue
    notification_receivers: ["default"]
notifications:
  receivers:
    default:
      webhook:
        url: "https://example.com/hook"
      command:
        path: "/usr/local/bin/notify"
        args: ["--cluster", "prod"]
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Policy.CheckTimeouts[model.PhaseProlog] != "1500ms" {
		t.Fatalf("unexpected prolog timeout: %+v", cfg.Policy.CheckTimeouts)
	}
	if cfg.Policy.Domains[model.DomainGPU].FailVerdictByPhase[model.PhaseProlog] != model.VerdictBlockDrainRequeue {
		t.Fatalf("unexpected gpu policy: %+v", cfg.Policy.Domains[model.DomainGPU])
	}
	if cfg.Notifications.Receivers["default"].Webhook.URL != "https://example.com/hook" {
		t.Fatalf("unexpected notifications: %+v", cfg.Notifications)
	}
}
