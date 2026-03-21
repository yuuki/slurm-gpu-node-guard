package rdma

import (
	"context"
	"errors"
	"testing"

	"github.com/yuuki/slurm-gpu-node-guard/internal/model"
	"github.com/yuuki/slurm-gpu-node-guard/internal/plugin"
)

type fakeRunner struct {
	output commandOutput
}

type commandOutput struct {
	stdout string
	stderr string
	err    error
}

func (f fakeRunner) Run(_ context.Context, name string, args ...string) (string, string, error) {
	if name != "ibstat" {
		return "", "", errors.New("unexpected command")
	}
	if len(args) != 0 {
		return "", "", errors.New("unexpected args")
	}
	return f.output.stdout, f.output.stderr, f.output.err
}

func TestCheckerPassesWhenAllPortsAreHealthy(t *testing.T) {
	checker := Checker{
		Runner: fakeRunner{output: commandOutput{
			stdout: "CA 'mlx5_0'\n\tCA type: MT4129\n\tNumber of ports: 2\n\tPort 1:\n\t\tState: Active\n\t\tPhysical state: LinkUp\n\tPort 2:\n\t\tState: Active\n\t\tPhysical state: LinkUp\n",
		}},
	}

	result := checker.Check(context.Background(), plugin.Input{Phase: model.PhaseProlog})
	if result.Status != model.StatusPass {
		t.Fatalf("expected pass, got %+v", result)
	}
	if result.Details["active_ports"] != 2 {
		t.Fatalf("unexpected details: %+v", result.Details)
	}
}

func TestCheckerWarnsWhenSomePortsAreDegraded(t *testing.T) {
	checker := Checker{
		Runner: fakeRunner{output: commandOutput{
			stdout: "CA 'mlx5_0'\n\tPort 1:\n\t\tState: Active\n\t\tPhysical state: LinkUp\n\tPort 2:\n\t\tState: Down\n\t\tPhysical state: Disabled\n",
		}},
	}

	result := checker.Check(context.Background(), plugin.Input{Phase: model.PhaseProlog})
	if result.Status != model.StatusWarn || result.StructuredCause != "rdma_partial_degradation" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestCheckerFailsWhenAllPortsAreDown(t *testing.T) {
	checker := Checker{
		Runner: fakeRunner{output: commandOutput{
			stdout: "CA 'mlx5_0'\n\tPort 1:\n\t\tState: Down\n\t\tPhysical state: Disabled\n\tPort 2:\n\t\tState: Down\n\t\tPhysical state: Polling\n",
		}},
	}

	result := checker.Check(context.Background(), plugin.Input{Phase: model.PhaseProlog})
	if result.Status != model.StatusFail || result.StructuredCause != "rdma_link_down" || !result.InfraEvidence {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestCheckerReturnsErrorWhenIbstatFails(t *testing.T) {
	checker := Checker{
		Runner: fakeRunner{output: commandOutput{
			stderr: "ibstat: command failed",
			err:    errors.New("exit status 1"),
		}},
	}

	result := checker.Check(context.Background(), plugin.Input{Phase: model.PhaseProlog})
	if result.Status != model.StatusError || result.StructuredCause != "ibstat_failed" {
		t.Fatalf("unexpected result: %+v", result)
	}
}
