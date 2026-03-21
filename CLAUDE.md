# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

Slurm GPU クラスタ向けのノードローカル健全性ガード。Slurm の Prolog/Epilog フックからプラグインベースのヘルスチェックを実行し、YAML ポリシーに基づいて drain/requeue を自動判定する。

## Build & Test

```bash
# ビルド
go build ./cmd/guardd
go build ./cmd/guardctl

# テスト (全体)
go test ./...

# 単一パッケージのテスト
go test ./internal/policy/
go test ./internal/plugin/ -run TestRunnerTimeout

# vet
go vet ./...
```

Makefile や CI 設定は未整備。`go build` / `go test` が基本コマンド。

## Architecture

2つの実行バイナリと6つの internal パッケージで構成される。

### Executables

- **`cmd/guardd`** — UNIX domain socket 上で HTTP API (`/v1/evaluate`, `/healthz`) を提供するノードローカルデーモン
- **`cmd/guardctl`** — Slurm Prolog/Epilog から呼ばれる CLI。サブコマンド: `prolog`, `epilog`, `check run`, `report event`

### Internal packages

- **`engine`** — プラグイン実行とポリシー評価を統合するオーケストレータ。プラグインは goroutine で並列実行され、phase timeout で制限される
- **`policy`** — CheckResult (pass/warn/fail/error) を Verdict (allow/allow_alert/drain_after_job/block_drain/block_drain_requeue) に変換。verdict は priority 順で最も重い結果が採用される
- **`plugin`** — 外部実行ファイルを subprocess で実行し、stdin JSON → stdout JSON 契約でやりとりする Runner
- **`slurm`** — `scontrol` による drain/requeue。冪等性を保証（already applied は非致命エラー）
- **`notify`** — webhook または外部コマンドによる通知ディスパッチ
- **`config`** — YAML 設定ロード。`policy.Policy` + `notify.Config` + プラグイン定義を統合
- **`app`** — daemon-first / local-fallback の評価フロー (`EvaluateWithFallback`)
- **`model`** — 全パッケージが共有する型定義 (Phase, CheckStatus, Verdict, FailureDomain 等)
- **`telemetry`** — OTel provider の初期化 (`SGNG_OTEL_STDOUT=true` で有効化)
- **`daemon`** — Server (UNIX socket HTTP) と Client (daemon 通信) の両方を含む

### Key data flow

```
guardctl prolog
  → config.Load
  → daemon.Client.Evaluate (失敗時 → engine.Evaluate にフォールバック)
    → engine.RunChecks (外部プラグイン並列実行)
    → policy.Evaluate (CheckResult[] → EvaluationDecision)
  → slurm.ApplyDecision (drain/requeue)
  → notify.Manager.Notify
```

### Critical invariants

- **fail-open**: daemon 不達・内部エラー時は `allow_alert` にフォールバック。guard 故障でクラスタを止めない
- **requeue boundary**: `block_drain_requeue` は `infra_evidence=true` のときだけ成立。証拠なしなら `block_drain` に降格
- **冪等性**: Slurm アクション (drain/requeue) は既適用状態を非致命エラーとして扱う

## Configuration

サンプル: `configs/policy.example.yaml`。主要セクション:
- `socket_path` — daemon の UNIX socket パス
- `plugins` — name, path, phases を定義
- `check_timeouts` — phase ごとのプラグイン実行タイムアウト
- `domains` — failure domain ごとの severity, verdict マッピング, drain reason template
- `notifications.receivers` — webhook URL またはコマンド

## Commit style

短いスコープ付き subject を使う: `feat:`, `fix:`, `docs:`, `refactor:`, `test:`
