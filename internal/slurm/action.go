package slurm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/yuuki/slurm-gpu-node-guard/internal/model"
)

var ErrAlreadyDone = errors.New("already applied")

type Executor interface {
	DrainNode(ctx context.Context, nodeName string, reason string) error
	RequeueJob(ctx context.Context, jobID string) error
}

type CommandExecutor struct {
	CommandPath string
}

func ApplyDecision(ctx context.Context, exec Executor, decision model.ActionDecision) error {
	var errs []error
	if decision.ShouldDrain {
		if err := exec.DrainNode(ctx, decision.NodeName, decision.DrainReason); err != nil && !errors.Is(err, ErrAlreadyDone) {
			errs = append(errs, err)
		}
	}
	if decision.ShouldRequeue {
		if err := exec.RequeueJob(ctx, decision.JobID); err != nil && !errors.Is(err, ErrAlreadyDone) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (c CommandExecutor) DrainNode(ctx context.Context, nodeName string, reason string) error {
	_, stderr, err := c.run(ctx, "update", fmt.Sprintf("NodeName=%s", nodeName), "State=DRAIN", fmt.Sprintf("Reason=%s", reason))
	if err != nil {
		return normalizeCommandError(err, stderr)
	}
	return nil
}

func (c CommandExecutor) RequeueJob(ctx context.Context, jobID string) error {
	_, stderr, err := c.run(ctx, "requeue", jobID)
	if err != nil {
		return normalizeCommandError(err, stderr)
	}
	return nil
}

func (c CommandExecutor) run(ctx context.Context, args ...string) (string, string, error) {
	path := c.CommandPath
	if path == "" {
		path = "scontrol"
	}
	cmd := exec.CommandContext(ctx, path, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func normalizeCommandError(err error, stderr string) error {
	lower := strings.ToLower(stderr)
	if strings.Contains(lower, "already") || strings.Contains(lower, "invalid job id specified") {
		return ErrAlreadyDone
	}
	return fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr))
}
