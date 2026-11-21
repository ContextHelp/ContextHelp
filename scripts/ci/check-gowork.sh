#!/usr/bin/env bash
# Fail when a tracked go.work has a `use` that is an absolute path or that
# resolves outside the repository. Such a workspace builds on one machine
# only, and hides a module that GOWORK=off (CI) cannot resolve.
set -euo pipefail

cd "$(dirname "$0")/../.."
root=$(pwd -P)
fail=0

while IFS= read -r f; do
	[ -n "$f" ] || continue
	dir=$(dirname "$f")
	while IFS= read -r p; do
		[ -n "$p" ] || continue
		case $p in
		/* | ~* | [A-Za-z]:[\\/]*)
			echo "FAIL: $f: use $p is an absolute path" >&2
			fail=1
			continue
			;;
		esac
		# Resolve .. and . by hand: the target need not exist, and python or
		# realpath -m are not on every runner.
		abs=$(cd "$dir" && pwd -P)
		IFS=/ read -ra parts <<<"$p"
		for seg in "${parts[@]}"; do
			case $seg in
			"" | .) ;;
			..) abs=${abs%/*} ;;
			*) abs=$abs/$seg ;;
			esac
		done
		case $abs/ in
		"$root"/*) ;;
		*)
			echo "FAIL: $f: use $p resolves outside the repository" >&2
			fail=1
			;;
		esac
	done < <(awk '
		/^[ \t]*\/\// { next }
		/^[ \t]*use[ \t]*\(/ { inblk = 1; next }
		inblk && /^[ \t]*\)/ { inblk = 0; next }
		inblk { l = $0; sub(/\/\/.*/, "", l); gsub(/^[ \t]+|[ \t]+$/, "", l); gsub(/"/, "", l); if (l != "") print l; next }
		/^[ \t]*use[ \t]+/ { l = $0; sub(/^[ \t]*use[ \t]+/, "", l); sub(/\/\/.*/, "", l); gsub(/^[ \t]+|[ \t]+$/, "", l); gsub(/"/, "", l); print l }
	' "$f")
done < <(git ls-files -- 'go.work' '**/go.work')

[ "$fail" = 0 ] && echo "ok: tracked go.work files use only in-repo relative paths"
exit "$fail"
