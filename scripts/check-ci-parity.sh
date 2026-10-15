#!/usr/bin/env bash
# Fail when .github/workflows/ci.yml and the local CI targets diverge.
#
# The Makefile is the single source of truth for what CI runs. Every check
# step in ci.yml is `make <target>`, and `make ci-<job>` runs the same
# targets in the same order, so `make ci` (or one `make ci-<job>`) executes
# the commands CI does, with the same flags, tags and env. This gate holds
# that arrangement:
#
#   1. every `run:` step in ci.yml is a one-line `make <target>`, optionally
#      followed by LINT_BASE=... (the PR base only CI knows); the exceptions
#      are the provisioning steps named in $exempt, and those may not run a
#      check themselves
#   2. no step or job in ci.yml sets env:, defaults: or working-directory:;
#      env belongs in the make target, where a local run sees it too
#   3. each job <j> has a Makefile target ci-<j> without prerequisites whose
#      recipe is one `$(CI_STEP) <target>` line per make step of <j>, in
#      step order, prefixed with `-` exactly when that step has
#      continue-on-error: true
#   4. CI_JOBS in the Makefile lists ci.yml's jobs, in order
#   5. every golangci-lint run in the Makefile, Taskfile, scripts/ and
#      .githooks/ goes through scripts/ci/tools.sh, which uses the
#      mise.toml pin or fails
#   6. the gosec and govulncheck versions named in workflows, the Taskfile
#      and scripts/ are the Makefile's GOSEC_VERSION / GOVULNCHECK_VERSION
#
#   scripts/check-ci-parity.sh --ci-only   lists the CI steps a local run
#                                          does not reproduce, and exits
set -euo pipefail

cd "$(dirname "$0")/.."

ci=.github/workflows/ci.yml
mk=Makefile

# Provisioning steps: they install what a local run gets from mise.toml
# (scripts/ci/tools.sh) or from scripts/ci/prereq.sh, or publish the pins.
exempt=(
	"Read pins from mise.toml"
	"Install gitleaks"
	"Install golangci-lint"
	"Install mingw-w64 (Windows CGO cross-compilation)"
	"Download dependencies"
)
# Actions that set a step up rather than check anything.
setup_actions="actions/checkout actions/setup-go actions/setup-node pnpm/action-setup"
# Make variables a CI step may pass: values only CI knows.
ci_vars="LINT_BASE"

fail=0
err() {
	echo "FAIL: $*" >&2
	fail=1
}

for f in "$ci" "$mk"; do
	[ -f "$f" ] || {
		echo "FAIL: $f not found" >&2
		exit 1
	}
done

