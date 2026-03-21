package gpuerrors

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

func TestCheckerFailsOnXIDByDefault(t *testing.T) {
	checker := Checker{
		Runner: fakeRunner{outputs: map[string]commandOutput{
			"nvidia-smi -q -x": {
				stdout: `<nvidia_smi_log><gpu><ecc_errors><aggregate><uncorrected>0</uncorrected></aggregate></ecc_errors><row_remapper><pending_rows>0</pending_rows><failure>no</failure></row_remapper></gpu></nvidia_smi_log>`,
			},
			"journalctl -k --since -5m --no-pager": {
				stdout: "NVRM: Xid (PCI:0000:17:00): 79, GPU has fallen off the bus.\n",
			},
		}},
	}

	result := checker.Check(context.Background(), plugin.Input{})
	if result.Status != model.StatusFail || result.StructuredCause != "pcie_link_error" || result.FailureDomain != model.DomainInterconnect {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestCheckerFailsOnUncorrectableECC(t *testing.T) {
	checker := Checker{
		Runner: fakeRunner{outputs: map[string]commandOutput{
			"nvidia-smi -q -x": {
				stdout: `<nvidia_smi_log><gpu><ecc_errors><aggregate><uncorrectable_sram>2</uncorrectable_sram></aggregate></ecc_errors><row_remapper><pending_rows>0</pending_rows><failure>no</failure></row_remapper></gpu></nvidia_smi_log>`,
			},
			"journalctl -k --since -5m --no-pager": {stdout: ""},
		}},
	}

	result := checker.Check(context.Background(), plugin.Input{})
	if result.Status != model.StatusFail || result.StructuredCause != "ecc_uncorrectable" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestCheckerFailsOnRowRemapThresholdExceeded(t *testing.T) {
	checker := Checker{
		Runner: fakeRunner{outputs: map[string]commandOutput{
			"nvidia-smi -q -x": {
				stdout: `<nvidia_smi_log><gpu><ecc_errors><aggregate><uncorrected>0</uncorrected></aggregate></ecc_errors><row_remapper><pending_rows>3</pending_rows><failure>no</failure></row_remapper></gpu></nvidia_smi_log>`,
			},
			"journalctl -k --since -5m --no-pager": {stdout: ""},
		}},
	}

	result := checker.Check(context.Background(), plugin.Input{
		PluginConfig: map[string]any{"row_remap_fail_threshold": 2},
	})
	if result.Status != model.StatusFail || result.StructuredCause != "row_remap_failed" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestCheckerWarnsOnConfiguredWarnXID(t *testing.T) {
	checker := Checker{
		Runner: fakeRunner{outputs: map[string]commandOutput{
			"nvidia-smi -q -x": {
				stdout: `<nvidia_smi_log><gpu><ecc_errors><aggregate><uncorrected>0</uncorrected></aggregate></ecc_errors><row_remapper><pending_rows>0</pending_rows><failure>no</failure></row_remapper></gpu></nvidia_smi_log>`,
			},
			"journalctl -k --since -5m --no-pager": {
				stdout: "NVRM: Xid (PCI:0000:17:00): 31, Ch 00000001\n",
			},
		}},
	}

	result := checker.Check(context.Background(), plugin.Input{
		PluginConfig: map[string]any{"xid_warn_codes": []int{31}},
	})
	if result.Status != model.StatusWarn || result.StructuredCause != "gpu_xid_warn" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestCheckerReturnsErrorWhenNvidiaSMIFails(t *testing.T) {
	checker := Checker{
		Runner: fakeRunner{outputs: map[string]commandOutput{
			"nvidia-smi -q -x": {stderr: "nvidia-smi failed", err: errors.New("exit status 1")},
		}},
	}

	result := checker.Check(context.Background(), plugin.Input{})
	if result.Status != model.StatusError || result.StructuredCause != "gpu_error_check_failed" {
		t.Fatalf("unexpected result: %+v", result)
	}
}
