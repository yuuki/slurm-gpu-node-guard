# slurm-gpu-node-guard Design

## Overview

`slurm-gpu-node-guard` is a node-local health guard for Slurm-based GPU clusters.
It is designed to stop jobs from starting on bad nodes, classify infrastructure-related failures with explicit semantics, and automate safe remediation actions such as draining nodes and requeueing jobs when appropriate.

The design follows the operational lesson described in [HPCA2025]: the system should strive for no second job failure from a bad node.
In practice, this means the tool favors fast pre-flight checks, conservative remediation boundaries, and a clear separation between fact collection and site policy.

## Design Goals

1. Operational safety
2. Clear failure semantics
3. Low Prolog latency
4. Composability and extensibility
5. OSS maintainability
6. Clean separation between policy and mechanism

## Non-Goals

- No Kubernetes dependency
- No required centralized control plane beyond Slurm
- No assumption of vendor-specific enterprise products
- No heavyweight diagnostics on every job start

## Architecture

The v1 architecture has two executable entrypoints:

- `guardctl`: a CLI invoked by Slurm `Prolog` and `Epilog`, and also usable for manual check execution and event reporting
- `guardd`: a node-local daemon that exposes a UNIX socket API for local evaluation requests

The daemon is intentionally local-only.
It does not act as a cluster-wide control plane.
Its job is to centralize node-local evaluation, provide a stable IPC endpoint, and keep the interaction model simple for Slurm hooks.

The main internal subsystems are:

- `engine`: runs phase-specific checks and evaluates policy
- `plugin`: executes external health check plugins and normalizes their output
- `policy`: maps raw check results to remediation verdicts
- `slurm`: applies `drain` and `requeue` actions via `scontrol`
- `notify`: emits structured notifications through webhooks or external commands
- `telemetry`: initializes OpenTelemetry providers

## Runtime Model

### Prolog

At job start, Slurm invokes `guardctl prolog`.
`guardctl` loads the YAML configuration, builds an evaluation input from the Slurm environment, and tries to send that input to `guardd`.

If `guardd` is available, the daemon performs the evaluation and returns a structured decision.
If the daemon is unavailable, `guardctl` falls back to local evaluation in-process.
If both paths fail, the command degrades to `allow_alert`.

This is the intended fail-open behavior:
the guard should not halt the cluster simply because the guard itself is unhealthy.

### Epilog

At job end, Slurm invokes `guardctl epilog`.
The epilog path follows the same daemon-first, local-fallback execution model, but evaluates post-job conditions instead of only startup conditions.

Epilog may still drain a node, but requeue is only appropriate when the result is clearly infrastructure-related and the policy explicitly allows it.

## Health Check Execution Model

Checks are implemented as external executable plugins.
Each plugin receives a JSON request on `stdin` and returns a JSON result on `stdout`.
This keeps the core binary small and stable while allowing sites to add or replace checks without recompiling the project.

Plugins are selected by phase.
For example, a GPU presence check may run in `prolog`, while an RDMA degradation check may run in both `prolog` and `epilog`.

Checks are executed concurrently and bounded by per-phase timeouts from policy.
This keeps Prolog latency low and prevents a single misbehaving plugin from blocking the entire hook indefinitely.

If a plugin exits non-zero or times out, the core converts that outcome into a `status=error` result.
Plugin execution failure is treated as an internal tool failure signal, not direct evidence that the node itself is broken.

## Failure Semantics

The design separates two concerns:

- mechanism: gather facts from plugins
- policy: decide what those facts mean operationally

Plugins emit one of four statuses:

- `pass`
- `warn`
- `fail`
- `error`

The policy engine converts check results into a final verdict:

- `allow`
- `allow_alert`
- `drain_after_job`
- `block_drain`
- `block_drain_requeue`

These verdicts are intentionally explicit.
They describe both scheduling impact and remediation scope.

### Requeue Boundary

`block_drain_requeue` is reserved for failures with explicit infrastructure evidence.
Examples include inaccessible GPUs, fatal XID-like conditions, filesystem health failures, or link-level failures that are known to invalidate the node as a scheduling target.

Ambiguous symptoms, application failures, or plugin execution problems must not trigger automatic requeue by default.
This keeps job retry behavior aligned with infrastructure responsibility rather than application semantics.

## Policy Model

The site-facing policy surface is declarative YAML.
This keeps site policy outside the core mechanism and makes OSS maintenance simpler.

The policy currently defines:

- per-phase check timeouts
- failure-domain rules
- severity labels
- drain reason templates
- verdict mappings for `warn` and `fail`
- whether infrastructure evidence is required
- notification receivers

This model lets operators change remediation behavior without modifying plugin code or rebuilding binaries.

## Slurm Integration

Slurm interaction is intentionally narrow and idempotent.
The core applies two actions:

- drain node
- requeue job

Both are executed through `scontrol`.
The action layer treats known "already applied" conditions as non-fatal, which makes repeated evaluation safer under retries or repeated hook invocation.

This is important because the control path may be re-entered after partial failures or hook retries, and idempotent behavior reduces operational risk.

## Observability

The project emits structured behavior at three levels:

- JSON logs through the standard logger
- OpenTelemetry traces and metrics
- notifications to webhooks or external commands

OpenTelemetry is initialized only when explicitly enabled.
This keeps the default runtime lightweight while preserving an integration path for environments that standardize on OTel.

Notifications are intentionally generic.
The core emits an event payload and leaves the final alert routing to site-specific webhook receivers or command adapters.

## Safety Properties

The most important safety properties in v1 are:

- fail-open when the guard itself is broken
- fail-fast for high-confidence infrastructure failures
- no automatic requeue without explicit infrastructure evidence
- no mandatory cluster-wide service dependency
- no heavyweight diagnostics in the job-start critical path

These choices favor predictable cluster behavior over aggressive automation.

## Current Scope and Limitations

v1 is intentionally narrow.
It focuses on per-job health evaluation and remediation hooks.
It does not include:

- periodic autonomous node health loops
- a centralized remediation service
- vendor-specific deep diagnostics
- historical trend analysis
- node repair orchestration

Those capabilities can be added later without changing the core public interfaces, because the current design already separates plugin execution, policy evaluation, and Slurm action application.

## Public Interfaces

The stable public interfaces in v1 are:

- `guardctl` CLI
- plugin JSON request and response contract
- YAML policy contract

The daemon socket protocol is considered internal.
It may evolve without the same compatibility guarantees as the external CLI and file-based interfaces.

## Testing Strategy

The implementation is backed by tests for:

- policy evaluation and verdict conversion
- plugin success, failure, and timeout normalization
- daemon/client fallback behavior
- Slurm action idempotency handling
- config loading and notification dispatch

This ensures the most important safety and semantics boundaries are verified in code.

## Reference

- [HPCA2025] "Revisiting Reliability in Large-Scale Machine Learning Research Clusters"
