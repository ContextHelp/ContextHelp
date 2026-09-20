#!/usr/bin/env bash
# Run `go vet` over the packages containing the staged Go files.
#
# Mirrors the CI step `go vet -tags fts5 ./...`. Vet analyses whole packages,
# not single files, so the staged file list is mapped to its directories.
#
# The fts5 tag is mandatory: without it the build fails on a deliberate
# tripwire in internal/storage/sqlite. See DEVELOPING.md.
set -euo pipefail

[ "$#" -eq 0 ] && exit 0

# Unique parent directories of the staged files, as ./pkg/path arguments.
pkgs=$(for f in "$@"; do printf './%s\n' "$(dirname "$f")"; done | sort -u)

# shellcheck disable=SC2086 # deliberate word-splitting of the package list
CGO_ENABLED=1 go vet -tags fts5 $pkgs
