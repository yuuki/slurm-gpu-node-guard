package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/yuuki/slurm-gpu-node-guard/internal/model"
)

// Runner executes external check plugins as child processes.
type Runner struct{}

// Request contains the parameters for running an external plugin.
type Request struct {
	Path         string
	Name         string
	Phase        model.Phase
	Job          model.JobContext
	Node         model.NodeContext
	Timeouts     map[string]string
	PluginConfig map[string]any
}

// Input is the JSON payload sent to external plugins on stdin.
type Input struct {
	Phase        model.Phase       `json:"phase"`
	Job          model.JobContext  `json:"job_context"`
	Node         model.NodeContext `json:"node_context"`
	Timeouts     map[string]string `json:"timeouts,omitempty"`
	PluginConfig map[string]any    `json:"plugin_config,omitempty"`
}

// Run executes the plugin binary, passing a JSON request on stdin and decoding the JSON result from stdout.
func (Runner) Run(ctx context.Context, req Request) (model.CheckResult, error) {
	payload, err := json.Marshal(Input{
		Phase:        req.Phase,
		Job:          req.Job,
		Node:         req.Node,
		Timeouts:     req.Timeouts,
		PluginConfig: req.PluginConfig,
	})
	if err != nil {
		return errorResult(req, fmt.Sprintf("marshal plugin input: %v", err)), nil
	}

	cmd := exec.CommandContext(ctx, req.Path)
	cmd.Stdin = bytes.NewReader(payload)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		summary := strings.TrimSpace(stderr.String())
		if ctx.Err() != nil {
			summary = nonEmpty(summary, ctx.Err().Error())
		}
		if summary == "" {
			summary = err.Error()
		}
		return errorResult(req, summary), nil
	}

	var result model.CheckResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return errorResult(req, fmt.Sprintf("decode plugin output: %v", err)), nil
	}
	if result.CheckName == "" {
		result.CheckName = defaultCheckName(req)
	}
	if result.FailureDomain == "" {
		result.FailureDomain = model.DomainUnknown
	}
	if result.Status == "" {
		result.Status = model.StatusError
	}
	return result, nil
}

func errorResult(req Request, summary string) model.CheckResult {
	return model.CheckResult{
		CheckName:     defaultCheckName(req),
		Status:        model.StatusError,
		FailureDomain: model.DomainRuntime,
		Summary:       summary,
	}
}

func defaultCheckName(req Request) string {
	if req.Name != "" {
		return req.Name
	}
	return filepath.Base(req.Path)
}

func nonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
