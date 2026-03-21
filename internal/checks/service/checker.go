package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/yuuki/slurm-gpu-node-guard/internal/checkplugin"
	"github.com/yuuki/slurm-gpu-node-guard/internal/model"
	"github.com/yuuki/slurm-gpu-node-guard/internal/plugin"
)

// CheckName is the identifier for the service health check plugin.
const CheckName = "service-health"

// Config configures required and optional services.
type Config struct {
	RequiredServices []string `json:"required_services"`
	OptionalServices []string `json:"optional_services"`
}

// Checker verifies systemd service health for required and optional node-local services.
type Checker struct {
	Runner checkplugin.CommandRunner
}

type serviceState struct {
	LoadState   string
	ActiveState string
	SubState    string
}

// Check runs service health checks and returns a CheckResult indicating pass, warn, fail, or error.
func (c Checker) Check(ctx context.Context, input plugin.Input) model.CheckResult {
	runner := c.Runner
	if runner == nil {
		runner = checkplugin.ExecRunner{}
	}

	cfg, err := checkplugin.DecodeConfig[Config](input.PluginConfig)
	if err != nil {
		return errorResult(err.Error())
	}

	requiredFailures := make([]string, 0)
	optionalWarnings := make([]string, 0)
	details := map[string]any{
		"required_services": append([]string(nil), cfg.RequiredServices...),
		"optional_services": append([]string(nil), cfg.OptionalServices...),
		"service_states":    map[string]any{},
	}
	stateDetails := details["service_states"].(map[string]any)

	for _, serviceName := range cfg.RequiredServices {
		state, err := inspectService(ctx, runner, serviceName)
		if err != nil {
			return errorResult(err.Error())
		}
		stateDetails[serviceName] = map[string]any{
			"load_state":   state.LoadState,
			"active_state": state.ActiveState,
			"sub_state":    state.SubState,
		}
		if !state.isHealthy() {
			requiredFailures = append(requiredFailures, summarizeService(serviceName, state))
		}
	}
	for _, serviceName := range cfg.OptionalServices {
		state, err := inspectService(ctx, runner, serviceName)
		if err != nil {
			return errorResult(err.Error())
		}
		stateDetails[serviceName] = map[string]any{
			"load_state":   state.LoadState,
			"active_state": state.ActiveState,
			"sub_state":    state.SubState,
		}
		if state.LoadState == "not-found" {
			continue
		}
		if !state.isHealthy() {
			optionalWarnings = append(optionalWarnings, summarizeService(serviceName, state))
		}
	}

	switch {
	case len(requiredFailures) > 0:
		return model.CheckResult{
			CheckName:       CheckName,
			Status:          model.StatusFail,
			FailureDomain:   model.DomainRuntime,
			InfraEvidence:   true,
			Summary:         fmt.Sprintf("required service unhealthy: %s", strings.Join(requiredFailures, ", ")),
			StructuredCause: "service_unhealthy",
			Details:         details,
		}
	case len(optionalWarnings) > 0:
		return model.CheckResult{
			CheckName:       CheckName,
			Status:          model.StatusWarn,
			FailureDomain:   model.DomainRuntime,
			InfraEvidence:   true,
			Summary:         fmt.Sprintf("optional service unhealthy: %s", strings.Join(optionalWarnings, ", ")),
			StructuredCause: "optional_service_unhealthy",
			Details:         details,
		}
	default:
		return model.CheckResult{
			CheckName:     CheckName,
			Status:        model.StatusPass,
			FailureDomain: model.DomainRuntime,
			Summary:       "required services healthy",
			Details:       details,
		}
	}
}

func inspectService(ctx context.Context, runner checkplugin.CommandRunner, serviceName string) (serviceState, error) {
	output, stderr, err := runner.Run(ctx, "systemctl", "show", serviceName, "--property=LoadState", "--property=ActiveState", "--property=SubState")
	if err != nil && strings.TrimSpace(output) == "" {
		return serviceState{}, fmt.Errorf("%s", checkplugin.CommandErrorSummary(stderr, err))
	}
	state := parseServiceState(output)
	if state.LoadState == "" && err != nil {
		return serviceState{}, fmt.Errorf("%s", checkplugin.CommandErrorSummary(stderr, err))
	}
	return state, nil
}

func parseServiceState(output string) serviceState {
	state := serviceState{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "LoadState="):
			state.LoadState = strings.TrimPrefix(line, "LoadState=")
		case strings.HasPrefix(line, "ActiveState="):
			state.ActiveState = strings.TrimPrefix(line, "ActiveState=")
		case strings.HasPrefix(line, "SubState="):
			state.SubState = strings.TrimPrefix(line, "SubState=")
		}
	}
	return state
}

func (s serviceState) isHealthy() bool {
	return s.LoadState == "loaded" && s.ActiveState == "active"
}

func summarizeService(name string, state serviceState) string {
	return fmt.Sprintf("%s(load=%s active=%s sub=%s)", name, state.LoadState, state.ActiveState, state.SubState)
}

func errorResult(summary string) model.CheckResult {
	return model.CheckResult{
		CheckName:       CheckName,
		Status:          model.StatusError,
		FailureDomain:   model.DomainRuntime,
		Summary:         summary,
		StructuredCause: "service_check_failed",
	}
}
