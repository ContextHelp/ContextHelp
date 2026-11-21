#!/usr/bin/env bash
# Print the packages whose tests only compile under the integration tag
# (a test file with `//go:build integration`), as ./relative paths, minus
# the packages named as arguments, which have their own CI step (they run a
# subset by -run name, or need extra tags). A new integration-tagged suite
# is picked up without editing the Makefile, and no package runs twice.
#
#   scripts/ci/integration-pkgs.sh ./internal/jobs ./internal/server/http
#
# Fails when nothing is left, so a broken discovery cannot pass as an empty
# run.
set -euo pipefail

cd "$(dirname "$0")/../.."

tags=fts5
fmt='{{.ImportPath}}|{{join .TestGoFiles ","}}|{{join .XTestGoFiles ","}}'
base=$(mktemp)
tagged=$(mktemp)
trap 'rm -f "$base" "$tagged"' EXIT

go list -tags "$tags" -f "$fmt" ./... | sort >"$base"
go list -tags "$tags integration" -f "$fmt" ./... | sort >"$tagged"
mod=$(go list -m)

excluded=()
for p in "$@"; do excluded+=("$mod/${p#./}"); done

found=0
# Lines only in the tagged listing: the package gained test files.
while IFS='|' read -r pkg _; do
	skip=0
	for e in "${excluded[@]:-}"; do [ "$pkg" = "$e" ] && skip=1; done
	[ "$skip" = 1 ] && continue
	echo "./${pkg#"$mod"/}"
	found=1
done < <(comm -13 "$base" "$tagged")

if [ "$found" = 0 ]; then
	echo "integration-pkgs: no integration-tagged packages found" >&2
	exit 1
fi
