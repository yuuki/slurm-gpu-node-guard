package gpu

import (
	"context"
	"fmt"
	"strings"

	"github.com/yuuki/slurm-gpu-node-guard/internal/checkplugin"
	"github.com/yuuki/slurm-gpu-node-guard/internal/model"
	"github.com/yuuki/slurm-gpu-node-guard/internal/plugin"
)

const CheckName = "gpu-presence"

type Checker struct {
	Runner checkplugin.CommandRunner
}

func (c Checker) Check(ctx context.Context, _ plugin.Input) model.CheckResult {
	runner := c.Runner
	if runner == nil {
		runner = checkplugin.ExecRunner{}
	}

	gpuList, stderr, err := runner.Run(ctx, "nvidia-smi", "-L")
	if err != nil {
		return errorResult(checkplugin.CommandErrorSummary(stderr, err))
	}

	gpuCount := countGPUs(gpuList)
	if gpuCount == 0 {
		return model.CheckResult{
			CheckName:       CheckName,
			Status:          model.StatusFail,
			FailureDomain:   model.DomainGPU,
			InfraEvidence:   true,
			Summary:         "no GPUs reported by nvidia-smi",
			StructuredCause: "gpu_missing",
			Details: map[string]any{
				"gpu_count":           0,
				"nvlink_down_links":   0,
				"nvswitch_down_count": 0,
				"nvlink_status":       "unknown",
				"nvswitch_status":     "unknown",
			},
		}
	}

	details := map[string]any{
		"gpu_count":           gpuCount,
		"nvlink_down_links":   0,
		"nvswitch_down_count": 0,
		"nvlink_status":       "healthy",
		"nvswitch_status":     "healthy",
	}

	nvlinkOutput, stderr, err := runner.Run(ctx, "nvidia-smi", "nvlink", "--status")
	if err != nil {
		return errorResult(checkplugin.CommandErrorSummary(stderr, err))
	}
	nvlinkState, err := parseNVLinkStatus(nvlinkOutput)
	if err != nil {
		return errorResult(err.Error())
	}
	if nvlinkState.Unsupported {
		details["nvlink_status"] = "unsupported"
	} else {
		details["nvlink_down_links"] = nvlinkState.DownCount
		if nvlinkState.DownCount > 0 {
			return model.CheckResult{
				CheckName:       CheckName,
				Status:          model.StatusFail,
				FailureDomain:   model.DomainGPU,
				InfraEvidence:   true,
				Summary:         fmt.Sprintf("NVLink down on %d link(s)", nvlinkState.DownCount),
				StructuredCause: "nvlink_down",
				Details:         details,
			}
		}
	}

	fabricOutput, stderr, err := runner.Run(ctx, "nvidia-smi", "-q")
	if err != nil {
		return errorResult(checkplugin.CommandErrorSummary(stderr, err))
	}
	fabricState, err := parseFabricStatus(fabricOutput)
	if err != nil {
		return errorResult(err.Error())
	}
	if fabricState.Unsupported {
		details["nvswitch_status"] = "unsupported"
	} else {
		details["nvswitch_down_count"] = fabricState.DownCount
		if fabricState.DownCount > 0 {
			return model.CheckResult{
				CheckName:       CheckName,
				Status:          model.StatusFail,
				FailureDomain:   model.DomainGPU,
				InfraEvidence:   true,
				Summary:         fmt.Sprintf("NVSwitch fabric down on %d section(s)", fabricState.DownCount),
				StructuredCause: "nvswitch_down",
				Details:         details,
			}
		}
	}

	return model.CheckResult{
		CheckName:     CheckName,
		Status:        model.StatusPass,
		FailureDomain: model.DomainGPU,
		Summary:       "GPU fabric healthy",
		Details:       details,
	}
}

type statusSummary struct {
	DownCount   int
	Unsupported bool
}

func parseNVLinkStatus(output string) (statusSummary, error) {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return statusSummary{Unsupported: true}, nil
	}
	lower := strings.ToLower(trimmed)
	if strings.Contains(lower, "not supported") || strings.Contains(lower, "n/a") || strings.Contains(lower, "no nvlink") {
		return statusSummary{Unsupported: true}, nil
	}

	var seenLink bool
	var down int
	for _, line := range strings.Split(output, "\n") {
		lowerLine := strings.ToLower(strings.TrimSpace(line))
		if lowerLine == "" {
			continue
		}
		if !strings.Contains(lowerLine, "link") {
			continue
		}
		seenLink = true
		if strings.Contains(lowerLine, "down") || strings.Contains(lowerLine, "inactive") || strings.Contains(lowerLine, "disabled") {
			down++
		}
	}
	if !seenLink {
		return statusSummary{}, fmt.Errorf("decode nvlink status: no link state found")
	}
	return statusSummary{DownCount: down}, nil
}

func parseFabricStatus(output string) (statusSummary, error) {
	lines := strings.Split(output, "\n")
	type fabricBlock struct {
		state  string
		status string
	}
	var blocks []fabricBlock

	inFabric := false
	fabricIndent := 0
	current := fabricBlock{}

	flush := func() {
		if current.state == "" && current.status == "" {
			return
		}
		blocks = append(blocks, current)
		current = fabricBlock{}
	}

	for _, rawLine := range lines {
		line := strings.ReplaceAll(rawLine, "\t", "    ")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if trimmed == "Fabric" {
			flush()
			inFabric = true
			fabricIndent = indent
			current = fabricBlock{}
			continue
		}
		if !inFabric {
			continue
		}
		if indent <= fabricIndent {
			flush()
			inFabric = false
			continue
		}
		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(lower, "state") {
			current.state = checkplugin.ValueAfterColon(trimmed)
		}
		if strings.HasPrefix(lower, "status") {
			current.status = checkplugin.ValueAfterColon(trimmed)
		}
	}
	flush()

	if len(blocks) == 0 {
		return statusSummary{Unsupported: true}, nil
	}

	var downCount int
	var unsupportedCount int
	for _, block := range blocks {
		state := strings.ToLower(block.state)
		status := strings.ToLower(block.status)
		if state == "" && status == "" {
			continue
		}
		if isUnsupportedState(state) || isUnsupportedState(status) {
			unsupportedCount++
			continue
		}
		if isDownState(state) || isDownState(status) {
			downCount++
		}
	}

	if unsupportedCount == len(blocks) {
		return statusSummary{Unsupported: true}, nil
	}
	return statusSummary{DownCount: downCount}, nil
}

func countGPUs(output string) int {
	count := 0
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "GPU ") {
			count++
		}
	}
	return count
}

func isUnsupportedState(value string) bool {
	return value == "" || value == "n/a" || strings.Contains(value, "not supported")
}

func isDownState(value string) bool {
	return strings.Contains(value, "down") || strings.Contains(value, "inactive") || strings.Contains(value, "disabled") || strings.Contains(value, "failure") || strings.Contains(value, "failed") || strings.Contains(value, "error")
}

func errorResult(summary string) model.CheckResult {
	return model.CheckResult{
		CheckName:       CheckName,
		Status:          model.StatusError,
		FailureDomain:   model.DomainRuntime,
		Summary:         summary,
		StructuredCause: "nvidia_smi_failed",
	}
}
