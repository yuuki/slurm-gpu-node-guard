package policy

import (
	"strings"
	"testing"

	"github.com/yuuki/slurm-gpu-node-guard/internal/model"
)

func samplePolicy() *Policy {
	return &Policy{
		CheckTimeouts: map[model.Phase]string{
			model.PhaseProlog: "1500ms",
			model.PhaseEpilog: "3s",
		},
		Domains: map[model.FailureDomain]DomainPolicy{
			model.DomainGPU: {
				Severity:              "critical",
				RequireInfraEvidence:  true,
				DrainReasonTemplate:   "gpu unhealthy: {{ .Summary }}",
				WarnVerdictByPhase:    map[model.Phase]model.Verdict{model.PhaseProlog: model.VerdictAllowAlert, model.PhaseEpilog: model.VerdictDrainAfterJob},
				FailVerdictByPhase:    map[model.Phase]model.Verdict{model.PhaseProlog: model.VerdictBlockDrainRequeue, model.PhaseEpilog: model.VerdictBlockDrainRequeue},
				NotificationReceivers: []string{"default"},
			},
			model.DomainRDMA: {
				Severity:              "deferred",
				RequireInfraEvidence:  false,
				DrainReasonTemplate:   "rdma degraded: {{ .Summary }}",
				WarnVerdictByPhase:    map[model.Phase]model.Verdict{model.PhaseProlog: model.VerdictAllowAlert, model.PhaseEpilog: model.VerdictDrainAfterJob},
				FailVerdictByPhase:    map[model.Phase]model.Verdict{model.PhaseProlog: model.VerdictBlockDrain, model.PhaseEpilog: model.VerdictDrainAfterJob},
				NotificationReceivers: []string{"default"},
			},
		},
	}
}

func TestEvaluatePrologHealthyAllows(t *testing.T) {
	p := samplePolicy()
	decision, err := Evaluate(model.EvaluationInput{
		Phase: model.PhaseProlog,
		Job: model.JobContext{
			ID:       "123",
			NodeName: "node001",
		},
		CheckResults: []model.CheckResult{
			{CheckName: "gpu-presence", Status: model.StatusPass, FailureDomain: model.DomainGPU},
		},
		Policy: p,
	})
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if decision.Verdict != model.VerdictAllow {
		t.Fatalf("expected %q, got %q", model.VerdictAllow, decision.Verdict)
	}
}

func TestEvaluatePrologInfraFailureDrainAndRequeue(t *testing.T) {
	p := samplePolicy()
	decision, err := Evaluate(model.EvaluationInput{
		Phase: model.PhaseProlog,
		Job: model.JobContext{
			ID:       "2000",
			NodeName: "node099",
		},
		CheckResults: []model.CheckResult{
			{
				CheckName:       "gpu-xid",
				Status:          model.StatusFail,
				FailureDomain:   model.DomainGPU,
				InfraEvidence:   true,
				Summary:         "XID 79",
				StructuredCause: "xid",
			},
		},
		Policy: p,
	})
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if decision.Verdict != model.VerdictBlockDrainRequeue {
		t.Fatalf("expected %q, got %q", model.VerdictBlockDrainRequeue, decision.Verdict)
	}
	if !strings.Contains(decision.DrainReason, "gpu unhealthy: XID 79") {
		t.Fatalf("unexpected drain reason: %q", decision.DrainReason)
	}
}

func TestEvaluateEpilogRDMAFailureDrainsWithoutRequeue(t *testing.T) {
	p := samplePolicy()
	decision, err := Evaluate(model.EvaluationInput{
		Phase: model.PhaseEpilog,
		Job: model.JobContext{
			ID:         "3000",
			NodeName:   "node070",
			ExitCode:   1,
			SignalName: "TERM",
		},
		CheckResults: []model.CheckResult{
			{
				CheckName:       "rdma-link",
				Status:          model.StatusFail,
				FailureDomain:   model.DomainRDMA,
				InfraEvidence:   false,
				Summary:         "symbol errors above threshold",
				StructuredCause: "link",
			},
		},
		Policy: p,
	})
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if decision.Verdict != model.VerdictDrainAfterJob {
		t.Fatalf("expected %q, got %q", model.VerdictDrainAfterJob, decision.Verdict)
	}
	if decision.ShouldRequeue {
		t.Fatalf("expected no requeue")
	}
}

func TestEvaluatePluginErrorDowngradesToAllowAlert(t *testing.T) {
	p := samplePolicy()
	decision, err := Evaluate(model.EvaluationInput{
		Phase: model.PhaseProlog,
		Job: model.JobContext{
			ID:       "4000",
			NodeName: "node111",
		},
		CheckResults: []model.CheckResult{
			{
				CheckName:     "gpu-plugin",
				Status:        model.StatusError,
				FailureDomain: model.DomainGPU,
				Summary:       "plugin exited with code 2",
			},
		},
		Policy: p,
	})
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if decision.Verdict != model.VerdictAllowAlert {
		t.Fatalf("expected %q, got %q", model.VerdictAllowAlert, decision.Verdict)
	}
}
