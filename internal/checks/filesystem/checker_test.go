package filesystem

import (
	"context"
	"errors"
	"testing"

	"github.com/yuuki/slurm-gpu-node-guard/internal/model"
	"github.com/yuuki/slurm-gpu-node-guard/internal/plugin"
)

type fakeRunner struct {
	outputs map[string]commandOutput
}

type commandOutput struct {
	stdout string
	stderr string
	err    error
}

func (f fakeRunner) Run(_ context.Context, name string, args ...string) (string, string, error) {
	key := name + " " + joinArgs(args)
	output, ok := f.outputs[key]
	if !ok {
		return "", "", errors.New("unexpected command: " + key)
	}
	return output.stdout, output.stderr, output.err
}

func joinArgs(args []string) string {
	if len(args) == 0 {
		return ""
	}
	value := args[0]
	for _, arg := range args[1:] {
		value += " " + arg
	}
	return value
}

func TestCheckerPassesWhenMountsAndLogsAreHealthy(t *testing.T) {
	checker := Checker{
		Runner: fakeRunner{outputs: map[string]commandOutput{
			"findmnt -rn -M /home -o TARGET,OPTIONS": {stdout: "/home rw,relatime\n"},
			"stat /home/user":                        {stdout: "ok\n"},
			"journalctl -k --since -5m --no-pager":   {stdout: ""},
		}},
	}

	result := checker.Check(context.Background(), plugin.Input{
		Phase: model.PhaseProlog,
		PluginConfig: map[string]any{
			"required_mounts":     []string{"/home"},
			"mount_probe_paths":   []string{"/home/user"},
			"kernel_log_lookback": "5m",
		},
	})
	if result.Status != model.StatusPass {
		t.Fatalf("expected pass, got %+v", result)
	}
}

func TestCheckerFailsWhenRequiredMountIsMissing(t *testing.T) {
	checker := Checker{
		Runner: fakeRunner{outputs: map[string]commandOutput{
			"findmnt -rn -M /datasets -o TARGET,OPTIONS": {stderr: "not found", err: errors.New("exit status 1")},
			"journalctl -k --since -5m --no-pager":       {stdout: ""},
		}},
	}

	result := checker.Check(context.Background(), plugin.Input{
		PluginConfig: map[string]any{
			"required_mounts": []string{"/datasets"},
		},
	})
	if result.Status != model.StatusFail || result.StructuredCause != "mount_missing" || !result.InfraEvidence {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestCheckerFailsWhenBlockIOErrorAppearsInKernelLog(t *testing.T) {
	checker := Checker{
		Runner: fakeRunner{outputs: map[string]commandOutput{
			"journalctl -k --since -5m --no-pager": {
				stdout: "blk_update_request: I/O error, dev nvme0n1, sector 42\n",
			},
		}},
	}

	result := checker.Check(context.Background(), plugin.Input{})
	if result.Status != model.StatusFail || result.StructuredCause != "block_io_error" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestCheckerReturnsErrorWhenJournalctlFails(t *testing.T) {
	checker := Checker{
		Runner: fakeRunner{outputs: map[string]commandOutput{
			"journalctl -k --since -5m --no-pager": {stderr: "journalctl: failed", err: errors.New("exit status 1")},
		}},
	}

	result := checker.Check(context.Background(), plugin.Input{})
	if result.Status != model.StatusError || result.StructuredCause != "filesystem_check_failed" {
		t.Fatalf("unexpected result: %+v", result)
	}
}
