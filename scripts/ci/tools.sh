#!/usr/bin/env bash
# Run a command with the tool versions this repo pins, or fail loudly.
#
#   scripts/ci/tools.sh <tool>[,<tool>...] -- <command> [args...]
#   scripts/ci/tools.sh <tool>[,<tool>...]          # check only
#
# Tools: go (gofmt ships with it), golangci-lint, gitleaks, node and
# goreleaser, pinned in mise.toml; pnpm, pinned by packageManager in the
# nearest package.json.
#
# Each tool resolves to mise's install of the pin when mise is on PATH, and
# to the one on PATH otherwise (CI installs the pins itself and has no mise).
# What it resolves to must report the pinned version: a missing tool or any
# other version is an error, never a warning or a silent skip. mise also
# puts go-installed binaries in Go's bin directory, so a plain PATH lookup
# can find a stray golangci-lint there instead of the pin; resolving each
# tool through its own mise entry avoids that.
#
# The command runs with the listed tools' directories first on PATH (so the
# go that golangci-lint shells out to is the pin too) and, when it names a
# listed tool, by that tool's resolved path. GOROOT is unset (each go binary
# knows its own root) and GOWORK is off: CI has no go.work, so a local one
# must not change what builds.
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
mise_toml=$root/mise.toml

usage() {
	echo "usage: $0 <tool>[,<tool>...] [-- <command> [args...]]" >&2
	exit 2
}

die() {
	printf 'ERROR: %s\n' "$@" >&2
	exit 1
}

# mise_key <tool>: the tool's [tools] key in mise.toml; empty when mise does
# not pin it.
mise_key() {
	case $1 in
	go | gofmt) echo go ;;
	golangci-lint) sed -n 's/^"\(go:.*golangci-lint\)" = ".*"$/\1/p' "$mise_toml" ;;
	node | gitleaks | goreleaser) echo "$1" ;;
	esac
}

# pin <tool>: the version the tool must report.
pin() {
	local d
	case $1 in
	go | gofmt) sed -n 's/^go = "\(.*\)"$/\1/p' "$mise_toml" ;;
	golangci-lint) sed -n 's/^"go:.*golangci-lint" = "\(.*\)"$/\1/p' "$mise_toml" ;;
	node | gitleaks | goreleaser) sed -n "s/^$1 = \"\\(.*\\)\"\$/\\1/p" "$mise_toml" ;;
	pnpm)
		# pnpm/action-setup reads packageManager; so does pnpm itself.
		d=$PWD
		while [ "$d" != / ]; do
			if [ -f "$d/package.json" ] && grep -q '"packageManager"' "$d/package.json"; then
				sed -n 's/.*"packageManager"[[:space:]]*:[[:space:]]*"pnpm@\([^"+]*\).*/\1/p' "$d/package.json"
				return
			fi
			d=$(dirname "$d")
		done
		;;
	*) die "unknown tool '$1' (go, gofmt, golangci-lint, gitleaks, node, goreleaser, pnpm)" ;;
	esac
}

# version <tool> <path>: the version the binary at path reports.
version() {
	case $1 in
	go) "$2" env GOVERSION | sed 's/^go//' ;;
	gofmt) "$(dirname "$2")/go" env GOVERSION | sed 's/^go//' ;;
	golangci-lint) "$2" version 2>&1 | sed -n 's/.*has version v\{0,1\}\([0-9][0-9.]*\).*/\1/p' | head -1 ;;
	gitleaks) "$2" version | sed 's/^v//' ;;
	node) "$2" --version | sed 's/^v//' ;;
	goreleaser) "$2" --version 2>&1 | sed -n 's/^GitVersion:[[:space:]]*//p' ;;
	pnpm) "$2" --version ;;
	esac
}

# resolve <tool>: sets path to the binary to use (empty when none) and
# mise_err to why mise could not supply it.
resolve() {
	local key d dirs
	path=""
	mise_err=""
	key=$(mise_key "$1")
	if [ -n "$key" ] && command -v mise >/dev/null 2>&1; then
		if dirs=$(cd "$root" && mise bin-paths "$key" 2>&1); then
			while IFS= read -r d; do
				if [ -x "$d/$1" ]; then
					path=$d/$1
					return
				fi
			done <<<"$dirs"
			mise_err="no $key installed for this checkout"
		else
			mise_err=$(printf '%s\n' "$dirs" | grep -m1 'not trusted' ||
				printf '%s\n' "$dirs" | grep -m1 'ERROR' || true)
		fi
	fi
	path=$(command -v "$1" || true)
}

# fix <tool>: how to get the pin.
fix() {
	case $1 in
	pnpm) echo "pnpm runs the packageManager version itself (pnpm >= 9.7); install pnpm, or run: corepack enable" ;;
	*)
		if command -v mise >/dev/null 2>&1; then
			echo "run in $root: mise trust && mise install"
		else
			echo "install mise (https://mise.jdx.dev), then in $root: mise trust && mise install"
		fi
		;;
	esac
}

[ $# -ge 1 ] || usage
IFS=, read -r -a tools <<<"$1"
shift
if [ $# -gt 0 ]; then
	[ "$1" = -- ] && [ $# -ge 2 ] || usage
	shift
fi

unset GOROOT
export GOWORK=off

prefix=""
cmd_path=""
for t in "${tools[@]}"; do
	want=$(pin "$t")
	[ -n "$want" ] || die "no pinned version for $t (mise.toml, or packageManager in package.json)"
	resolve "$t"
	hint=$(fix "$t")
	[ -z "$mise_err" ] || hint="$hint (mise: $mise_err)"
	[ -n "$path" ] || die "$t not found; this repo pins $want." "$hint"
	have=$(version "$t" "$path" 2>/dev/null || true)
	[ "$have" = "$want" ] ||
		die "$t at $path is ${have:-of unknown version}; this repo pins $want, and CI runs $want." "$hint"
	prefix=$prefix${prefix:+:}$(dirname "$path")
	if [ $# -gt 0 ] && [ "$1" = "$t" ]; then cmd_path=$path; fi
done
export PATH="$prefix:$PATH"

[ $# -gt 0 ] || exit 0

# A listed tool another one shells out to (go under golangci-lint, node
# under pnpm) must now be the pin on PATH; a directory listed earlier that
# also holds one would shadow it.
for t in "${tools[@]}"; do
	case $t in go | node) ;; *) continue ;; esac
	got=$(command -v "$t")
	[ "$(version "$t" "$got" 2>/dev/null || true)" = "$(pin "$t")" ] ||
		die "$got shadows the pinned $t on PATH; list $t first: $0 $t,..."
done

if [ -n "$cmd_path" ]; then
	shift
	exec "$cmd_path" "$@"
fi
exec "$@"
