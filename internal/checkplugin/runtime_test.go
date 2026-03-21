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
