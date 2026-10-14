#!/usr/bin/env bash
# Assert every toolchain declaration agrees with mise.toml.
#
# mise.toml is the single source of truth for tool versions. CI derives
# GO_VERSION from it at runtime, but Dockerfiles bake a `golang:<ver>` base
# image that nothing can derive, and go.mod carries its own `go` directive.
# This script is the gate that keeps all of them in step.
#
# Checks:
#   1. Dockerfile golang base image == mise go pin, and so is every golang
#      image a compose file (docker-compose*.yml, docker/, .devcontainer/) or
#      a workflow (.github/workflows/*.yml: services, job containers,
#      docker:// steps) runs; every other image those files pull and every
#      Dockerfile base image names an exact release (X.Y.Z tag, dated tag or
#      digest), never a floating tag
#   2. go.mod `go` directive is satisfied by the mise go pin (pin >= directive)
#   3. no workflow hardcodes a go-version literal instead of deriving it
#   4. node: mise pins an exact version; no workflow hardcodes node-version;
#      every node image (compose, workflows, Dockerfile FROM) runs the pin;
#      .nvmrc / .node-version equal it; package.json engines.node is the pin
#      or a >= minimum the pin satisfies
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
mise_node=$(sed -n 's/^node = "\(.*\)"$/\1/p' mise.toml)

[ -n "$mise_go" ] || err "mise.toml has no exact 'go' pin"
[ -n "$mise_lint" ] || err "mise.toml has no exact 'golangci-lint' pin"
case "$mise_go" in
*latest* | "") err "mise.toml go pin must be an exact version, got '$mise_go'" ;;
esac
# node builds the embedded web bundles; an alias (lts, latest) or a major
# line (22) installs whatever is newest today.
[[ "$mise_node" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] ||
	err "mise.toml node pin must be an exact X.Y.Z version, got '$mise_node'"
echo "mise.toml: go=$mise_go golangci-lint=$mise_lint node=$mise_node"

# ── 1. Dockerfiles ───────────────────────────────────────────────────────────
# Both build stages must use the same Go as mise/CI, or a container build can
# succeed (or fail) on a compiler the rest of the toolchain never sees.
for df in Dockerfile; do
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

# Every other image must name one release as well. mise.toml pins no
# service versions, so the tag itself is the pin: a major or minor tag
# (redis:7-alpine, node:22.23-alpine), a codename (debian:bookworm-slim),
# latest or no tag at all pulls whatever the registry points at today.
#
# exact_ref <ref>: true when ref is a digest, or its tag carries a full
# X.Y.Z version or a YYYYMMDD date stamp (debian:bookworm-20260918-slim).
exact_ref() {
	local ref=$1 name tag
	case "$ref" in *@sha256:*) return 0 ;; esac
	name=${ref##*/} # registry.local:5000/redis:7 -> redis:7
	case "$name" in
	*:*) tag=${name##*:} ;;
	*) return 1 ;;
	esac
	[[ "$tag" =~ (^|[^0-9.])[0-9]+\.[0-9]+\.[0-9]+([^0-9.]|$) ]] && return 0
	[[ "$tag" =~ (^|-)20[0-9]{6}(-|$) ]] && return 0
	return 1
}
floating="a floating tag; pin an exact release (X.Y.Z or a dated tag) or a digest"

