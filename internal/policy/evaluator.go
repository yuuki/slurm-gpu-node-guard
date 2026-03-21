package policy

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"

	"github.com/yuuki/slurm-gpu-node-guard/internal/model"
)

// Policy defines per-phase timeouts and per-failure-domain verdict rules.
type Policy struct {
	CheckTimeouts map[model.Phase]string               `yaml:"check_timeouts"`
	Domains       map[model.FailureDomain]DomainPolicy `yaml:"domains"`
}

// DomainPolicy configures how a specific failure domain maps check results to verdicts.
type DomainPolicy struct {
	Severity              string                        `yaml:"severity"`
	RequireInfraEvidence  bool                          `yaml:"require_infra_evidence"`
	DrainReasonTemplate   string                        `yaml:"drain_reason_template"`
	WarnVerdictByPhase    map[model.Phase]model.Verdict `yaml:"on_warn"`
	FailVerdictByPhase    map[model.Phase]model.Verdict `yaml:"on_fail"`
	NotificationReceivers []string                      `yaml:"notification_receivers"`
}

// Evaluate applies the policy to the given check results and returns the highest-priority verdict.
func Evaluate(input model.EvaluationInput) (model.EvaluationDecision, error) {
	p, err := coercePolicy(input.Policy)
	if err != nil {
		return model.EvaluationDecision{}, err
	}

	decision := model.EvaluationDecision{
		Verdict: model.VerdictAllow,
		Source:  "policy",
		Results: input.CheckResults,
	}

	for _, result := range input.CheckResults {
		candidate := evaluateResult(input.Phase, p, result)
		if verdictPriority(candidate.Verdict) > verdictPriority(decision.Verdict) {
			candidate.Results = input.CheckResults
			decision = candidate
			continue
		}
		if verdictPriority(candidate.Verdict) == verdictPriority(decision.Verdict) {
			decision.NotificationReceivers = mergeUnique(decision.NotificationReceivers, candidate.NotificationReceivers)
			if decision.DrainReason == "" && candidate.DrainReason != "" {
				decision.DrainReason = candidate.DrainReason
			}
			if decision.Summary == "" && candidate.Summary != "" {
				decision.Summary = candidate.Summary
			}
		}
	}

	decision.ShouldDrain = shouldDrain(decision.Verdict)
	decision.ShouldRequeue = decision.Verdict == model.VerdictBlockDrainRequeue
	return decision, nil
}

func coercePolicy(v any) (*Policy, error) {
	switch p := v.(type) {
	case *Policy:
		return p, nil
	case Policy:
		return &p, nil
	case nil:
		return nil, fmt.Errorf("policy is required")
	default:
		return nil, fmt.Errorf("unsupported policy type %T", v)
	}
}

func evaluateResult(phase model.Phase, p *Policy, result model.CheckResult) model.EvaluationDecision {
	if result.Status == model.StatusPass {
		return model.EvaluationDecision{
			Verdict: model.VerdictAllow,
			Summary: result.Summary,
			Source:  "policy",
		}
	}
	if result.Status == model.StatusError {
		return model.EvaluationDecision{
			Verdict: model.VerdictAllowAlert,
			Summary: nonEmpty(result.Summary, "check execution error"),
			Source:  "policy",
		}
	}

	domainPolicy, ok := p.Domains[result.FailureDomain]
	if !ok {
		return model.EvaluationDecision{
			Verdict: model.VerdictAllowAlert,
			Summary: nonEmpty(result.Summary, "unmapped failure domain"),
			Source:  "policy",
		}
	}

	var verdict model.Verdict
	switch result.Status {
	case model.StatusWarn:
		verdict = domainPolicy.WarnVerdictByPhase[phase]
	case model.StatusFail:
		verdict = domainPolicy.FailVerdictByPhase[phase]
	default:
		verdict = model.VerdictAllowAlert
	}
	if verdict == "" {
		verdict = fallbackVerdict(domainPolicy.Severity, phase, result.Status)
	}
	if verdict == model.VerdictBlockDrainRequeue && !result.InfraEvidence {
		verdict = model.VerdictBlockDrain
	}
	if verdict == model.VerdictBlockDrain && domainPolicy.RequireInfraEvidence && !result.InfraEvidence && result.Status == model.StatusWarn {
		verdict = model.VerdictAllowAlert
	}

	return model.EvaluationDecision{
		Verdict:               verdict,
		DrainReason:           renderDrainReason(domainPolicy.DrainReasonTemplate, result),
		Summary:               result.Summary,
		Source:                "policy",
		NotificationReceivers: append([]string(nil), domainPolicy.NotificationReceivers...),
	}
}

func fallbackVerdict(severity string, phase model.Phase, status model.CheckStatus) model.Verdict {
	switch severity {
	case "critical":
		if status == model.StatusWarn {
			return model.VerdictAllowAlert
		}
		if phase == model.PhaseProlog {
			return model.VerdictBlockDrain
		}
		return model.VerdictDrainAfterJob
	case "deferred":
		if phase == model.PhaseProlog {
			return model.VerdictBlockDrain
		}
		return model.VerdictDrainAfterJob
	default:
		return model.VerdictAllowAlert
	}
}

func renderDrainReason(format string, result model.CheckResult) string {
	if format == "" {
		return result.Summary
	}
	tmpl, err := template.New("drain_reason").Parse(format)
	if err != nil {
		return result.Summary
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, result); err != nil {
		return result.Summary
	}
	return strings.TrimSpace(buf.String())
}

func verdictPriority(v model.Verdict) int {
	switch v {
	case model.VerdictAllow:
		return 0
	case model.VerdictAllowAlert:
		return 1
	case model.VerdictDrainAfterJob:
		return 2
	case model.VerdictBlockDrain:
		return 3
	case model.VerdictBlockDrainRequeue:
		return 4
	default:
		return -1
	}
}

func shouldDrain(v model.Verdict) bool {
	return v == model.VerdictDrainAfterJob || v == model.VerdictBlockDrain || v == model.VerdictBlockDrainRequeue
}

func mergeUnique(dst []string, src []string) []string {
	seen := make(map[string]struct{}, len(dst))
	for _, item := range dst {
		seen[item] = struct{}{}
	}
	for _, item := range src {
		if _, ok := seen[item]; ok {
			continue
		}
		dst = append(dst, item)
		seen[item] = struct{}{}
	}
	return dst
}

func nonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