# steps <workflow>: one record (fields split by \037) per job and per step:
#   J  <job>
#   JENV <job>
#   S  <job> <name> <uses> <continue-on-error> <env> <working-dir> <block> <run>
# Jobs are read at two-space indentation, job keys at four, step keys at
# eight, as in every workflow here. A block scalar run (| or >) is joined
# with " ; ".
steps() {
	awk '
		function flush() {
			if (instep)
				printf "S\037%s\037%s\037%s\037%s\037%s\037%s\037%s\037%s\n", job, name, uses, coe, env, wd, block, run
			instep = 0; name = ""; uses = ""; coe = "false"; env = 0; wd = 0; block = 0; run = ""
		}
		function unquote(v) {
			sub(/^[ \t]+/, "", v); sub(/[ \t]+$/, "", v)
			if (v ~ /^".*"$/ || v ~ /^\047.*\047$/) v = substr(v, 2, length(v) - 2)
			return v
		}
		{
			raw = $0
			if (inblock) {
				if (raw ~ /^[ \t]*$/) next
				match(raw, /^ */)
				if (RLENGTH > 8) {
					l = raw; sub(/^ +/, "", l)
					run = run (run == "" ? "" : " ; ") l
					next
				}
				inblock = 0
			}
			line = raw
			if (line ~ /^[ \t]*#/ || line ~ /^[ \t]*$/) next
			sub(/[ \t]+#.*$/, "", line)
			if (line ~ /^[^ ]/) {
				flush(); top = line; sub(/:.*/, "", top); job = ""; insteps = 0
				next
			}
			if (top != "jobs") next
			if (line ~ /^  [A-Za-z0-9_-]+:[ \t]*$/) {
				flush(); job = line; sub(/^  /, "", job); sub(/:.*/, "", job); insteps = 0
				print "J\037" job
				next
			}
			if (job == "") next
			if (line ~ /^    (env|defaults):/) print "JENV\037" job
			if (line ~ /^    [A-Za-z]/) {
				flush(); insteps = (line ~ /^    steps:/)
				next
			}
			if (!insteps) next
			if (line ~ /^      - /) {
				flush(); instep = 1
				line = "        " substr(line, 9)
			}
			if (line ~ /^        [A-Za-z-]+:/) {
				key = line; sub(/^ +/, "", key); sub(/:.*/, "", key)
				val = line; sub(/^ +[A-Za-z-]+:[ \t]*/, "", val)
				if (key == "name") name = unquote(val)
				else if (key == "uses") uses = unquote(val)
				else if (key == "env") env = 1
				else if (key == "working-directory") wd = 1
				else if (key == "continue-on-error") coe = unquote(val)
				else if (key == "run") {
					if (val ~ /^[|>][-+]?$/) { inblock = 1; block = 1; run = "" }
					else run = val
				}
			}
		}
		END { flush() }
	' "$1"
}

is_exempt() {
	local n
	for n in "${exempt[@]}"; do [ "$1" = "$n" ] && return 0; done
	return 1
}

is_setup_action() {
	local a
	for a in $setup_actions; do [ "${1%%@*}" = "$a" ] && return 0; done
	return 1
}

records=$(steps "$ci")

# ── --ci-only ────────────────────────────────────────────────────────────────
if [ "${1:-}" = --ci-only ]; then
	while IFS=$'\037' read -r kind job name uses _; do
		[ "$kind" = S ] && [ -n "$uses" ] || continue
		is_setup_action "$uses" && continue
		echo "$job: ${name:-$uses} (${uses%%@*}; CI only, not run locally)"
	done <<<"$records"
	exit 0
fi

# ── 1-3. steps ↔ ci-<job> recipes ────────────────────────────────────────────
var_re="^([[:space:]]+($(echo "$ci_vars" | tr ' ' '|'))=(\"[^\"]*\"|'[^']*'|[^[:space:]\"']*))*[[:space:]]*$"
check_re='(^|[^A-Za-z0-9_-])(go (test|vet|build|run)|golangci-lint run|gofmt|gosec|govulncheck|gitleaks (git|dir|detect|protect)|pnpm|make)([[:space:]]|$)'

jobs=()
while IFS=$'\037' read -r kind job rest; do
	case $kind in
	J) jobs+=("$job") ;;
	JENV) err "$ci job $job sets env: or defaults: at job level; put it in the make target so a local run gets it too" ;;
	esac
done <<<"$records"

for job in "${jobs[@]}"; do
	want=()
	while IFS=$'\037' read -r kind j name uses coe env wd block run; do
		[ "$kind" = S ] && [ "$j" = "$job" ] || continue
		[ -n "$run" ] || [ "$block" = 1 ] || continue # a uses: step
		label="$ci job $job step '${name:-$run}'"
		if is_exempt "$name"; then
			if [[ "$run" =~ $check_re ]]; then
				err "$label is a provisioning step but runs a check (${BASH_REMATCH[2]}); move the check into a make target"
			fi
			continue
		fi
		[ "$env" = 0 ] || err "$label sets env:; put it in the make target so a local run gets it too"
		[ "$wd" = 0 ] || err "$label sets working-directory:; cd in the make target instead"
		if [ "$block" = 1 ] || ! [[ "$run" =~ ^make[[:space:]]+([A-Za-z0-9_.-]+)(.*)$ ]]; then
			err "$label runs '$run' inline; make it a Makefile target and run 'make <target>' (CI and local then share one command)"
			continue
		fi
		target=${BASH_REMATCH[1]}
		args=${BASH_REMATCH[2]}
		[[ "$args" =~ $var_re ]] ||
			err "$label passes '$args' to make; only $ci_vars may differ between CI and a local run"
		grep -qE "^$target:" "$mk" || err "$label runs make $target, which $mk does not define"
		if [ "$coe" = true ]; then want+=("-$target"); else want+=("$target"); fi
	done <<<"$records"

	tgt="ci-$job"
	recipe=$(awk -v t="$tgt" '
		index($0, t ":") == 1 && substr($0, length(t) + 2, 1) != "=" {
			pre = substr($0, length(t) + 2); gsub(/[ \t]/, "", pre)
			print "PRE\037" pre; inr = 1; next
		}
		inr && /^\t/ { print "R\037" $0; next }
		inr { exit }
	' "$mk")
	if [ -z "$recipe" ]; then
		err "$mk has no target $tgt for $ci job $job; add it with one \$(CI_STEP) line per make step: ${want[*]:-<none>}"
		continue
	fi
	got=()
	while IFS=$'\037' read -r kind line; do
		case $kind in
		PRE) [ -z "$line" ] || err "$mk $tgt has prerequisites ($line); list them as \$(CI_STEP) recipe lines in ci.yml step order" ;;
		R)
			# shellcheck disable=SC2016 # a literal $(CI_STEP), not an expansion
			if [[ "$line" =~ ^$'\t'(-?)'$(CI_STEP)'[[:space:]]+([A-Za-z0-9_.-]+)[[:space:]]*$ ]]; then
				got+=("${BASH_REMATCH[1]}${BASH_REMATCH[2]}")
			else
				err "$mk $tgt runs '${line#$'\t'}'; a ci-<job> target only runs \$(CI_STEP) <target> lines, so it cannot do anything CI does not"
			fi
			;;
		esac
	done <<<"$recipe"
	if [ "${want[*]:-}" != "${got[*]:-}" ]; then
		err "$mk $tgt runs [${got[*]:-}] but $ci job $job runs [${want[*]:-}] ('-' = continue-on-error: true)"
	else
		echo "ok: $tgt == $ci job $job: ${got[*]:-<no make steps>}"
	fi
