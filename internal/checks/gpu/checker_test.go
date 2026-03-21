package gpu

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

func TestCheckerPassesWhenGPUAndFabricHealthy(t *testing.T) {
	checker := Checker{
		Runner: fakeRunner{outputs: map[string]commandOutput{
			"nvidia-smi -L": {
				stdout: "GPU 0: NVIDIA H100 80GB HBM3 (UUID: GPU-123)\nGPU 1: NVIDIA H100 80GB HBM3 (UUID: GPU-456)\n",
			},
			"nvidia-smi nvlink --status": {
				stdout: "GPU 0:\n    Link 0: Up\n    Link 1: Up\nGPU 1:\n    Link 0: Up\n",
			},
			"nvidia-smi -q": {
				stdout: "GPU 00000000:18:00.0\n    Fabric\n        State                   : Completed\n        Status                  : Success\n",
			},
		}},
	}

	result := checker.Check(context.Background(), plugin.Input{Phase: model.PhaseProlog})
	if result.Status != model.StatusPass {
		t.Fatalf("expected pass, got %+v", result)
	}
	if result.FailureDomain != model.DomainGPU {
		t.Fatalf("unexpected failure domain: %+v", result)
	}
	if result.Details["gpu_count"] != 2 {
		t.Fatalf("unexpected details: %+v", result.Details)
	}
}

func TestCheckerFailsWhenNoGPUIsReported(t *testing.T) {
	checker := Checker{
		Runner: fakeRunner{outputs: map[string]commandOutput{
			"nvidia-smi -L": {stdout: ""},
		}},
	}

	result := checker.Check(context.Background(), plugin.Input{Phase: model.PhaseProlog})
	if result.Status != model.StatusFail || result.StructuredCause != "gpu_missing" || !result.InfraEvidence {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestCheckerFailsWhenNVLinkIsDown(t *testing.T) {
	checker := Checker{
		Runner: fakeRunner{outputs: map[string]commandOutput{
			"nvidia-smi -L": {
				stdout: "GPU 0: NVIDIA H100 80GB HBM3 (UUID: GPU-123)\n",
			},
			"nvidia-smi nvlink --status": {
				stdout: "GPU 0:\n    Link 0: Up\n    Link 1: Down\n",
			},
			"nvidia-smi -q": {
				stdout: "GPU 00000000:18:00.0\n    Fabric\n        State                   : Completed\n        Status                  : Success\n",
			},
		}},
	}

	result := checker.Check(context.Background(), plugin.Input{Phase: model.PhaseProlog})
	if result.Status != model.StatusFail || result.StructuredCause != "nvlink_down" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestCheckerFailsWhenNVSwitchIsDown(t *testing.T) {
	checker := Checker{
		Runner: fakeRunner{outputs: map[string]commandOutput{
			"nvidia-smi -L": {
				stdout: "GPU 0: NVIDIA H100 80GB HBM3 (UUID: GPU-123)\n",
			},
			"nvidia-smi nvlink --status": {
				stdout: "GPU 0:\n    Link 0: Up\n",
			},
			"nvidia-smi -q": {
				stdout: "GPU 00000000:18:00.0\n    Fabric\n        State                   : Inactive\n        Status                  : Failure\n",
			},
		}},
	}

	result := checker.Check(context.Background(), plugin.Input{Phase: model.PhaseProlog})
	if result.Status != model.StatusFail || result.StructuredCause != "nvswitch_down" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestCheckerReturnsErrorWhenNvidiaSMIFails(t *testing.T) {
	checker := Checker{
		Runner: fakeRunner{outputs: map[string]commandOutput{
			"nvidia-smi -L": {stderr: "driver communication failed", err: errors.New("exit status 9")},
		}},
	}

	result := checker.Check(context.Background(), plugin.Input{Phase: model.PhaseProlog})
	if result.Status != model.StatusError || result.StructuredCause != "nvidia_smi_failed" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestCheckerPassesWhenFabricIsUnsupported(t *testing.T) {
	checker := Checker{
		Runner: fakeRunner{outputs: map[string]commandOutput{
			"nvidia-smi -L": {
				stdout: "GPU 0: NVIDIA L40S (UUID: GPU-123)\n",
			},
			"nvidia-smi nvlink --status": {
				stdout: "NVLink not supported on this system\n",
			},
			"nvidia-smi -q": {
				stdout: "GPU 00000000:18:00.0\n    Fabric\n        State                   : N/A\n        Status                  : N/A\n",
			},
		}},
	}

	result := checker.Check(context.Background(), plugin.Input{Phase: model.PhaseProlog})
	if result.Status != model.StatusPass {
		t.Fatalf("expected pass, got %+v", result)
	}
	if result.Details["nvlink_status"] != "unsupported" {
		t.Fatalf("unexpected nvlink details: %+v", result.Details)
	}
	if result.Details["nvswitch_status"] != "unsupported" {
		t.Fatalf("unexpected nvswitch details: %+v", result.Details)
	}
}
