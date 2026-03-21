package model

import "errors"

// Phase represents a Slurm job lifecycle phase (prolog or epilog).
type Phase string

// Supported lifecycle phases.
const (
	PhaseProlog Phase = "prolog"
	PhaseEpilog Phase = "epilog"
)

// CheckStatus represents the outcome status of a health check plugin.
type CheckStatus string

// Possible check statuses returned by plugins.
const (
	StatusPass  CheckStatus = "pass"
	StatusWarn  CheckStatus = "warn"
	StatusFail  CheckStatus = "fail"
	StatusError CheckStatus = "error"
)

// FailureDomain categorizes the infrastructure component that failed.
type FailureDomain string

// Known failure domains.
const (
	DomainUnknown      FailureDomain = "unknown"
	DomainGPU          FailureDomain = "gpu"
	DomainRDMA         FailureDomain = "rdma"
	DomainInterconnect FailureDomain = "interconnect"
	DomainRuntime      FailureDomain = "runtime"
	DomainFilesystem   FailureDomain = "filesystem"
)

// Verdict represents the final decision produced by the policy evaluator.
type Verdict string

// Supported verdicts, ordered from least to most severe.
const (
	VerdictAllow             Verdict = "allow"
	VerdictAllowAlert        Verdict = "allow_alert"
	VerdictDrainAfterJob     Verdict = "drain_after_job"
	VerdictBlockDrain        Verdict = "block_drain"
	VerdictBlockDrainRequeue Verdict = "block_drain_requeue"
)

// ErrDaemonUnavailable is returned when the guard daemon cannot be reached.
var ErrDaemonUnavailable = errors.New("daemon unavailable")

// JobContext holds Slurm job metadata passed to plugins and evaluations.
type JobContext struct {
	ID         string `json:"id,omitempty" yaml:"id,omitempty"`
	NodeName   string `json:"node_name,omitempty" yaml:"node_name,omitempty"`
	Cluster    string `json:"cluster,omitempty" yaml:"cluster,omitempty"`
	User       string `json:"user,omitempty" yaml:"user,omitempty"`
	ExitCode   int    `json:"exit_code,omitempty" yaml:"exit_code,omitempty"`
	SignalName string `json:"signal_name,omitempty" yaml:"signal_name,omitempty"`
}

// NodeContext holds the identity of the compute node being checked.
type NodeContext struct {
	Name string `json:"name,omitempty" yaml:"name,omitempty"`
}

// PluginSpec defines an external check plugin and the phases it runs in.
type PluginSpec struct {
	Name   string  `json:"name" yaml:"name"`
	Path   string  `json:"path" yaml:"path"`
	Phases []Phase `json:"phases,omitempty" yaml:"phases,omitempty"`
}

// CheckResult is the structured output returned by a check plugin.
type CheckResult struct {
	CheckName       string         `json:"check_name" yaml:"check_name"`
	Status          CheckStatus    `json:"status" yaml:"status"`
	FailureDomain   FailureDomain  `json:"failure_domain" yaml:"failure_domain"`
	InfraEvidence   bool           `json:"infra_evidence,omitempty" yaml:"infra_evidence,omitempty"`
	Summary         string         `json:"summary,omitempty" yaml:"summary,omitempty"`
	Details         map[string]any `json:"details,omitempty" yaml:"details,omitempty"`
	StructuredCause string         `json:"structured_cause,omitempty" yaml:"structured_cause,omitempty"`
}

// EvaluationInput bundles the phase, job/node context, and check results for policy evaluation.
type EvaluationInput struct {
	Phase        Phase         `json:"phase"`
	Job          JobContext    `json:"job_context"`
	Node         NodeContext   `json:"node_context,omitempty"`
	CheckResults []CheckResult `json:"check_results,omitempty"`
	Policy       any           `json:"-"`
}

// EvaluationDecision is the verdict produced by the policy evaluator, including drain/requeue flags.
type EvaluationDecision struct {
	Verdict               Verdict       `json:"verdict"`
	DrainReason           string        `json:"drain_reason,omitempty"`
	ShouldDrain           bool          `json:"should_drain"`
	ShouldRequeue         bool          `json:"should_requeue"`
	Summary               string        `json:"summary,omitempty"`
	Source                string        `json:"source,omitempty"`
	NotificationReceivers []string      `json:"notification_receivers,omitempty"`
	Results               []CheckResult `json:"results,omitempty"`
}

// ToActionDecision converts the evaluation decision into an ActionDecision scoped to the given job.
func (d EvaluationDecision) ToActionDecision(job JobContext) ActionDecision {
	return ActionDecision{
		NodeName:      job.NodeName,
		JobID:         job.ID,
		DrainReason:   d.DrainReason,
		ShouldDrain:   d.ShouldDrain,
		ShouldRequeue: d.ShouldRequeue,
	}
}

// ActionDecision describes the drain and requeue actions to apply via scontrol.
type ActionDecision struct {
	NodeName      string
	JobID         string
	DrainReason   string
	ShouldDrain   bool
	ShouldRequeue bool
}

// NotificationEvent is the payload sent to notification receivers after evaluation.
type NotificationEvent struct {
	ReceiverNames []string `json:"receiver_names,omitempty"`
	NodeName      string   `json:"node_name,omitempty"`
	JobID         string   `json:"job_id,omitempty"`
	Verdict       Verdict  `json:"verdict"`
	Summary       string   `json:"summary,omitempty"`
	DrainReason   string   `json:"drain_reason,omitempty"`
	Source        string   `json:"source,omitempty"`
}
