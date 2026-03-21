package gpuerrors

import (
	"context"
	"encoding/xml"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/yuuki/slurm-gpu-node-guard/internal/checkplugin"
	"github.com/yuuki/slurm-gpu-node-guard/internal/model"
	"github.com/yuuki/slurm-gpu-node-guard/internal/plugin"
)

// CheckName is the identifier for the GPU error health check plugin.
const CheckName = "gpu-errors"

const defaultLogLookback = "5m"

var (
	xidTailPattern      = regexp.MustCompile(`([0-9]+)`)
	pcieErrorIndicators = []string{
		"fallen off the bus",
		"pcie bus error",
		"aer:",
		"pcie link",
		"gpu disappeared",
	}
)

// Config configures GPU error detection thresholds and filters.
type Config struct {
	XIDFailCodes          []int  `json:"xid_fail_codes"`
	XIDWarnCodes          []int  `json:"xid_warn_codes"`
	KernelLogLookback     string `json:"kernel_log_lookback"`
	RowRemapFailThreshold int    `json:"row_remap_fail_threshold"`
	RequireECCHealthy     *bool  `json:"require_ecc_healthy"`
}

// Checker verifies GPU error signals from nvidia-smi XML and recent kernel logs.
type Checker struct {
	Runner checkplugin.CommandRunner
}

type xmlNode struct {
	XMLName  xml.Name
	Content  string    `xml:",chardata"`
	Children []xmlNode `xml:",any"`
}

type leafValue struct {
	path  []string
	name  string
	value string
}

// Check runs GPU error health checks and returns a CheckResult indicating pass, warn, fail, or error.
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
		cfg.KernelLogLookback = defaultLogLookback
	}

	xmlOutput, stderr, err := runner.Run(ctx, "nvidia-smi", "-q", "-x")
	if err != nil {
		return errorResult(checkplugin.CommandErrorSummary(stderr, err))
	}
	snapshot, err := parseSMIXML(xmlOutput)
	if err != nil {
		return errorResult(err.Error())
	}

	kernelLog, stderr, err := runner.Run(ctx, "journalctl", "-k", "--since", "-"+cfg.KernelLogLookback, "--no-pager")
	if err != nil {
		return errorResult(checkplugin.CommandErrorSummary(stderr, err))
	}
	xids := parseXIDs(kernelLog)
	pcieLines := findPCIELines(kernelLog)

	details := map[string]any{
		"kernel_log_lookback":        cfg.KernelLogLookback,
		"xid_codes":                  xids,
		"pcie_error_lines":           pcieLines,
		"ecc_uncorrectable_total":    snapshot.eccUncorrectableTotal,
		"row_remap_pending_total":    snapshot.rowRemapPendingTotal,
		"row_remap_failure_detected": snapshot.rowRemapFailureDetected,
	}

	if len(pcieLines) > 0 {
		return model.CheckResult{
			CheckName:       CheckName,
			Status:          model.StatusFail,
			FailureDomain:   model.DomainInterconnect,
			InfraEvidence:   true,
			Summary:         "PCIe link error detected in kernel log",
			StructuredCause: "pcie_link_error",
			Details:         details,
		}
	}
	if snapshot.rowRemapFailureDetected || snapshot.rowRemapPendingTotal > cfg.RowRemapFailThreshold {
		return model.CheckResult{
			CheckName:       CheckName,
			Status:          model.StatusFail,
			FailureDomain:   model.DomainGPU,
			InfraEvidence:   true,
			Summary:         "row remap failure detected",
			StructuredCause: "row_remap_failed",
			Details:         details,
		}
	}
	if cfg.requireECCHealthy() && snapshot.eccUncorrectableTotal > 0 {
		return model.CheckResult{
			CheckName:       CheckName,
			Status:          model.StatusFail,
			FailureDomain:   model.DomainGPU,
			InfraEvidence:   true,
			Summary:         fmt.Sprintf("uncorrectable ECC errors detected: %d", snapshot.eccUncorrectableTotal),
			StructuredCause: "ecc_uncorrectable",
			Details:         details,
		}
	}

	failXIDs, warnXIDs := classifyXIDs(xids, cfg)
	switch {
	case len(failXIDs) > 0:
		details["xid_fail_codes"] = failXIDs
		return model.CheckResult{
			CheckName:       CheckName,
			Status:          model.StatusFail,
			FailureDomain:   model.DomainGPU,
			InfraEvidence:   true,
			Summary:         fmt.Sprintf("GPU XID errors detected: %v", failXIDs),
			StructuredCause: "gpu_xid",
			Details:         details,
		}
	case len(warnXIDs) > 0:
		details["xid_warn_codes"] = warnXIDs
		return model.CheckResult{
			CheckName:       CheckName,
			Status:          model.StatusWarn,
			FailureDomain:   model.DomainGPU,
			InfraEvidence:   true,
			Summary:         fmt.Sprintf("GPU XID warnings detected: %v", warnXIDs),
			StructuredCause: "gpu_xid_warn",
			Details:         details,
		}
	default:
		return model.CheckResult{
			CheckName:     CheckName,
			Status:        model.StatusPass,
			FailureDomain: model.DomainGPU,
			Summary:       "GPU error signals healthy",
			Details:       details,
		}
	}
}

