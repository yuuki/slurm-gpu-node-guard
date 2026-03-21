package app

import (
	"context"
	"errors"

	"github.com/yuuki/slurm-gpu-node-guard/internal/model"
)

var ErrDaemonUnavailable = model.ErrDaemonUnavailable

type Client interface {
	Evaluate(ctx context.Context, input model.EvaluationInput) (model.EvaluationDecision, error)
}

type LocalEvaluator interface {
	Evaluate(ctx context.Context, input model.EvaluationInput) (model.EvaluationDecision, error)
}

type Dependencies struct {
	Client         Client
	LocalEvaluator LocalEvaluator
}

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
