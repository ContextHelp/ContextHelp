#!/usr/bin/env bash
# Run all eva contracts in contracts/*.eva.yaml against their recorded
# fixtures under test/integration/testdata/eva-fixtures/. Used by
# `make eva` and the eva-contracts CI workflow.
#
# Each contract is paired with one or more fixture files following the
# convention: a fixture matches a contract when the fixture filename
# starts with the contract's basename (sans `.eva.yaml`). Example:
#   contracts/healthz.eva.yaml
#     ↳ test/integration/testdata/eva-fixtures/healthz-healthy.json
#     ↳ test/integration/testdata/eva-fixtures/healthz-upgrading.json
#
# Exit codes:
#   0 — every fixture passes its contract.
#   1 — at least one fixture failed validation.
#   2 — eva CLI not on PATH or missing fixtures (operator error, not a
#       contract regression). The CI gate also exits 2 in this case so
#       missing eva is a hard fail rather than a silent skip.
set -euo pipefail

# Resolve repo root from this script's location.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

CONTRACTS_DIR="${REPO_ROOT}/contracts"
FIXTURES_DIR="${REPO_ROOT}/test/integration/testdata/eva-fixtures"

if [ ! -d "${CONTRACTS_DIR}" ]; then
  echo "ERROR: contracts directory not found: ${CONTRACTS_DIR}" >&2
  exit 2
fi
if [ ! -d "${FIXTURES_DIR}" ]; then
  echo "ERROR: fixtures directory not found: ${FIXTURES_DIR}" >&2
  exit 2
fi

# Locate eva. Prefer EVA_BIN (CI override), fall back to PATH.
EVA_BIN="${EVA_BIN:-$(command -v eva || true)}"
if [ -z "${EVA_BIN}" ]; then
  echo "ERROR: eva CLI not found on PATH; install hop.top/eva or set EVA_BIN" >&2
  echo "  pip install eva  # or: pipx install eva" >&2
  exit 2
fi

# `eva run --contract <yaml> --input <json>` is the standalone-contract
# mode (no agent call, no gateway). Added in eva HEAD past v0.1.0a1; the
# released tag exposes only `eva run --dataset` mode. CI installs from a
# pinned commit until the next eva tag ships the standalone-mode CLI.
fail=0
total=0
for contract in "${CONTRACTS_DIR}"/*.eva.yaml; do
  base="$(basename "${contract}" .eva.yaml)"
  matched=0
  for fixture in "${FIXTURES_DIR}/${base}"*.json; do
    if [ ! -f "${fixture}" ]; then
      # No fixtures matched the glob — skip to outer error below.
      continue
    fi
    matched=1
    total=$((total + 1))
    fixture_name="$(basename "${fixture}")"
    printf '%-48s %-40s ' "${base}.eva.yaml" "${fixture_name}"
    if "${EVA_BIN}" run \
         --contract "${contract}" \
         --input "${fixture}" \
         --format json \
         --quiet \
         >/dev/null 2>/tmp/eva-run-err.$$; then
      echo "PASS"
    else
      echo "FAIL"
      cat /tmp/eva-run-err.$$ >&2 || true
      fail=$((fail + 1))
    fi
    rm -f /tmp/eva-run-err.$$
  done
  if [ "${matched}" -eq 0 ]; then
    echo "ERROR: no fixtures matched ${base}.eva.yaml under ${FIXTURES_DIR}" >&2
    exit 2
  fi
done

echo
if [ "${fail}" -gt 0 ]; then
  echo "FAILED  ${fail}/${total} fixture(s) regressed"
  exit 1
fi
echo "PASSED  ${total}/${total} fixture(s)"
