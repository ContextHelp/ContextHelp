#!/usr/bin/env bash
# Run golangci-lint over the staged packages, reporting only NEW findings.
#
# Same linter version and config as CI's lint job (make ci-lint-full), with
# two deliberate differences that keep it fast enough for a commit hook:
#
#   1. Scoped to the packages holding the staged files, not ./...
#   2. --new-from-rev=HEAD, so only findings this commit introduces fail the
#      hook. The full scan in CI catches anything this scope misses.
#
# See .pre-commit-config.yaml.
#
# golangci-lint is the mise.toml pin, resolved by scripts/ci/tools.sh. When
# it is missing or another version comes first on PATH the hook FAILS: a
# different version's findings are not evidence about CI, in either
# direction.
set -euo pipefail

[ "$#" -eq 0 ] && exit 0

tools="$(git rev-parse --show-toplevel)/scripts/ci/tools.sh"
"$tools" golangci-lint,go

# module_root prints the directory of the go.mod nearest to $1.
module_root() {
	local d=$1
	while [ "$d" != "." ] && [ ! -f "$d/go.mod" ]; do
		d=$(dirname "$d")
	done
	printf '%s\n' "$d"
}

# in_workspace reports whether module dir $1 is a `use` entry of go.work.
in_workspace() {
	[ -f go.work ] && grep -Eq "^[[:space:]]*(use[[:space:]]+)?\./$1[[:space:]]*$" go.work
}

# Packages in nested modules (a directory with its own go.mod) outside go.work are not
# part of the build: golangci-lint run from the root cannot type-check
# them, and CI's `golangci-lint run ./...` never lints them either. Skip
# them here too.
pkgs=$(for f in "$@"; do
	dir=$(dirname "$f")
	root=$(module_root "$dir")
	if [ "$root" != "." ] && ! in_workspace "$root"; then
		echo "golangci-staged: skipping $dir (nested module)" >&2
		continue
	fi
	printf './%s\n' "$dir"
done | sort -u)

[ -z "$pkgs" ] && exit 0

# Drop packages the lint tag set (.golangci.yml run.build-tags; keep in
# sync) excludes entirely, e.g. test/e2e/federation (federation_e2e
# only). `golangci-lint run ./...` skips them; naming one fails the
# type check with "build constraints exclude all Go files".
# shellcheck disable=SC2086 # deliberate word-splitting of the package list
pkgs=$(CGO_ENABLED=1 "$tools" go -- go list -e -tags integration,e2e,fts5,watch_e2e \
	-f '{{if or .GoFiles .TestGoFiles .XTestGoFiles}}{{.Dir}}{{end}}' $pkgs)
[ -z "$pkgs" ] && exit 0

# shellcheck disable=SC2086 # deliberate word-splitting of the package list
"$tools" golangci-lint,go -- golangci-lint run --timeout=10m --new-from-rev=HEAD $pkgs
