package filesystem

import (
	"context"
	"fmt"
	"strings"

	"github.com/yuuki/slurm-gpu-node-guard/internal/checkplugin"
	"github.com/yuuki/slurm-gpu-node-guard/internal/model"
	"github.com/yuuki/slurm-gpu-node-guard/internal/plugin"
)

// CheckName is the identifier for the filesystem health check plugin.
const CheckName = "filesystem-health"

const defaultKernelLogLookback = "5m"

var defaultBlockErrorPatterns = []string{
	"blk_update_request",
	"buffer i/o error",
	"i/o error",
	"critical medium error",
	"ext4-fs error",
	"xfs error",
	"xfs_alert",
	"xfs_buf_ioerror",
}

// Config configures filesystem checks.
type Config struct {
	RequiredMounts     []string `json:"required_mounts"`
	MountProbePaths    []string `json:"mount_probe_paths"`
	KernelLogLookback  string   `json:"kernel_log_lookback"`
	BlockErrorPatterns []string `json:"block_error_patterns"`
}

// Checker verifies mount presence, mount health, probe-path accessibility, and kernel block I/O errors.
type Checker struct {
	Runner checkplugin.CommandRunner
}

// Check runs filesystem health checks and returns a CheckResult indicating pass, fail, or error.
func (c Checker) Check(ctx context.Context, input plugin.Input) model.CheckResult {
	runner := c.Runner
	if runner == nil {
		runner = checkplugin.ExecRunner{}
	}

	cfg, err := checkplugin.DecodeConfig[Config](input.PluginConfig)
	if err != nil {
		return errorResult(err.Error())
	}
	if cfg.KernelLogLookback == "" {
		cfg.KernelLogLookback = defaultKernelLogLookback
	}
	if len(cfg.BlockErrorPatterns) == 0 {
		cfg.BlockErrorPatterns = append([]string(nil), defaultBlockErrorPatterns...)
	}

	missingMounts := make([]string, 0)
	readOnlyMounts := make([]string, 0)
	probeFailures := make([]string, 0)
	matchedLogLines := make([]string, 0)

	for _, mount := range cfg.RequiredMounts {
		output, stderr, err := runner.Run(ctx, "findmnt", "-rn", "-M", mount, "-o", "TARGET,OPTIONS")
		if err != nil {
			if strings.TrimSpace(output) == "" {
				missingMounts = append(missingMounts, mount)
				continue
			}
			return errorResult(checkplugin.CommandErrorSummary(stderr, err))
		}
		if isReadOnlyMount(output) {
			readOnlyMounts = append(readOnlyMounts, mount)
		}
	}

	for _, path := range cfg.MountProbePaths {
		_, stderr, err := runner.Run(ctx, "stat", path)
		if err != nil {
			if stderr == "" {
				stderr = err.Error()
			}
			probeFailures = append(probeFailures, fmt.Sprintf("%s (%s)", path, strings.TrimSpace(stderr)))
		}
	}

	kernelLog, stderr, err := runner.Run(ctx, "journalctl", "-k", "--since", "-"+cfg.KernelLogLookback, "--no-pager")
	if err != nil {
		return errorResult(checkplugin.CommandErrorSummary(stderr, err))
	}
	matchedLogLines = matchPatterns(kernelLog, cfg.BlockErrorPatterns)

	details := map[string]any{
		"required_mounts":      append([]string(nil), cfg.RequiredMounts...),
		"read_only_mounts":     readOnlyMounts,
		"missing_mounts":       missingMounts,
		"mount_probe_paths":    append([]string(nil), cfg.MountProbePaths...),
		"probe_failures":       probeFailures,
		"kernel_log_lookback":  cfg.KernelLogLookback,
		"matched_block_errors": matchedLogLines,
	}

	switch {
	case len(missingMounts) > 0:
		return model.CheckResult{
			CheckName:       CheckName,
			Status:          model.StatusFail,
			FailureDomain:   model.DomainFilesystem,
			InfraEvidence:   true,
			Summary:         fmt.Sprintf("required mount missing: %s", strings.Join(missingMounts, ", ")),
			StructuredCause: "mount_missing",
			Details:         details,
		}
	case len(readOnlyMounts) > 0:
		return model.CheckResult{
			CheckName:       CheckName,
			Status:          model.StatusFail,
			FailureDomain:   model.DomainFilesystem,
			InfraEvidence:   true,
			Summary:         fmt.Sprintf("read-only mount detected: %s", strings.Join(readOnlyMounts, ", ")),
			StructuredCause: "filesystem_read_only",
			Details:         details,
		}
	case len(probeFailures) > 0:
		return model.CheckResult{
			CheckName:       CheckName,
			Status:          model.StatusFail,
			FailureDomain:   model.DomainFilesystem,
			InfraEvidence:   true,
			Summary:         fmt.Sprintf("filesystem probe failed: %s", strings.Join(probeFailures, ", ")),
			StructuredCause: "mount_probe_inaccessible",
			Details:         details,
		}
	case len(matchedLogLines) > 0:
		return model.CheckResult{
			CheckName:       CheckName,
			Status:          model.StatusFail,
			FailureDomain:   model.DomainFilesystem,
			InfraEvidence:   true,
			Summary:         "kernel reported block I/O errors",
			StructuredCause: "block_io_error",
			Details:         details,
		}
	default:
		return model.CheckResult{
			CheckName:     CheckName,
			Status:        model.StatusPass,
			FailureDomain: model.DomainFilesystem,
			Summary:       "filesystem healthy",
			Details:       details,
		}
	}
}

func isReadOnlyMount(output string) bool {
	line := strings.TrimSpace(output)
	if line == "" {
		return false
	}
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return false
	}
	for _, option := range strings.Split(fields[len(fields)-1], ",") {
		if strings.EqualFold(strings.TrimSpace(option), "ro") {
			return true
		}
	}
	return false
}

func matchPatterns(output string, patterns []string) []string {
	lowerPatterns := make([]string, len(patterns))
	for i, p := range patterns {
		lowerPatterns[i] = strings.ToLower(p)
	}
	matches := make([]string, 0)
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lowerLine := strings.ToLower(trimmed)
		for _, pattern := range lowerPatterns {
			if strings.Contains(lowerLine, pattern) {
				matches = append(matches, trimmed)
				break
			}
		}
	}
	return matches
}

func errorResult(summary string) model.CheckResult {
	return model.CheckResult{
		CheckName:       CheckName,
		Status:          model.StatusError,
		FailureDomain:   model.DomainRuntime,
		Summary:         summary,
		StructuredCause: "filesystem_check_failed",
	}
}
