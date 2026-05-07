#!/usr/bin/env bash
# Enforce a recall-floor on the JSON output of `ben run --format json`.
#
# Usage:
#   ben-floor.sh <run.json> <floor> [<candidate>]
#
# Exits 0 when every (selected) candidate's recall_at_k >= floor; exits 2
# otherwise. Exits 1 on input errors (missing file, malformed JSON, jq
# missing). The optional <candidate> argument restricts the check to a
# single named candidate; absent, all candidates must clear the floor.
#
# Why a shell script and not a Go test:
#   - The check is invoked from both the Makefile and the CI workflow —
#     both already shell out — so a script is the natural seam.
#   - jq is already a transitive build dep of the project; not adding a
#     new tool.
#   - The Go-side test of the adapter itself lives at
#     cmd/ben-adapter-ctxt-recall/main_test.go.
set -euo pipefail

if [ "$#" -lt 2 ] || [ "$#" -gt 3 ]; then
  echo "usage: $(basename "$0") <run.json> <floor> [<candidate>]" >&2
  exit 1
fi

run_json="$1"
floor="$2"
candidate="${3:-}"

if ! command -v jq >/dev/null 2>&1; then
  echo "ben-floor: jq is required but not on PATH" >&2
  exit 1
fi

if [ ! -f "$run_json" ]; then
  echo "ben-floor: $run_json not found" >&2
  exit 1
fi

# Validate floor is a number in [0, 1].
case "$floor" in
  ''|*[!0-9.]*)
    echo "ben-floor: floor must be numeric, got '$floor'" >&2
    exit 1
    ;;
esac

filter='.candidates[]'
if [ -n "$candidate" ]; then
  filter='.candidates[] | select(.name == "'"$candidate"'")'
fi

# Stream `name<TAB>recall_at_k` lines. Avoid `mapfile` so this also runs
# on macOS' system bash 3.2 (which CI doesn't see, but local dev does).
rows_file=$(mktemp)
trap 'rm -f "$rows_file"' EXIT
jq -r "$filter | [.name, (.metrics.recall_at_k // 0)] | @tsv" <"$run_json" >"$rows_file"

if [ ! -s "$rows_file" ]; then
  echo "ben-floor: no candidates found in $run_json (filter='$filter')" >&2
  exit 1
fi

fail=0
while IFS= read -r row; do
  name="${row%%	*}"
  recall="${row##*	}"
  # Use awk for float comparison (POSIX shell can't do it directly).
  ok=$(awk -v r="$recall" -v f="$floor" 'BEGIN{print (r+0 >= f+0) ? "1" : "0"}')
  if [ "$ok" = "1" ]; then
    printf '  [pass] %-12s recall_at_k=%s  (floor=%s)\n' "$name" "$recall" "$floor"
  else
    printf '  [FAIL] %-12s recall_at_k=%s  (floor=%s)\n' "$name" "$recall" "$floor" >&2
    fail=1
  fi
done <"$rows_file"

if [ "$fail" -ne 0 ]; then
  echo "ben-floor: at least one candidate dropped below the floor; see ADR-070 §6 for the escape-hatch process." >&2
  exit 2
fi
