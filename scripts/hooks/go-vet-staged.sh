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
# Directories belonging to a NESTED module are dropped. The repo carries six
# of them under plugins/ (each with its own go.mod), and vetting one from the
# root module fails with "main module does not contain package" — a staged
# file there would block every commit. Same for build-tagged trees the
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

# shellcheck disable=SC2086 # deliberate word-splitting of the package list
CGO_ENABLED=1 go vet -tags fts5 $pkgs
