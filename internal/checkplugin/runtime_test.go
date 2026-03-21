package checkplugin

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/yuuki/slurm-gpu-node-guard/internal/model"
	"github.com/yuuki/slurm-gpu-node-guard/internal/plugin"
)

func TestRunWritesStructuredErrorForInvalidRequest(t *testing.T) {
	var stdout bytes.Buffer

	exitCode := Run("guard-plugin-gpu", strings.NewReader("{"), &stdout, func(context.Context, plugin.Input) model.CheckResult {
		t.Fatal("checker must not be called")
		return model.CheckResult{}
	})
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d", exitCode)
	}
	if !strings.Contains(stdout.String(), `"status":"error"`) {
		t.Fatalf("expected structured error output, got %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), `"structured_cause":"invalid_request"`) {
		t.Fatalf("expected invalid_request cause, got %q", stdout.String())
	}
}

func TestRunPassesPluginConfigToChecker(t *testing.T) {
	var stdout bytes.Buffer

	exitCode := Run("guard-plugin-gpu", strings.NewReader(`{
		"phase": "prolog",
		"plugin_config": {
			"required_services": ["slurmd", "munged"],
			"kernel_log_lookback": "5m"
		}
	}`), &stdout, func(_ context.Context, input plugin.Input) model.CheckResult {
		if input.PluginConfig["kernel_log_lookback"] != "5m" {
			t.Fatalf("unexpected plugin config: %+v", input.PluginConfig)
		}
		services, ok := input.PluginConfig["required_services"].([]any)
		if !ok || len(services) != 2 {
			t.Fatalf("unexpected required_services: %+v", input.PluginConfig)
		}
		return model.CheckResult{
			CheckName:     "guard-plugin-gpu",
			Status:        model.StatusPass,
			FailureDomain: model.DomainGPU,
		}
	})
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d", exitCode)
	}
}
