package checkplugin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/yuuki/slurm-gpu-node-guard/internal/model"
	"github.com/yuuki/slurm-gpu-node-guard/internal/plugin"
)

type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) (stdout string, stderr string, err error)
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout strings.Builder
	var stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func Run(checkName string, stdin io.Reader, stdout io.Writer, checker func(context.Context, plugin.Input) model.CheckResult) int {
	var req plugin.Input
	if err := json.NewDecoder(stdin).Decode(&req); err != nil {
		return writeResult(stdout, model.CheckResult{
			CheckName:       checkName,
			Status:          model.StatusError,
			FailureDomain:   model.DomainRuntime,
			Summary:         fmt.Sprintf("decode plugin input: %v", err),
			StructuredCause: "invalid_request",
		})
	}

	ctx := context.Background()
	if timeout := requestTimeout(req); timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	return writeResult(stdout, checker(ctx, req))
}

func requestTimeout(req plugin.Input) time.Duration {
	if req.Timeouts == nil {
		return 0
	}
	raw := req.Timeouts[string(req.Phase)]
	if raw == "" {
		return 0
	}
	timeout, err := time.ParseDuration(raw)
	if err != nil || timeout <= 0 {
		return 0
	}
	return timeout
}

func writeResult(stdout io.Writer, result model.CheckResult) int {
	if result.CheckName == "" {
		result.CheckName = "unknown"
	}
	if result.Status == "" {
		result.Status = model.StatusError
	}
	if result.FailureDomain == "" {
		result.FailureDomain = model.DomainRuntime
	}
	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		return 1
	}
	return 0
}

func CommandErrorSummary(stderr string, err error) string {
	stderr = strings.TrimSpace(stderr)
	if stderr != "" {
		return stderr
	}
	if err == nil {
		return ""
	}
	return err.Error()
}