type xmlSnapshot struct {
	eccUncorrectableTotal   int
	rowRemapPendingTotal    int
	rowRemapFailureDetected bool
}

func parseSMIXML(output string) (xmlSnapshot, error) {
	var root xmlNode
	if err := xml.Unmarshal([]byte(output), &root); err != nil {
		return xmlSnapshot{}, fmt.Errorf("decode nvidia-smi xml: %w", err)
	}

	var snapshot xmlSnapshot
	leaves := collectLeafValues(root, nil)
	for _, leaf := range leaves {
		lowerName := strings.ToLower(leaf.name)
		lowerPath := strings.ToLower(strings.Join(leaf.path, "/"))
		value := strings.TrimSpace(leaf.value)
		if value == "" || strings.EqualFold(value, "N/A") {
			continue
		}
		if strings.Contains(lowerPath, "ecc") && strings.Contains(lowerName, "uncorrect") {
			snapshot.eccUncorrectableTotal += parseInt(value)
			continue
		}
		if strings.Contains(lowerPath, "row_remap") || strings.Contains(lowerPath, "row-remap") || strings.Contains(lowerPath, "row_remapper") {
			switch {
			case strings.Contains(lowerName, "pending"):
				snapshot.rowRemapPendingTotal += parseInt(value)
			case strings.Contains(lowerName, "fail"):
				if parseBoolish(value) || parseInt(value) > 0 {
					snapshot.rowRemapFailureDetected = true
				}
			}
		}
	}
	return snapshot, nil
}

func collectLeafValues(node xmlNode, path []string) []leafValue {
	currentPath := append(append([]string(nil), path...), node.XMLName.Local)
	if len(node.Children) == 0 {
		return []leafValue{{
			path:  currentPath,
			name:  node.XMLName.Local,
			value: strings.TrimSpace(node.Content),
		}}
	}
	values := make([]leafValue, 0)
	for _, child := range node.Children {
		values = append(values, collectLeafValues(child, currentPath)...)
	}
	return values
}

func parseXIDs(output string) []int {
	codes := make([]int, 0)
	seen := map[int]struct{}{}
	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(strings.ToLower(line), "xid") {
			continue
		}
		candidate := line
		if idx := strings.LastIndex(candidate, "):"); idx >= 0 {
			candidate = candidate[idx+2:]
		} else if idx := strings.LastIndex(strings.ToLower(candidate), "xid"); idx >= 0 {
			candidate = candidate[idx+3:]
		}
		match := xidTailPattern.FindStringSubmatch(candidate)
		if len(match) < 2 {
			continue
		}
		code, err := strconv.Atoi(match[1])
		if err != nil {
			continue
		}
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		codes = append(codes, code)
	}
	slices.Sort(codes)
	return codes
}

func findPCIELines(output string) []string {
	lines := make([]string, 0)
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		for _, indicator := range pcieErrorIndicators {
			if strings.Contains(lower, indicator) {
				lines = append(lines, trimmed)
				break
			}
		}
	}
	return lines
}

func classifyXIDs(xids []int, cfg Config) ([]int, []int) {
	if len(xids) == 0 {
		return nil, nil
	}
	fail := make([]int, 0)
	warn := make([]int, 0)
	for _, code := range xids {
		switch {
		case len(cfg.XIDWarnCodes) > 0 && slices.Contains(cfg.XIDWarnCodes, code) && !slices.Contains(cfg.XIDFailCodes, code):
			warn = append(warn, code)
		case len(cfg.XIDFailCodes) == 0 && len(cfg.XIDWarnCodes) == 0:
			fail = append(fail, code)
		case slices.Contains(cfg.XIDFailCodes, code):
			fail = append(fail, code)
		case len(cfg.XIDWarnCodes) > 0 && slices.Contains(cfg.XIDWarnCodes, code):
			warn = append(warn, code)
		default:
			fail = append(fail, code)
		}
	}
	return fail, warn
}

func parseInt(value string) int {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0
	}
	return n
}

func parseBoolish(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "yes", "y", "1", "failed", "failure":
		return true
	default:
		return false
	}
}

func (c Config) requireECCHealthy() bool {
	return c.RequireECCHealthy == nil || *c.RequireECCHealthy
}

func errorResult(summary string) model.CheckResult {
	return model.CheckResult{
		CheckName:       CheckName,
		Status:          model.StatusError,
		FailureDomain:   model.DomainRuntime,
		Summary:         summary,
		StructuredCause: "gpu_error_check_failed",
	}
}