# toolchain_mismatch <ref>: prints why an image that carries a toolchain
# mise.toml pins (golang, node) runs a different version; prints nothing
# for an image that agrees or carries no pinned toolchain. Compose files,
# workflows and Dockerfiles run these images directly (the dev service
# builds and hot-reloads inside one), so exact is not enough: it must be
# the pin.
toolchain_mismatch() {
	local name=${1##*/} tag ver
	name=${name%%@*} # golang:1.26.8@sha256:... -> golang:1.26.8
	case "$name" in
	golang | golang:*)
		tag=${name#golang}
		tag=${tag#:}
		ver=${tag%%-*}
		[ "$ver" = "$mise_go" ] ||
			echo "but mise.toml pins go $mise_go (use golang:$mise_go-<variant>)"
		;;
	node | node:*)
		tag=${name#node}
		tag=${tag#:}
		ver=${tag%%-*}
		[ "$ver" = "$mise_node" ] ||
			echo "but mise.toml pins node $mise_node (use node:$mise_node-<variant>)"
		;;
	esac
}

for cf in docker-compose*.yml docker/docker-compose*.yml .devcontainer/docker-compose*.yml; do
	[ -f "$cf" ] || continue
	while read -r img; do
		[ -n "$img" ] || continue
		# The project's own image is the local build output (`build:`),
		# tagged by $TAG; nothing pulls it from a registry.
		case "$img" in ctxt/*) continue ;; esac
		ref=$img
		# ${VAR:-default} / ${VAR-default}: check the default compose uses.
		while [[ "$ref" =~ \$\{[A-Za-z_][A-Za-z0-9_]*:?-([^}]*)\} ]]; do
			ref=${ref/"${BASH_REMATCH[0]}"/"${BASH_REMATCH[1]}"}
		done
		if [[ "$ref" == *'$'* ]]; then
			err "$cf runs image $img, which has no default to check; give it an exact default"
		elif ! exact_ref "$ref"; then
			err "$cf runs image $img, $floating"
		elif why=$(toolchain_mismatch "$ref") && [ -n "$why" ]; then
			err "$cf runs image $img $why"
		else
			echo "ok: $cf -> $img"
		fi
	done < <(sed -n "s/^[[:space:]]*image:[[:space:]]*//p" "$cf" | sed -e 's/[[:space:]]#.*$//' -e 's/[[:space:]]*$//' -e "s/^[\"']//" -e "s/[\"']\$//")
done

# Workflows pull images as well: service containers (services.*.image), a
# job's container (`container: <image>` or `container: {image: ...}`) and
# `uses: docker://<image>` steps. Same rule as compose. An expression such
# as ${{ matrix.image }} resolves at run time, so the gate cannot check it.
workflow_images() {
	sed -n -E \
		-e 's/^[[:space:]]*(-[[:space:]]+)?image:[[:space:]]*//p' \
		-e 's/^[[:space:]]*container:[[:space:]]*([^[:space:]#].*)$/\1/p' \
		-e 's|^[[:space:]]*(-[[:space:]]+)?uses:[[:space:]]*["'"'"']?docker://||p' "$1" |
		sed -e 's/[[:space:]]#.*$//' -e 's/[[:space:]]*$//' -e "s/^[\"']//" -e "s/[\"']\$//"
}

for wf in .github/workflows/*.yml .github/workflows/*.yaml; do
	[ -f "$wf" ] || continue
	while read -r img; do
		[ -n "$img" ] || continue
		# shellcheck disable=SC2016 # literal ${{ expression marker
		if [[ "$img" == *'${{'* ]]; then
			err "$wf runs image $img, an expression the gate cannot check; name an exact release"
		elif ! exact_ref "$img"; then
			err "$wf runs image $img, $floating"
		elif why=$(toolchain_mismatch "$img") && [ -n "$why" ]; then
			err "$wf runs image $img $why"
		else
			echo "ok: $wf -> $img"
		fi
	done < <(workflow_images "$wf")
done

# Dockerfile base images: every FROM that pulls an image. ARG defaults are
# substituted; earlier build stages and scratch pull nothing.
for df in Dockerfile Dockerfile.* docker/Dockerfile* docker/*/Dockerfile* .devcontainer/Dockerfile*; do
	[ -f "$df" ] || continue
	# `# syntax=` pulls the BuildKit frontend image before any FROM. Parser
	# directives are only directives on the leading comment lines.
	while IFS= read -r line; do
		[[ "$line" =~ ^#[[:space:]]*([A-Za-z]+)[[:space:]]*=[[:space:]]*([^[:space:]]+) ]] || break
		[[ "${BASH_REMATCH[1]}" == [Ss][Yy][Nn][Tt][Aa][Xx] ]] || continue
		fe=${BASH_REMATCH[2]}
		if exact_ref "$fe"; then
			echo "ok: $df -> syntax $fe"
		else
			err "$df uses frontend $fe, $floating"
		fi
	done <"$df"

	arg_names=()
	arg_values=()
	stages=" scratch "
	while IFS= read -r line; do
		if [[ "$line" =~ ^[[:space:]]*[Aa][Rr][Gg][[:space:]]+([A-Za-z_][A-Za-z0-9_]*)=([^[:space:]]+) ]]; then
			v=${BASH_REMATCH[2]}
			v=${v#[\"\']}
			v=${v%[\"\']}
			arg_names+=("${BASH_REMATCH[1]}")
			arg_values+=("$v")
			continue
		fi
		[[ "$line" =~ ^[[:space:]]*[Ff][Rr][Oo][Mm][[:space:]]+(.*)$ ]] || continue
		set -f
		# shellcheck disable=SC2086 # split FROM into its words
		set -- ${BASH_REMATCH[1]}
		set +f
		while [ $# -gt 0 ] && [[ "$1" == --* ]]; do shift; done # --platform=...
		img=${1:-}
		stage=""
		if [ $# -ge 3 ] && [[ "$2" == [Aa][Ss] ]]; then stage=$3; fi
		ref=$img
		i=${#arg_names[@]}
		while [ "$i" -gt 0 ]; do
			i=$((i - 1))
			n=${arg_names[$i]}
			# shellcheck disable=SC2016 # literal ${NAME} pattern
			pat='${'"$n"'}'
			ref=${ref//"$pat"/${arg_values[$i]}}
			ref=${ref//"\$$n"/${arg_values[$i]}}
		done
		if [[ "$stages" == *" $ref "* ]]; then
			: # an earlier stage, or scratch
		elif [[ "$ref" == *'$'* ]]; then
			err "$df builds FROM $img, which has no ARG default to check; give it an exact default"
		elif ! exact_ref "$ref"; then
			err "$df builds FROM $img, $floating"
		elif why=$(toolchain_mismatch "$ref") && [ -n "$why" ]; then
			err "$df builds FROM $img $why"
		else
			echo "ok: $df -> FROM $ref"
		fi
		[ -n "$stage" ] && stages="$stages$stage "
	done <"$df"
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

# ── 4. node ──────────────────────────────────────────────────────────────────
# setup-node cannot read mise.toml, so CI derives node-version from it in the
# toolchain job, exactly like go-version. A literal (even today's pin) or a
# version file is a second source of truth.
while read -r hit; do
	[ -n "$hit" ] || continue
	err "workflow hardcodes a Node version, derive it from mise.toml instead: $hit"
done < <(grep -rnE "^\s*(NODE_VERSION|node-version):" .github/workflows/ | grep -vE "(NODE_VERSION|node-version):\s*['\"]?\\$\{\{" || true)

while read -r hit; do
	[ -n "$hit" ] || continue
	err "workflow reads node-version-file instead of the mise.toml pin: $hit"
done < <(grep -rnE "^\s*node-version-file:" .github/workflows/ || true)

# engines_node <package.json>: the engines.node constraint, if any.
engines_node() {
	awk '
		/"engines"[[:space:]]*:[[:space:]]*[{]/ { in_e = 1 }
		in_e && match($0, /"node"[[:space:]]*:[[:space:]]*"[^"]*"/) {
			s = substr($0, RSTART, RLENGTH)
			sub(/^"node"[[:space:]]*:[[:space:]]*"/, "", s)
			sub(/"$/, "", s)
			print s
			exit
		}
		in_e && /[}]/ { in_e = 0 }
	' "$1"
}

# nvm, fnm, setup-node and corepack read these, so a contradicting value
# builds the bundles with a different Node depending on who runs them.
while IFS= read -r f; do
	f=${f#./}
	case "$f" in
	*package.json)
		eng=$(engines_node "$f")
		[ -n "$eng" ] || continue
		e=${eng// /}
		case "$e" in
		">="*)
			min=${e#>=}
			min=${min#v}
			if [[ "$min" =~ ^[0-9]+(\.[0-9]+){0,2}$ ]] &&
				[ "$(printf '%s\n%s\n' "$mise_node" "$min" | sort -V | head -1)" = "$min" ]; then
				echo "ok: $f -> engines.node $eng"
			else
				err "$f engines.node $eng is not satisfied by mise.toml node pin $mise_node"
			fi
			;;
		*)
			if [ "${e#v}" = "$mise_node" ]; then
				echo "ok: $f -> engines.node $eng"
			else
				err "$f engines.node $eng: use the mise.toml pin $mise_node or a >= minimum it satisfies"
			fi
			;;
		esac
		;;
	*)
		v=$(head -1 "$f" | tr -d '[:space:]')
		v=${v#v}
		if [ "$v" != "$mise_node" ]; then
			err "$f pins node $v but mise.toml pins node $mise_node"
		else
			echo "ok: $f -> node $v"
		fi
		;;
	esac
done < <(find . \( -name node_modules -o -name .git \) -prune -o -type f \( -name .nvmrc -o -name .node-version -o -name package.json \) -print | sort)

if [ "$fail" -ne 0 ]; then
	echo
	echo "Toolchain declarations disagree. mise.toml is the source of truth:" >&2
	echo "  update mise.toml, then run 'make toolchain-check' to find stragglers." >&2
	exit 1
fi

echo "toolchain pins agree"
