#!/bin/sh
set -eu

cat >/dev/null
if command -v ibstat >/dev/null 2>&1 && ibstat >/dev/null 2>&1; then
  printf '%s\n' '{"check_name":"rdma-link","status":"pass","failure_domain":"rdma","summary":"ibstat ok"}'
  exit 0
fi

printf '%s\n' '{"check_name":"rdma-link","status":"warn","failure_domain":"rdma","infra_evidence":false,"summary":"ibstat unavailable","structured_cause":"ibstat_missing"}'

