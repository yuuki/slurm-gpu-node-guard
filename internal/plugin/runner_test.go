package plugin

import (
	"context"
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
