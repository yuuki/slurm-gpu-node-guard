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
- `cmd/guard-plugin-gpu`: External GPU health plugin using `nvidia-smi`
- `cmd/guard-plugin-gpu-errors`: External GPU error plugin using `nvidia-smi -q -x` and `journalctl`
- `cmd/guard-plugin-rdma`: External RDMA health plugin using `ibstat`
- `cmd/guard-plugin-filesystem`: External filesystem health plugin using `findmnt`, `stat`, and `journalctl`
- `cmd/guard-plugin-service`: External service health plugin using `systemctl`
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
  "plugin_config": {
    "required_mounts": ["/home", "/datasets"]
  },
  "timeouts": {
    "prolog": "1.5s"
  }
}
```

Plugins return a JSON response on stdout:

```json
{
  "check_name": "filesystem-health",
  "status": "fail",
  "failure_domain": "filesystem",
  "infra_evidence": true,
  "summary": "required mount missing: /datasets",
  "structured_cause": "mount_missing"
}
```

A non-zero exit code is treated as an internal plugin error and converted to `status=error`.

## Configuration

See [configs/policy.example.yaml](configs/policy.example.yaml) for a sample configuration. The policy defines per-phase timeouts, per-failure-domain verdicts, drain reason templates, and notification receivers.

The sample configuration assumes the external plugins are installed at:

- `/usr/local/libexec/slurm-gpu-node-guard/guard-plugin-gpu`
- `/usr/local/libexec/slurm-gpu-node-guard/guard-plugin-rdma`

## Usage

```bash
install -d /usr/local/libexec/slurm-gpu-node-guard
go build -o /usr/local/bin/guardctl ./cmd/guardctl
go build -o /usr/local/bin/guardd ./cmd/guardd
go build -o /usr/local/libexec/slurm-gpu-node-guard/guard-plugin-gpu ./cmd/guard-plugin-gpu
go build -o /usr/local/libexec/slurm-gpu-node-guard/guard-plugin-gpu-errors ./cmd/guard-plugin-gpu-errors
go build -o /usr/local/libexec/slurm-gpu-node-guard/guard-plugin-rdma ./cmd/guard-plugin-rdma
go build -o /usr/local/libexec/slurm-gpu-node-guard/guard-plugin-filesystem ./cmd/guard-plugin-filesystem
go build -o /usr/local/libexec/slurm-gpu-node-guard/guard-plugin-service ./cmd/guard-plugin-service

guardd -config ./configs/policy.example.yaml
guardctl prolog -config ./configs/policy.example.yaml
guardctl epilog -config ./configs/policy.example.yaml
guardctl check run -config ./configs/policy.example.yaml -phase prolog
guardctl report event -config ./configs/policy.example.yaml --receivers default --summary "manual remediation"
```

## Slurm Configuration

Add the following to `slurm.conf` to invoke `guardctl` from Slurm's Prolog/Epilog hooks:

```conf
Prolog=/usr/local/bin/guardctl prolog -config /etc/slurm-gpu-node-guard/policy.yaml
Epilog=/usr/local/bin/guardctl epilog -config /etc/slurm-gpu-node-guard/policy.yaml
```

If `guardd` is running on each node, `guardctl` will connect to it via the UNIX domain socket. If the daemon is unreachable, `guardctl` falls back to in-process evaluation (fail-open).

To start `guardd` as a systemd service, install [configs/slurm-node-guardd.service](configs/slurm-node-guardd.service) and enable it:

```bash
sudo cp configs/slurm-node-guardd.service /etc/systemd/system/slurm-node-guardd.service
sudo systemctl daemon-reload
sudo systemctl enable --now slurm-node-guardd
```

## OpenTelemetry

Set `SGNG_OTEL_STDOUT=true` to emit traces and metrics via the stdout exporter. When unset, no OTel provider is initialized.

## References

- [HPCA2025] Revisiting Reliability in Large-Scale Machine Learning Research Clusters
