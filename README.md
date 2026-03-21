# slurm-gpu-node-guard

`slurm-gpu-node-guard` is a node-local health guard for Slurm-based GPU clusters. It runs `guardctl` from Slurm `Prolog`/`Epilog` hooks and keeps `guardd` resident on each node, providing lightweight pre-job checks, post-job cleanup, structured notifications, and consistent `drain`/`requeue` semantics.

## Design Principles

- **Low-latency Prolog**: Plugins run concurrently with per-phase timeouts.
- **Separation of failure semantics**: Plugins report facts; the final verdict is determined by YAML policy.
- **Strict requeue boundary**: `block_drain_requeue` only applies when `infra_evidence=true`.
- **Fail-open**: When the daemon is unreachable or an internal error occurs, the system falls back to `allow_alert` so the guard itself never blocks the entire cluster.
- **OSS extensibility**: Checks can be added as external executable plugins.

## Components

- `cmd/guardctl`: `prolog`, `epilog`, `check run`, `report event`
- `cmd/guardd`: Local evaluation API over a UNIX domain socket
- `internal/policy`: Maps `pass|warn|fail|error` to verdicts
- `internal/plugin`: External plugin JSON contract
- `internal/slurm`: `drain`/`requeue` via `scontrol`
- `internal/notify`: Webhook and command-based notifications

## Plugin Contract

Plugins receive a JSON request on stdin:

```json
{
  "phase": "prolog",
  "job_context": {
    "id": "12345",
    "node_name": "gpu-a01"
  },
  "node_context": {
    "name": "gpu-a01"
  },
  "timeouts": {
    "prolog": "1.5s"
  }
}
```

Plugins return a JSON response on stdout:

```json
{
  "check_name": "gpu-presence",
  "status": "fail",
  "failure_domain": "gpu",
  "infra_evidence": true,
  "summary": "GPU not accessible",
  "structured_cause": "gpu_missing"
}
```

A non-zero exit code is treated as an internal plugin error and converted to `status=error`.

## Configuration

See [configs/policy.example.yaml](configs/policy.example.yaml) for a sample configuration. The policy defines per-phase timeouts, per-failure-domain verdicts, drain reason templates, and notification receivers.

## Usage

```bash
go build ./cmd/guardctl
go build ./cmd/guardd

./guardd -config ./configs/policy.example.yaml
./guardctl prolog -config ./configs/policy.example.yaml
./guardctl epilog -config ./configs/policy.example.yaml
./guardctl check run -config ./configs/policy.example.yaml -phase prolog
./guardctl report event -config ./configs/policy.example.yaml --receivers default --summary "manual remediation"
```

## OpenTelemetry

Set `SGNG_OTEL_STDOUT=true` to emit traces and metrics via the stdout exporter. When unset, no OTel provider is initialized.

## References

- [HPCA2025] Revisiting Reliability in Large-Scale Machine Learning Research Clusters
