package rdma

import (
	"context"
	"fmt"
	"strings"

	"github.com/yuuki/slurm-gpu-node-guard/internal/checkplugin"
	"github.com/yuuki/slurm-gpu-node-guard/internal/model"
	"github.com/yuuki/slurm-gpu-node-guard/internal/plugin"
)

const CheckName = "rdma-link"

type Checker struct {
	Runner checkplugin.CommandRunner
}

func (c Checker) Check(ctx context.Context, _ plugin.Input) model.CheckResult {
	runner := c.Runner
	if runner == nil {
		runner = checkplugin.ExecRunner{}
	}

	output, stderr, err := runner.Run(ctx, "ibstat")
	if err != nil {
		return model.CheckResult{
			CheckName:       CheckName,
			Status:          model.StatusError,
			FailureDomain:   model.DomainRuntime,
			Summary:         checkplugin.CommandErrorSummary(stderr, err),
			StructuredCause: "ibstat_failed",
		}
	}

	summary, err := parseIBStat(output)
	if err != nil {
		return model.CheckResult{
			CheckName:       CheckName,
			Status:          model.StatusError,
			FailureDomain:   model.DomainRuntime,
			Summary:         err.Error(),
			StructuredCause: "ibstat_failed",
		}
	}

	details := map[string]any{
		"device_count":   summary.deviceCount,
		"active_ports":   summary.activePorts,
		"degraded_ports": summary.degradedPorts,
	}

	switch {
	case summary.activePorts > 0 && summary.degradedPorts == 0:
		return model.CheckResult{
			CheckName:     CheckName,
			Status:        model.StatusPass,
			FailureDomain: model.DomainRDMA,
			Summary:       "RDMA ports healthy",
			Details:       details,
		}
	case summary.activePorts > 0:
		return model.CheckResult{
			CheckName:       CheckName,
			Status:          model.StatusWarn,
			FailureDomain:   model.DomainRDMA,
			InfraEvidence:   true,
			Summary:         fmt.Sprintf("RDMA partially degraded: %d active, %d degraded", summary.activePorts, summary.degradedPorts),
			StructuredCause: "rdma_partial_degradation",
			Details:         details,
		}
	default:
		return model.CheckResult{
			CheckName:       CheckName,
			Status:          model.StatusFail,
			FailureDomain:   model.DomainRDMA,
			InfraEvidence:   true,
			Summary:         "RDMA link down on all ports",
			StructuredCause: "rdma_link_down",
			Details:         details,
		}
	}
}

type ibstatSummary struct {
	deviceCount   int
	activePorts   int
	degradedPorts int
}

type portStatus struct {
	state    string
	physical string
}

func parseIBStat(output string) (ibstatSummary, error) {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return ibstatSummary{}, fmt.Errorf("decode ibstat output: empty output")
	}
	lower := strings.ToLower(trimmed)
	if strings.Contains(lower, "no ib devices found") || strings.Contains(lower, "no infiniband adapters found") {
		return ibstatSummary{}, nil
	}

	var summary ibstatSummary
	var current *portStatus
	parsedPort := false

	finalize := func() {
		if current == nil {
			return
		}
		parsedPort = true
		if isActivePort(*current) {
			summary.activePorts++
		} else {
			summary.degradedPorts++
		}
		current = nil
	}

	for _, rawLine := range strings.Split(output, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		lowerLine := strings.ToLower(line)
		switch {
		case strings.HasPrefix(line, "CA '"):
			summary.deviceCount++
		case strings.HasPrefix(lowerLine, "port "):
			finalize()
			current = &portStatus{}
		case strings.HasPrefix(lowerLine, "state:"):
			if current == nil {
				current = &portStatus{}
			}
			current.state = checkplugin.ValueAfterColon(line)
		case strings.HasPrefix(lowerLine, "physical state:"):
			if current == nil {
				current = &portStatus{}
			}
			current.physical = checkplugin.ValueAfterColon(line)
		}
	}
	finalize()

	if !parsedPort && summary.deviceCount == 0 {
		return ibstatSummary{}, fmt.Errorf("decode ibstat output: no CA or port information found")
	}
	return summary, nil
}

func isActivePort(port portStatus) bool {
	return strings.EqualFold(strings.TrimSpace(port.state), "active") &&
		(strings.EqualFold(strings.TrimSpace(port.physical), "linkup") || strings.EqualFold(strings.TrimSpace(port.physical), "active"))
}

