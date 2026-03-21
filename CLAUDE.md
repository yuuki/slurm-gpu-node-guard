# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & test commands

```bash
go build ./cmd/guardd && go build ./cmd/guardctl
go test ./...
go test ./internal/policy/ -run TestEvaluate   # single test
go vet ./...
```

No Makefile or CI pipeline. `go build` and `go test` are the only commands needed.

## Architecture

Two binaries: `guardd` (node-local daemon, UNIX socket HTTP) and `guardctl` (CLI called from Slurm Prolog/Epilog).

Data flow for `guardctl prolog`:

```
config.Load -> daemon.Client.Evaluate (fallback: engine.Evaluate in-process)
  -> engine.RunChecks (plugins run concurrently, bounded by per-phase timeout)
  -> policy.Evaluate (CheckResult[] -> highest-priority Verdict)
-> slurm.ApplyDecision (drain/requeue via scontrol)
-> notify.Manager.Notify (webhook or command)
```

Key design decisions that aren't obvious from reading individual files:

- **Fail-open**: If the daemon is unreachable or any internal error occurs, the system falls back to `allow_alert`. The guard must never block the cluster due to its own failure.
- **Requeue boundary**: `block_drain_requeue` requires `infra_evidence=true` from the plugin. Without evidence it downgrades to `block_drain`. This is enforced in `internal/policy/evaluator.go` and is the most important safety invariant.
- **Idempotent Slurm actions**: `internal/slurm/action.go` treats "already drained" / "invalid job id" as non-fatal (`ErrAlreadyDone`), so repeated evaluation is safe.
- **Plugin contract**: Plugins are external executables receiving JSON on stdin and returning JSON on stdout. Non-zero exit becomes `status=error`. See @README.md for the full schema.

## Code style

- Go 1.25+. Use `log/slog` for logging (JSON handler to stderr).
- Errors wrap with `fmt.Errorf("context: %w", err)`.
- OpenTelemetry is opt-in via `SGNG_OTEL_STDOUT=true`. Don't initialize OTel unconditionally.

## Commit style

Short scoped subjects: `feat:`, `fix:`, `docs:`, `refactor:`, `test:`
