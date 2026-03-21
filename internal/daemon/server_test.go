package daemon

import (
	"context"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/yuuki/slurm-gpu-node-guard/internal/model"
)

type fakeEvaluator struct {
	response model.EvaluationDecision
	err      error
}

func (f fakeEvaluator) Evaluate(_ context.Context, _ model.EvaluationInput) (model.EvaluationDecision, error) {
	return f.response, f.err
}

func TestServerAndClientEvaluateOverUnixSocket(t *testing.T) {
	socketPath := fmt.Sprintf("/tmp/sgng-%d.sock", time.Now().UnixNano())
	t.Cleanup(func() {
		_ = os.Remove(socketPath)
	})

	srv := NewServer(socketPath, fakeEvaluator{
		response: model.EvaluationDecision{
			Verdict:       model.VerdictAllowAlert,
			Source:        "daemon",
			ShouldDrain:   false,
			ShouldRequeue: false,
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Run(ctx)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		conn, err := net.Dial("unix", socketPath)
		if err == nil {
			_ = conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("daemon did not start listening: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	client := NewClient(socketPath)
	decision, err := client.Evaluate(context.Background(), model.EvaluationInput{
		Phase: model.PhaseProlog,
		Job:   model.JobContext{ID: "123"},
	})
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if decision.Source != "daemon" || decision.Verdict != model.VerdictAllowAlert {
		t.Fatalf("unexpected decision: %+v", decision)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil && err != context.Canceled {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for daemon shutdown")
	}
}
