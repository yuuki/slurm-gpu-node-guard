package slurm

import (
	"context"
	"errors"
	"testing"

	"github.com/yuuki/slurm-gpu-node-guard/internal/model"
)

type fakeExecutor struct {
	drains   int
	requeues int
	drainErr error
	reqErr   error
}

func (f *fakeExecutor) DrainNode(_ context.Context, _ string, _ string) error {
	f.drains++
	return f.drainErr
}

func (f *fakeExecutor) RequeueJob(_ context.Context, _ string) error {
	f.requeues++
	return f.reqErr
}

func TestApplyDecisionForRequeueVerdict(t *testing.T) {
	exec := &fakeExecutor{}
	err := ApplyDecision(context.Background(), exec, model.ActionDecision{
		NodeName:      "node001",
		JobID:         "123",
		DrainReason:   "gpu unhealthy",
		ShouldDrain:   true,
		ShouldRequeue: true,
	})
	if err != nil {
		t.Fatalf("ApplyDecision returned error: %v", err)
	}
	if exec.drains != 1 || exec.requeues != 1 {
		t.Fatalf("unexpected calls: drains=%d requeues=%d", exec.drains, exec.requeues)
	}
}

func TestApplyDecisionIgnoresAlreadyDoneErrors(t *testing.T) {
	exec := &fakeExecutor{
		drainErr: ErrAlreadyDone,
		reqErr:   ErrAlreadyDone,
	}
	err := ApplyDecision(context.Background(), exec, model.ActionDecision{
		NodeName:      "node001",
		JobID:         "123",
		DrainReason:   "gpu unhealthy",
		ShouldDrain:   true,
		ShouldRequeue: true,
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestApplyDecisionReturnsRealErrors(t *testing.T) {
	exec := &fakeExecutor{
		drainErr: errors.New("permission denied"),
	}
	err := ApplyDecision(context.Background(), exec, model.ActionDecision{
		NodeName:    "node001",
		DrainReason: "gpu unhealthy",
		ShouldDrain: true,
	})
	if err == nil {
		t.Fatalf("expected error")
	}
}
