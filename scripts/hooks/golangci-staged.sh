#!/usr/bin/env bash
# Run golangci-lint over the staged packages, reporting only NEW findings.
#
# Mirrors the CI step `golangci-lint run --timeout=5m`, with two deliberate
# differences that make it usable as a commit hook:
#
#   1. Scoped to the packages holding the staged files, not ./...
#   2. --new-from-rev=HEAD, so the large pre-existing backlog is filtered out
#      and only findings this commit introduces fail the hook.
#
# Without (2) the hook fails on any file that already has findings — which is
# most of them — and gets switched off. See .pre-commit-config.yaml.
set -euo pipefail

[ "$#" -eq 0 ] && exit 0

# Keep this in sync with .github/workflows/ci.yml.
GOLANGCI_VERSION=v2.1.6

if ! command -v golangci-lint >/dev/null 2>&1; then
	echo "golangci-lint not found; skipping." >&2
	echo "Install the CI-pinned version:" >&2
	echo "  go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_VERSION}" >&2
	exit 0
fi

have=$(golangci-lint version 2>&1 | grep -oE 'v?2\.[0-9]+\.[0-9]+' | head -1 || true)
if [ -n "$have" ] && [ "v${have#v}" != "$GOLANGCI_VERSION" ]; then
	echo "warning: golangci-lint ${have} differs from the CI pin ${GOLANGCI_VERSION};" >&2
	echo "         findings may not match CI." >&2
fi

pkgs=$(for f in "$@"; do printf './%s\n' "$(dirname "$f")"; done | sort -u)

# shellcheck disable=SC2086 # deliberate word-splitting of the package list
golangci-lint run --timeout=5m --new-from-rev=HEAD $pkgs
