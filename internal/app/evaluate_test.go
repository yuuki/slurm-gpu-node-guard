package app

import (
	"context"
	"errors"
	"testing"

	"github.com/yuuki/slurm-gpu-node-guard/internal/model"
)

type fakeClient struct {
	response model.EvaluationDecision
	err      error
}

func (f fakeClient) Evaluate(_ context.Context, _ model.EvaluationInput) (model.EvaluationDecision, error) {
	return f.response, f.err
}

type fakeLocalEvaluator struct {
	response model.EvaluationDecision
	err      error
	called   bool
}

func (f *fakeLocalEvaluator) Evaluate(_ context.Context, _ model.EvaluationInput) (model.EvaluationDecision, error) {
	f.called = true
	return f.response, f.err
}

func TestEvaluateWithFallbackUsesLocalWhenDaemonUnavailable(t *testing.T) {
	local := &fakeLocalEvaluator{
		response: model.EvaluationDecision{Verdict: model.VerdictAllow, Source: "local-fallback"},
	}
	decision, err := EvaluateWithFallback(context.Background(), Dependencies{
		Client:         fakeClient{err: ErrDaemonUnavailable},
		LocalEvaluator: local,
	}, model.EvaluationInput{
		Phase: model.PhaseProlog,
	})
	if err != nil {
		t.Fatalf("EvaluateWithFallback returned error: %v", err)
	}
	if !local.called {
		t.Fatalf("expected local evaluator to be called")
	}
	if decision.Source != "local-fallback" {
		t.Fatalf("unexpected source: %+v", decision)
	}
}

func TestEvaluateWithFallbackReturnsClientResponseWhenDaemonHealthy(t *testing.T) {
	local := &fakeLocalEvaluator{
		response: model.EvaluationDecision{Verdict: model.VerdictAllow, Source: "local-fallback"},
	}
	decision, err := EvaluateWithFallback(context.Background(), Dependencies{
		Client:         fakeClient{response: model.EvaluationDecision{Verdict: model.VerdictAllowAlert, Source: "daemon"}},
		LocalEvaluator: local,
	}, model.EvaluationInput{
		Phase: model.PhaseProlog,
	})
	if err != nil {
		t.Fatalf("EvaluateWithFallback returned error: %v", err)
	}
	if local.called {
		t.Fatalf("expected local evaluator not to be called")
	}
	if decision.Source != "daemon" {
		t.Fatalf("unexpected source: %+v", decision)
	}
}

func TestEvaluateWithFallbackReturnsErrorWhenBothPathsFail(t *testing.T) {
	local := &fakeLocalEvaluator{
		err: errors.New("local failed"),
	}
	_, err := EvaluateWithFallback(context.Background(), Dependencies{
		Client:         fakeClient{err: ErrDaemonUnavailable},
		LocalEvaluator: local,
	}, model.EvaluationInput{
		Phase: model.PhaseProlog,
	})
	if err == nil {
		t.Fatalf("expected error")
	}
}
