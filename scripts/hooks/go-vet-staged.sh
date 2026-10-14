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
#
# Directories belonging to a NESTED module (a directory with its own go.mod)
# are dropped: vetting one from the root module fails with "main module does
# not contain package", so a staged file there would block every commit. Same for build-tagged trees the
# default tag set cannot load. Walk up to the nearest go.mod and keep only
# the paths this module actually owns.
root_mod=$(cd "$(git rev-parse --show-toplevel)" && pwd)
owned=""
for f in "$@"; do
	d=$(dirname "$f")
	probe="$d"
	while [ "$probe" != "." ] && [ "$probe" != "/" ]; do
		[ -f "$probe/go.mod" ] && break
		probe=$(dirname "$probe")
	done
	# Owned by the root module only when no nearer go.mod was found.
	if [ ! -f "$probe/go.mod" ] || [ "$(cd "$probe" && pwd)" = "$root_mod" ]; then
		owned="$owned ./$d"
	fi
done

pkgs=$(printf '%s\n' $owned | sort -u)
[ -z "$pkgs" ] && exit 0

# e2e and watch_e2e join fts5 so the black-box suites under test/e2e/
# that carry only one of those tags (and are linted with them, see
# .golangci.yml build-tags) load as packages instead of failing with
# "build constraints exclude all Go files"; they select no file in any
# other package.
tags=fts5,e2e,watch_e2e

# Drop packages this tag set excludes entirely (test/e2e/federation
# carries only federation_e2e). `go vet ./...` skips them the same way;
# naming one fails with "build constraints exclude all Go files".
# shellcheck disable=SC2086 # deliberate word-splitting of the package list
pkgs=$(CGO_ENABLED=1 go list -e -tags "$tags" \
	-f '{{if or .GoFiles .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' $pkgs)
[ -z "$pkgs" ] && exit 0

# shellcheck disable=SC2086 # deliberate word-splitting of the package list
CGO_ENABLED=1 go vet -tags "$tags" $pkgs
