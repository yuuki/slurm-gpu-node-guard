# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

A node-local health guard for Slurm-based GPU clusters. It runs plugin-based health checks from Slurm Prolog/Epilog hooks and automatically determines drain/requeue actions based on YAML policy.

## Build & Test

```bash
# Build
go build ./cmd/guardd
go build ./cmd/guardctl

# Run all tests
go test ./...

# Run tests for a single package
go test ./internal/policy/
go test ./internal/plugin/ -run TestRunnerTimeout

# Vet
go vet ./...
```

No Makefile or CI pipeline yet. `go build` and `go test` are the primary commands.

## Architecture

The project consists of two executables and several internal packages.

### Executables

- **`cmd/guardd`** — Node-local daemon serving an HTTP API (`/v1/evaluate`, `/healthz`) over a UNIX domain socket
- **`cmd/guardctl`** — CLI invoked by Slurm Prolog/Epilog. Subcommands: `prolog`, `epilog`, `check run`, `report event`

### Internal packages

- **`engine`** — Orchestrator that integrates plugin execution and policy evaluation. Plugins run concurrently in goroutines, bounded by per-phase timeouts
- **`policy`** — Converts CheckResult (pass/warn/fail/error) into Verdict (allow/allow_alert/drain_after_job/block_drain/block_drain_requeue). The highest-priority verdict wins
- **`plugin`** — Runner that executes external binaries as subprocesses using a stdin JSON / stdout JSON contract
- **`slurm`** — Drain/requeue via `scontrol`. Idempotent: "already applied" conditions are treated as non-fatal
- **`notify`** — Dispatches notifications via webhooks or external commands
- **`config`** — Loads YAML configuration, combining `policy.Policy`, `notify.Config`, and plugin definitions
- **`app`** — Daemon-first / local-fallback evaluation flow (`EvaluateWithFallback`)
- **`model`** — Shared type definitions (Phase, CheckStatus, Verdict, FailureDomain, etc.)
- **`telemetry`** — OTel provider initialization (enabled with `SGNG_OTEL_STDOUT=true`)
- **`daemon`** — Contains both the Server (UNIX socket HTTP) and Client (daemon communication)

### Key data flow

```
guardctl prolog
  -> config.Load
  -> daemon.Client.Evaluate (falls back to engine.Evaluate on failure)
    -> engine.RunChecks (concurrent external plugin execution)
    -> policy.Evaluate (CheckResult[] -> EvaluationDecision)
  -> slurm.ApplyDecision (drain/requeue)
  -> notify.Manager.Notify
```

### Critical invariants

- **Fail-open**: Falls back to `allow_alert` when the daemon is unreachable or an internal error occurs. The guard never blocks the cluster due to its own failure
- **Requeue boundary**: `block_drain_requeue` requires `infra_evidence=true`. Without evidence, it downgrades to `block_drain`
- **Idempotency**: Slurm actions (drain/requeue) treat "already applied" states as non-fatal errors

## Configuration

Sample: `configs/policy.example.yaml`. Key sections:
- `socket_path` — UNIX socket path for the daemon
- `plugins` — Plugin name, path, and phases
- `check_timeouts` — Per-phase plugin execution timeouts
- `domains` — Per-failure-domain severity, verdict mappings, and drain reason templates
- `notifications.receivers` — Webhook URLs or commands

## Commit style

Use short, scoped subjects: `feat:`, `fix:`, `docs:`, `refactor:`, `test:`
