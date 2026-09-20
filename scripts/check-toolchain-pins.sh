#!/usr/bin/env bash
# Assert every toolchain declaration agrees with mise.toml.
#
# mise.toml is the single source of truth for tool versions. CI derives
# GO_VERSION from it at runtime, but Dockerfiles bake a `golang:<ver>` base
# image that nothing can derive, and go.mod carries its own `go` directive.
# This script is the gate that keeps all of them in step.
#
# Checks:
#   1. Dockerfile + docker/Dockerfile.dpkms golang base image == mise go pin
#   2. go.mod `go` directive is satisfied by the mise go pin (pin >= directive)
#   3. no workflow hardcodes a go-version literal instead of deriving it
set -euo pipefail

cd "$(dirname "$0")/.."

fail=0
err() {
	echo "FAIL: $*" >&2
	fail=1
}

# ── mise.toml pins ───────────────────────────────────────────────────────────
mise_go=$(sed -n 's/^go = "\(.*\)"$/\1/p' mise.toml)
# golangci-lint is declared via the `go:` backend, so the key is quoted:
#   "go:github.com/golangci/golangci-lint/v2/cmd/golangci-lint" = "2.1.6"
mise_lint=$(sed -n 's/^"go:.*golangci-lint" = "\(.*\)"$/\1/p' mise.toml)

[ -n "$mise_go" ] || err "mise.toml has no exact 'go' pin"
[ -n "$mise_lint" ] || err "mise.toml has no exact 'golangci-lint' pin"
case "$mise_go" in
*latest* | "") err "mise.toml go pin must be an exact version, got '$mise_go'" ;;
esac
echo "mise.toml: go=$mise_go golangci-lint=$mise_lint"

# ── 1. Dockerfiles ───────────────────────────────────────────────────────────
# Both build stages must use the same Go as mise/CI, or a container build can
# succeed (or fail) on a compiler the rest of the toolchain never sees.
for df in Dockerfile docker/Dockerfile.dpkms; do
	[ -f "$df" ] || continue
	while read -r img; do
		[ -n "$img" ] || continue
		# golang:1.26.8-bookworm -> 1.26.8
		ver=${img#golang:}
		ver=${ver%%-*}
		if [ "$ver" != "$mise_go" ]; then
			err "$df uses golang:$ver but mise.toml pins go $mise_go"
		else
			echo "ok: $df -> golang:$ver"
		fi
	done < <(grep -oE '\bgolang:[0-9][^ ]*' "$df" || true)
done

# The devcontainer parameterises its base image via ARG, so the literal-tag
# grep above cannot see it; check the default explicitly.
dc_df=.devcontainer/Dockerfile
if [ -f "$dc_df" ]; then
	dc_go=$(sed -n 's/^ARG GO_VERSION=\(.*\)$/\1/p' "$dc_df" | head -1)
	if [ -z "$dc_go" ]; then
		err "$dc_df has no ARG GO_VERSION default to check"
	elif [ "$dc_go" != "$mise_go" ]; then
		err "$dc_df pins GO_VERSION=$dc_go but mise.toml pins go $mise_go"
	else
		echo "ok: $dc_df -> GO_VERSION=$dc_go"
	fi
	dc_lint=$(sed -n 's/^ARG GOLANGCI_LINT_VERSION=v\(.*\)$/\1/p' "$dc_df" | head -1)
	if [ -n "$dc_lint" ] && [ "$dc_lint" != "$mise_lint" ]; then
		err "$dc_df pins golangci-lint $dc_lint but mise.toml pins $mise_lint"
	elif [ -n "$dc_lint" ]; then
		echo "ok: $dc_df -> golangci-lint v$dc_lint"
	fi
fi

# ── 2. go.mod directive ──────────────────────────────────────────────────────
# The go.mod directive is a MINIMUM, not an equality: the pin must be >= it.
gomod_go=$(awk '/^go /{print $2; exit}' go.mod)
if [ -n "$gomod_go" ]; then
	lowest=$(printf '%s\n%s\n' "$mise_go" "$gomod_go" | sort -V | head -1)
	if [ "$lowest" != "$gomod_go" ]; then
		err "mise.toml pins go $mise_go but go.mod requires >= $gomod_go"
	else
		echo "ok: go.mod requires >= $gomod_go, pin $mise_go satisfies it"
	fi
fi

# ── 3. workflows must derive, not hardcode ───────────────────────────────────
# A literal version in a workflow silently re-introduces the drift this whole
# mechanism exists to remove.
while read -r hit; do
	[ -n "$hit" ] || continue
	err "workflow hardcodes a Go version, derive it from mise.toml instead: $hit"
done < <(grep -rnE "^\s*(GO_VERSION|go-version):\s*'?\"?[0-9]" .github/workflows/ || true)

# go-version-file: go.mod reads the MINIMUM directive, not the pin, so it is a
# second source of truth rather than a derivation of this one.
while read -r hit; do
	[ -n "$hit" ] || continue
	err "workflow reads go-version-file instead of the mise.toml pin: $hit"
done < <(grep -rnE "^\s*go-version-file:" .github/workflows/ || true)

# golangci-lint rules drift between minors; a literal version makes local lint
# output stop being evidence about CI.
while read -r hit; do
	[ -n "$hit" ] || continue
	err "workflow hardcodes a golangci-lint version, derive it from mise.toml instead: $hit"
done < <(grep -rnE "golangci-lint.*@v?[0-9]+\.[0-9]+|golangci-lint/(master|v[0-9])" .github/workflows/ | grep -vE 'golangci-lint/v2/cmd/golangci-lint@v\$\{\{' || true)

if [ "$fail" -ne 0 ]; then
	echo
	echo "Toolchain declarations disagree. mise.toml is the source of truth:" >&2
	echo "  update mise.toml, then run 'make toolchain-check' to find stragglers." >&2
	exit 1
fi

echo "toolchain pins agree"
