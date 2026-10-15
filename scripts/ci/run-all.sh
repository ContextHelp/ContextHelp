#!/usr/bin/env bash
# Run every CI job locally (`make ci`) and print one verdict per job.
#
#   scripts/ci/run-all.sh <job>...      # job ids of .github/workflows/ci.yml
#
# Each job runs as `make ci-<job>`: the same make targets, in the same
# order, that its CI steps call (scripts/check-ci-parity.sh holds the two
# together). Like CI, a failing job does not stop the others.
#
# A job whose runner-provided prerequisite is missing here (Postgres,
# mingw-w64) is SKIPPED with how to run it; scripts/ci/prereq.sh records
# why. Skips never count as a pass: the run ends INCOMPLETE, and exits 1
# when CI_STRICT=1.
set -uo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$root" || exit 1

[ $# -gt 0 ] || {
	echo "usage: $0 <job>..." >&2
	exit 2
}

report=$(mktemp)
trap 'rm -f "$report" "$report.jobs"' EXIT
export CI_LOCAL_REPORT=$report

make=${MAKE:-make}
results=()
failed=0
skipped=0
for job in "$@"; do
	printf '\n=== ci-%s ===\n' "$job"
	before=$(wc -l <"$report")
	start=$(date +%s)
	if "$make" --no-print-directory "ci-$job"; then
		status=PASS
	elif [ "$(grep -c '^SKIP' <(tail -n +"$((before + 1))" "$report"))" -gt 0 ]; then
		status=SKIP
		skipped=$((skipped + 1))
	else
		status=FAIL
		failed=$((failed + 1))
	fi
	results+=("$(printf '%-4s  %-12s %4ss' "$status" "$job" "$(($(date +%s) - start))")")
	tail -n +"$((before + 1))" "$report" | while IFS=$'\t' read -r kind what how; do
		printf '%s\t%s\t%s\t%s\n' "$job" "$kind" "$what" "$how"
	done >>"$report.jobs"
done

echo
echo "================ local CI summary ================"
printf '%s\n' "${results[@]}"
if [ -s "$report.jobs" ]; then
	echo
	while IFS=$'\t' read -r job kind what how; do
		printf '%s  %s: %s\n      run it: %s\n' "$kind" "$job" "$what" "$how"
	done <"$report.jobs"
fi
rm -f "$report.jobs"

echo
echo "Not reproduced locally (CI-only steps; see docs/ci-cd.md#local-parity):"
scripts/check-ci-parity.sh --ci-only | sed 's/^/  /'
echo "  test job runs on ubuntu-latest AND macos-latest; this run: $(uname -s) $(uname -m)"
echo

if [ "$failed" -gt 0 ]; then
	echo "RESULT: FAILED ($failed job(s)); CI will fail too."
	exit 1
fi
if [ "$skipped" -gt 0 ]; then
	echo "RESULT: INCOMPLETE: $skipped job(s) skipped above. Not CI-equivalent until they run."
	[ "${CI_STRICT:-}" = 1 ] && exit 1
	exit 0
fi
echo "RESULT: PASS: every CI job ran and passed (platform differences aside)."
