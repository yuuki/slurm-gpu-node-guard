package service

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

func TestCheckerPassesWhenRequiredServicesAreHealthy(t *testing.T) {
	checker := Checker{
		Runner: fakeRunner{outputs: map[string]commandOutput{
			"systemctl show slurmd --property=LoadState --property=ActiveState --property=SubState": {
				stdout: "LoadState=loaded\nActiveState=active\nSubState=running\n",
			},
		}},
	}

	result := checker.Check(context.Background(), plugin.Input{
		PluginConfig: map[string]any{"required_services": []string{"slurmd"}},
	})
	if result.Status != model.StatusPass {
		t.Fatalf("expected pass, got %+v", result)
	}
}

func TestCheckerFailsWhenRequiredServiceIsInactive(t *testing.T) {
	checker := Checker{
		Runner: fakeRunner{outputs: map[string]commandOutput{
			"systemctl show munged --property=LoadState --property=ActiveState --property=SubState": {
				stdout: "LoadState=loaded\nActiveState=failed\nSubState=failed\n",
			},
		}},
	}

	result := checker.Check(context.Background(), plugin.Input{
		PluginConfig: map[string]any{"required_services": []string{"munged"}},
	})
	if result.Status != model.StatusFail || result.StructuredCause != "service_unhealthy" || !result.InfraEvidence {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestCheckerWarnsWhenOptionalServiceIsInactive(t *testing.T) {
	checker := Checker{
		Runner: fakeRunner{outputs: map[string]commandOutput{
			"systemctl show nvidia-fabricmanager --property=LoadState --property=ActiveState --property=SubState": {
				stdout: "LoadState=loaded\nActiveState=inactive\nSubState=dead\n",
			},
		}},
	}

	result := checker.Check(context.Background(), plugin.Input{
		PluginConfig: map[string]any{"optional_services": []string{"nvidia-fabricmanager"}},
	})
	if result.Status != model.StatusWarn || result.StructuredCause != "optional_service_unhealthy" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestCheckerReturnsErrorWhenSystemctlFails(t *testing.T) {
	checker := Checker{
		Runner: fakeRunner{outputs: map[string]commandOutput{
			"systemctl show slurmd --property=LoadState --property=ActiveState --property=SubState": {
				stderr: "systemctl failed",
				err:    errors.New("exit status 1"),
			},
		}},
	}

	result := checker.Check(context.Background(), plugin.Input{
		PluginConfig: map[string]any{"required_services": []string{"slurmd"}},
	})
	if result.Status != model.StatusError || result.StructuredCause != "service_check_failed" {
		t.Fatalf("unexpected result: %+v", result)
	}
}
