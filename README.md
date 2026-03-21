# slurm-gpu-node-guard

`slurm-gpu-node-guard` は Slurm GPU クラスタ向けのノードローカル健全性ガードです。`guardctl` を Slurm `Prolog`/`Epilog` から呼び出し、`guardd` を各ノードで常駐させることで、軽量なジョブ開始前チェック、ジョブ終了時 cleanup、構造化通知、`drain`/`requeue` を一貫した意味論で扱います。

## 設計の要点

- 低遅延 Prolog: 各プラグインは phase ごとの timeout 付きで並列実行されます。
- failure semantics の分離: プラグインは事実を返し、最終 verdict は YAML policy が決めます。
- 厳格な requeue 境界: `block_drain_requeue` は `infra_evidence=true` のときだけ成立します。
- fail-open: daemon 不達や内部エラー時は `allow_alert` に落とし、guard 自身の故障でクラスター全体を止めません。
- OSS 拡張性: チェックは外部実行プラグインとして追加できます。

## コンポーネント

- `cmd/guardctl`: `prolog`、`epilog`、`check run`、`report event`
- `cmd/guardd`: UNIX domain socket 経由のローカル evaluate API
- `internal/policy`: `pass|warn|fail|error` から verdict への写像
- `internal/plugin`: 外部プラグイン JSON 契約
- `internal/slurm`: `scontrol` による `drain`/`requeue`
- `internal/notify`: webhook / コマンド通知

## プラグイン契約

plugin には stdin JSON で次を渡します。

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

plugin は stdout JSON で次を返します。

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

非 0 exit は plugin 内部 error として `status=error` に変換されます。

## 設定

サンプルは [configs/policy.example.yaml](/Users/y-tsubouchi/src/github.com/yuuki/slurm-gpu-node-guard/configs/policy.example.yaml) を参照してください。policy では phase timeout、failure-domain ごとの verdict、drain reason template、通知先を定義します。

## 使い方

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

`SGNG_OTEL_STDOUT=true` を設定すると、trace / metric を stdout exporter で出力します。未設定時は OTel provider を初期化しません。

## 参考

- [HPCA2025] Revisiting Reliability in Large-Scale Machine Learning Research Clusters