done

# ── 4. CI_JOBS ───────────────────────────────────────────────────────────────
mk_jobs=$(sed -n 's/^CI_JOBS[[:space:]]*:*=[[:space:]]*//p' "$mk" | head -1 | tr -s ' \t' ' ' | sed 's/ $//')
if [ "$mk_jobs" != "${jobs[*]}" ]; then
	err "$mk CI_JOBS is '$mk_jobs' but $ci has jobs '${jobs[*]}'; make ci would not run what CI runs"
else
	echo "ok: CI_JOBS == $ci jobs"
fi

# ── 5 and 6. pinned tools on every local path ────────────────────────────────
dev_files=("$mk")
for f in Taskfile.yml Taskfile.yaml; do [ -f "$f" ] && dev_files+=("$f"); done
while IFS= read -r f; do dev_files+=("$f"); done < <(
	find scripts .githooks -type f ! -name '*_test.go' ! -path '*/testdata/*' ! -name check-ci-parity.sh 2>/dev/null | sort
)

while IFS= read -r hit; do
	[ -n "$hit" ] || continue
	err "$hit runs golangci-lint without scripts/ci/tools.sh, so it uses whatever version is on PATH; run: scripts/ci/tools.sh golangci-lint,go -- golangci-lint ..."
done < <(awk '{
	line = $0
	if (line ~ /^[ \t]*#/) next
	sub(/[ \t]+#.*$/, "", line)
	if (line ~ /(echo|printf)[ \t]/) next
	if (line ~ /(^|[^A-Za-z0-9_.\/-])golangci-lint[ \t]+(run|fmt)([^A-Za-z0-9_.-]|$)/ &&
		line !~ /(tools\.sh|\$\{?tools\}?|\$\(CI_TOOLS\))["\047]?[ \t]+[a-z,-]*golangci-lint[a-z,-]*[ \t]+--[ \t]+golangci-lint/) {
		sub(/^[ \t]+/, "", line)
		printf "%s:%d: %s\n", FILENAME, FNR, line
	}
}' "${dev_files[@]}")

for tool in GOSEC:github.com/securego/gosec/v2/cmd/gosec GOVULNCHECK:golang.org/x/vuln/cmd/govulncheck; do
	var=${tool%%:*}_VERSION
	pkg=${tool#*:}
	pin=$(sed -n "s/^${var}[[:space:]]*:*=[[:space:]]*\\([^[:space:]]*\\).*/\\1/p" "$mk" | head -1)
	if [ -z "$pin" ]; then
		err "$mk defines no $var"
		continue
	fi
	while IFS=: read -r f n ref; do
		[ -n "$ref" ] || continue
		[ "${ref#*@}" = "$pin" ] || err "$f:$n installs $ref but $mk pins $pkg@$pin ($var)"
	done < <(grep -noE "$pkg@v[0-9][0-9.]*" .github/workflows/*.yml "${dev_files[@]}" 2>/dev/null || true)
done

if [ "$fail" -ne 0 ]; then
	echo
	echo "ci.yml and the local CI targets disagree. Each CI check step runs" >&2
	echo "'make <target>', and the Makefile's ci-<job> runs the same targets in" >&2
	echo "the same order; see docs/ci-cd.md#local-parity." >&2
	exit 1
fi
echo "ci.yml and make ci agree"
