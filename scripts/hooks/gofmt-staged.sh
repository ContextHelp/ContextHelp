#!/usr/bin/env bash
# Fail if a staged Go file is not gofmt-clean AND was not already broken.
#
# Mirrors the CI step `if [ -n "$(gofmt -l .)" ]; then ... fi`, but scoped so
# the repo's pre-existing backlog (184 files fail `gofmt -l .` today) does not
# block every commit. See .pre-commit-config.yaml for the rationale.
#
# A staged file fails only when it is unformatted now and was formatted — or
# absent — at HEAD. That catches regressions and new files while letting you
# edit an already-unformatted file without being made responsible for it.
set -euo pipefail

[ "$#" -eq 0 ] && exit 0

# is_unformatted <file>   — the working-tree copy
is_unformatted() {
	[ -n "$(gofmt -l -- "$1" 2>/dev/null)" ]
}

# was_unformatted_at_head <file> — the committed copy, if any
was_unformatted_at_head() {
	git cat-file -e "HEAD:$1" 2>/dev/null || return 1
	[ -n "$(git show "HEAD:$1" 2>/dev/null | gofmt -l /dev/stdin 2>/dev/null)" ]
}

regressions=()
for f in "$@"; do
	is_unformatted "$f" || continue
	was_unformatted_at_head "$f" && continue
	regressions+=("$f")
done

if [ "${#regressions[@]}" -gt 0 ]; then
	echo "Not gofmt-clean (newly introduced):" >&2
	printf '  %s\n' "${regressions[@]}" >&2
	echo >&2
	echo "Fix with: gofmt -w ${regressions[*]}" >&2
	exit 1
fi
