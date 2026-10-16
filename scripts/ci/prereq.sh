#!/usr/bin/env bash
# Check a prerequisite a CI job gets from its runner but a laptop may lack.
#
#   scripts/ci/prereq.sh postgres|mingw|chrome
#
# postgres, mingw: missing is an error, with how to provide it. Under the
# local CI runner (`make ci` sets CI_LOCAL_REPORT) the error is also
# recorded, so the runner reports the job as SKIPPED, not passed. Never on
# GitHub Actions: there a missing prerequisite is a plain failure.
#
# chrome: CI's runner images ship Chrome, so the headless viewer checks in
# the e2e suites run there; without one they skip themselves. Missing is a
# loud notice, not an error, since the rest of the suite still runs.
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
ci_yml=$root/.github/workflows/ci.yml

banner() {
	printf '\n' >&2
	printf '!!! %s\n' "$@" >&2
	printf '\n' >&2
}

record() {
	if [ -n "${CI_LOCAL_REPORT:-}" ] && [ "${GITHUB_ACTIONS:-}" != true ]; then
		printf '%s\t%s\t%s\n' "$1" "$2" "$3" >>"$CI_LOCAL_REPORT"
	fi
}

# missing <what> <how>
missing() {
	banner "NOT RUN: $1" "    CI provides it, so CI runs this step; a local pass is not CI-equivalent." "    To run it: $2"
	record SKIP "$1" "$2"
	exit 1
}

case ${1:-} in
postgres)
	host=${POSTGRES_HOST:-localhost}
	port=${POSTGRES_PORT:-5432}
	if ! (exec 3<>"/dev/tcp/$host/$port") 2>/dev/null; then
		image=$(sed -n 's/^ *image: *\(pgvector\/[^ ]*\).*/\1/p' "$ci_yml" | head -1)
		missing "Postgres not reachable at $host:$port (integration job service)" \
			"docker run -d --rm --name ctxt-ci-postgres -p $port:5432 -e POSTGRES_USER=${POSTGRES_USER:-} -e POSTGRES_PASSWORD=${POSTGRES_PASSWORD:-} -e POSTGRES_DB=${POSTGRES_DB:-} ${image:-<integration service image in ci.yml>}, then rerun"
	fi
	;;
mingw)
	command -v x86_64-w64-mingw32-gcc >/dev/null 2>&1 ||
		missing "x86_64-w64-mingw32-gcc not found (windows job cross-compiles with CGO)" \
			"brew install mingw-w64 (macOS) or apt-get install gcc-mingw-w64-x86-64 (Debian/Ubuntu), then rerun"
	;;
chrome)
	[ "${GITHUB_ACTIONS:-}" != true ] || exit 0
	if [ "${CTXT_CHROME:-}" = off ] ||
		! "$root/scripts/ci/tools.sh" go -- "$root/scripts/headless-chrome.sh" path >/dev/null 2>&1; then
		banner "PARTIAL: no Chrome/Chromium (or CTXT_CHROME=off); the headless viewer checks will skip." \
			"    CI runs them. Install Chrome or set CTXT_CHROME=/path/to/chrome to run them here."
		record NOTE "headless viewer checks skipped: no Chrome/Chromium" "install Chrome or set CTXT_CHROME"
	fi
	;;
*)
	echo "usage: $0 postgres|mingw|chrome" >&2
	exit 2
	;;
esac
