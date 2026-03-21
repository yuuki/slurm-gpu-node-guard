package engine

import (
	"context"
	"fmt"
	"slices"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/yuuki/slurm-gpu-node-guard/internal/model"
	"github.com/yuuki/slurm-gpu-node-guard/internal/plugin"
	"github.com/yuuki/slurm-gpu-node-guard/internal/policy"
)

type Engine struct {
	policy  *policy.Policy
	plugins []model.PluginSpec
	runner  plugin.Runner
	tracer  trace.Tracer
	counter metric.Int64Counter
}

func New(p *policy.Policy, plugins []model.PluginSpec) (*Engine, error) {
	meter := otel.Meter("slurm-gpu-node-guard/engine")
	counter, err := meter.Int64Counter("slurm_gpu_node_guard_checks_total")
	if err != nil {
		return nil, fmt.Errorf("create check counter: %w", err)
	}
	return &Engine{
		policy:  p,
		plugins: append([]model.PluginSpec(nil), plugins...),
		runner:  plugin.Runner{},
		tracer:  otel.Tracer("slurm-gpu-node-guard/engine"),
		counter: counter,
	}, nil
}

func (e *Engine) Evaluate(ctx context.Context, input model.EvaluationInput) (model.EvaluationDecision, error) {
	ctx, span := e.tracer.Start(ctx, "engine.evaluate", trace.WithAttributes(attribute.String("phase", string(input.Phase))))
	defer span.End()

	if len(input.CheckResults) == 0 {
		results, err := e.RunChecks(ctx, input.Phase, input.Job, input.Node)
		if err != nil {
			return model.EvaluationDecision{}, err
		}
		input.CheckResults = results
	}
	input.Policy = e.policy
	decision, err := policy.Evaluate(input)
	if err != nil {
		return model.EvaluationDecision{}, err
	}
	if decision.Source == "" {
		decision.Source = "local"
	}
	return decision, nil
}

func (e *Engine) RunChecks(ctx context.Context, phase model.Phase, job model.JobContext, node model.NodeContext) ([]model.CheckResult, error) {
	selected := make([]model.PluginSpec, 0, len(e.plugins))
	for _, spec := range e.plugins {
		if len(spec.Phases) == 0 || slices.Contains(spec.Phases, phase) {
			selected = append(selected, spec)
		}
	}
	if len(selected) == 0 {
		return nil, nil
	}

	timeout := e.phaseTimeout(phase)
	type resultItem struct {
		result model.CheckResult
		err    error
	}
	ch := make(chan resultItem, len(selected))
	for _, spec := range selected {
		go func(spec model.PluginSpec) {
			runCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			result, err := e.runner.Run(runCtx, plugin.Request{
				Path:  spec.Path,
				Name:  spec.Name,
				Phase: phase,
				Job:   job,
				Node:  node,
				Timeouts: map[string]string{
					string(phase): timeout.String(),
				},
			})
			ch <- resultItem{result: result, err: err}
		}(spec)
	}

	results := make([]model.CheckResult, 0, len(selected))
	for range selected {
		item := <-ch
		if item.err != nil {
			continue
		}
		e.counter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("check_name", item.result.CheckName),
			attribute.String("status", string(item.result.Status)),
			attribute.String("phase", string(phase)),
		))
		results = append(results, item.result)
	}
	return results, nil
}

func (e *Engine) phaseTimeout(phase model.Phase) time.Duration {
	if e.policy == nil {
		return 2 * time.Second
	}
	raw := e.policy.CheckTimeouts[phase]
	if raw == "" {
		return 2 * time.Second
	}
	timeout, err := time.ParseDuration(raw)
	if err != nil {
		return 2 * time.Second
	}
	return timeout
}
