package plugin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yuuki/slurm-gpu-node-guard/internal/model"
)

func writePlugin(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "plugin.sh")
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestRunnerReturnsJSONResult(t *testing.T) {
	path := writePlugin(t, "#!/bin/sh\ncat >/dev/null\nprintf '%s' '{\"check_name\":\"gpu-presence\",\"status\":\"pass\",\"failure_domain\":\"gpu\",\"summary\":\"ok\"}'\n")

	runner := Runner{}
	result, err := runner.Run(context.Background(), Request{
		Path:  path,
		Phase: model.PhaseProlog,
		Job:   model.JobContext{ID: "123"},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.CheckName != "gpu-presence" || result.Status != model.StatusPass {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestRunnerNonZeroExitBecomesErrorResult(t *testing.T) {
	path := writePlugin(t, "#!/bin/sh\ncat >/dev/null\necho boom >&2\nexit 2\n")

	runner := Runner{}
	result, err := runner.Run(context.Background(), Request{
		Path:  path,
		Phase: model.PhaseProlog,
		Job:   model.JobContext{ID: "123"},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Status != model.StatusError {
		t.Fatalf("expected error status, got %+v", result)
	}
}

func TestRunnerTimeoutBecomesErrorResult(t *testing.T) {
	path := writePlugin(t, "#!/bin/sh\ncat >/dev/null\nsleep 5\n")

	runner := Runner{}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	result, err := runner.Run(ctx, Request{
		Path:  path,
		Phase: model.PhaseProlog,
		Job:   model.JobContext{ID: "123"},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Status != model.StatusError {
		t.Fatalf("expected timeout to map to error, got %+v", result)
	}
}

func TestRunnerIncludesPluginConfigInRequest(t *testing.T) {
	dir := t.TempDir()
	payloadPath := filepath.Join(dir, "payload.json")
	path := writePlugin(t, "#!/bin/sh\ncat >"+payloadPath+"\nprintf '%s' '{\"check_name\":\"capture\",\"status\":\"pass\",\"failure_domain\":\"runtime\"}'\n")

	runner := Runner{}
	result, err := runner.Run(context.Background(), Request{
		Path:         path,
		Phase:        model.PhaseProlog,
		PluginConfig: map[string]any{"required_mounts": []string{"/home"}, "row_remap_fail_threshold": 2},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Status != model.StatusPass {
		t.Fatalf("unexpected result: %+v", result)
	}
	var payload map[string]any
	data, err := os.ReadFile(payloadPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	config, ok := payload["plugin_config"].(map[string]any)
	if !ok {
		t.Fatalf("missing plugin_config in payload: %+v", payload)
	}
	if config["row_remap_fail_threshold"] != float64(2) {
		t.Fatalf("unexpected numeric config: %+v", config)
	}
	mounts, ok := config["required_mounts"].([]any)
	if !ok || len(mounts) != 1 || mounts[0] != "/home" {
		t.Fatalf("unexpected required_mounts: %+v", config)
	}
}
