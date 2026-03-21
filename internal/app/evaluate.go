package app

import (
	"context"
	"errors"

	"github.com/yuuki/slurm-gpu-node-guard/internal/model"
)

// ErrDaemonUnavailable is an alias for model.ErrDaemonUnavailable.
var ErrDaemonUnavailable = model.ErrDaemonUnavailable

// Client is the interface for communicating with the guard daemon.
type Client interface {
	Evaluate(ctx context.Context, input model.EvaluationInput) (model.EvaluationDecision, error)
}

// LocalEvaluator is the interface for running evaluations in-process as a fallback.
type LocalEvaluator interface {
	Evaluate(ctx context.Context, input model.EvaluationInput) (model.EvaluationDecision, error)
}

// Dependencies bundles the daemon client and local evaluator used by EvaluateWithFallback.
type Dependencies struct {
	Client         Client
	LocalEvaluator LocalEvaluator
}

// EvaluateWithFallback tries the daemon first; if unavailable, falls back to the local evaluator.
func EvaluateWithFallback(ctx context.Context, deps Dependencies, input model.EvaluationInput) (model.EvaluationDecision, error) {
	if deps.Client != nil {
		decision, err := deps.Client.Evaluate(ctx, input)
		if err == nil {
			return decision, nil
		}
		if !errors.Is(err, ErrDaemonUnavailable) {
			return model.EvaluationDecision{}, err
		}
	}
	if deps.LocalEvaluator == nil {
		return model.EvaluationDecision{}, ErrDaemonUnavailable
	}
	decision, err := deps.LocalEvaluator.Evaluate(ctx, input)
	if err != nil {
		return model.EvaluationDecision{}, err
	}
	if decision.Source == "" {
		decision.Source = "local-fallback"
	}
	return decision, nil
}
