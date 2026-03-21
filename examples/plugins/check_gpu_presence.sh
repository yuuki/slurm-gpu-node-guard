#!/bin/sh
set -eu

cat >/dev/null
if command -v nvidia-smi >/dev/null 2>&1 && nvidia-smi -L >/dev/null 2>&1; then
  printf '%s\n' '{"check_name":"gpu-presence","status":"pass","failure_domain":"gpu","summary":"nvidia-smi ok"}'
  exit 0
fi

printf '%s\n' '{"check_name":"gpu-presence","status":"fail","failure_domain":"gpu","infra_evidence":true,"summary":"GPU not accessible","structured_cause":"gpu_missing"}'

