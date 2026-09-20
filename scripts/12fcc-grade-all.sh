#!/usr/bin/env bash
# Grade the recorded 12fcc cassettes against a locally served
# conformance grading service.
#
# Boots `kit conformance svc serve` on a loopback port with this
# repo's scenario library (e2e/conformance/scenarios/), mints a
# single-use token carrying one grade:<ns> scope per binary namespace
# into a throwaway claims DB, uploads every cassette under
# e2e/conformance/cassettes/ at tier 3, and prints the per-scenario
# verdicts plus a per-factor rollup.
#
# The service authorizes uploads per scenario namespace, so a cassette
# recorded from the substrate is rejected by a ctxt-only token. The
# scopes are derived from the namespace directories that actually
# exist under scenarios/, so adding a third binary needs no edit
# here.
#
# Requirements:
#   - KIT_BIN: path to a kit binary that ships the `conformance`
#     command group (build from hop.top/kit cmd/kit). Defaults to
#     `kit` on PATH.
#
# Outputs:
#   - e2e/conformance/verdicts/<scenario-id>.json  (full tier-3 trace)
#   - stdout: verdict + factor tables
#
# Exit code: 0 when every cassette was graded (verdicts may still be
# fail -- grading is measurement, not a gate). STRICT=1 additionally
# fails the script when any verdict is not pass. Any ungradable
# verdict or transport error always fails the script: that means
# measurement itself broke.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CONF="$REPO_ROOT/e2e/conformance"
KIT_BIN="${KIT_BIN:-kit}"
STRICT="${STRICT:-0}"
TIER=3

if ! "$KIT_BIN" conformance svc serve --help >/dev/null 2>&1; then
  echo "ERROR: '$KIT_BIN' does not ship 'conformance svc'. Set KIT_BIN to a kit binary built from hop.top/kit cmd/kit." >&2
  exit 1
fi

TMP="$(mktemp -d "${TMPDIR:-/tmp}/ctxt-12fcc-grade.XXXXXX")"
SERVE_PID=""
cleanup() {
  if [ -n "$SERVE_PID" ]; then
    kill "$SERVE_PID" 2>/dev/null || true
    wait "$SERVE_PID" 2>/dev/null || true
  fi
  rm -rf "$TMP"
}
trap cleanup EXIT

CLAIMS_DB="$TMP/claims.db"

SCOPE_ARGS=()
SCOPE_NAMES=()
for ns_dir in "$CONF"/scenarios/*/; do
  [ -d "$ns_dir" ] || continue
  ns="$(basename "$ns_dir")"
  SCOPE_ARGS+=(--scope "grade:$ns")
  SCOPE_NAMES+=("grade:$ns")
done
if [ "${#SCOPE_ARGS[@]}" -eq 0 ]; then
  echo "ERROR: no scenario namespaces under $CONF/scenarios" >&2
  exit 1
fi

echo "==> minting grade token (${SCOPE_NAMES[*]})"
MINT_OUT="$TMP/mint.out"
"$KIT_BIN" conformance svc token mint \
  --claims-db "$CLAIMS_DB" \
  "${SCOPE_ARGS[@]}" \
  --tier-max 3 \
  --description "local cassette grading" >"$MINT_OUT"
TOKEN="$(awk '/Token \(copy now/{grab=1; next} grab && NF {print $1; exit}' "$MINT_OUT")"
if [ -z "$TOKEN" ]; then
  echo "ERROR: could not parse minted token" >&2
  cat "$MINT_OUT" >&2
  exit 1
fi

echo "==> serving scenario library from e2e/conformance/"
SERVE_LOG="$TMP/serve.log"
"$KIT_BIN" conformance svc serve \
  --claims-db "$CLAIMS_DB" \
  --scenarios-root "$CONF" \
  --addr 127.0.0.1 \
  --port 0 >"$SERVE_LOG" 2>&1 &
SERVE_PID=$!

PORT=""
for _ in $(seq 1 50); do
  PORT="$(sed -n 's/.*"port": *\([0-9][0-9]*\).*/\1/p' "$SERVE_LOG" | head -1)"
  [ -n "$PORT" ] && break
  if ! kill -0 "$SERVE_PID" 2>/dev/null; then
    echo "ERROR: grading service exited at boot:" >&2
    cat "$SERVE_LOG" >&2
    exit 1
  fi
  sleep 0.2
done
if [ -z "$PORT" ]; then
  echo "ERROR: could not determine service port" >&2
  cat "$SERVE_LOG" >&2
  exit 1
fi
SERVICE="http://127.0.0.1:$PORT"
echo "    service: $SERVICE"

VERDICTS="$CONF/verdicts"
mkdir -p "$VERDICTS"

overall=0
strict_fail=0
for dir in "$CONF"/cassettes/*/; do
  [ -d "$dir" ] || continue
  sid="$(basename "$dir")"
  out="$VERDICTS/$sid.json"
  echo "==> grading $sid (tier $TIER)"
  set +e
  "$KIT_BIN" conformance grade "$dir" \
    --service "$SERVICE" \
    --token "$TOKEN" \
    --tier "$TIER" \
    --format json \
    --no-hints -o "$out" >/dev/null 2>"$TMP/$sid.err"
  rc=$?
  set -e
  if [ ! -s "$out" ]; then
    echo "ERROR: no verdict returned for $sid (grade exit $rc):" >&2
    cat "$TMP/$sid.err" >&2
    overall=1
    continue
  fi
  verdict="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("verdict","?"))' "$out")"
  echo "    verdict: $verdict"
  case "$verdict" in
    pass) ;;
    fail) strict_fail=1 ;;
    *) overall=1 ;;  # ungradable/unknown = measurement broke
  esac
done

# The grading service's JSON-schema library embeds the serving
# process's working directory in output_schema_matches failure
# messages ("does not validate with file:///<cwd>/inline.json#/...").
# Verdicts are committed and this repo is public, so the recording
# host's path is rewritten to a stable placeholder before the verdict
# lands. Only the message text is touched; statuses and observed
# values are left exactly as the service returned them.
echo "==> scrubbing recorder-host paths from verdicts"
python3 - "$VERDICTS" "$REPO_ROOT" <<'PYSCRUB'
import json, os, sys

vdir, root = sys.argv[1], sys.argv[2]
for name in sorted(os.listdir(vdir)):
    if not name.endswith(".json"):
        continue
    path = os.path.join(vdir, name)
    raw = open(path).read()
    if "file://" + root not in raw:
        continue
    scrubbed = raw.replace("file://" + root, "file://<scenario-root>")
    json.loads(scrubbed)  # refuse to write anything that stopped parsing
    open(path, "w").write(scrubbed)
    print("    scrubbed %s" % name)
PYSCRUB

echo
python3 - "$VERDICTS" <<'PYEOF'
import json, os, sys
vdir = sys.argv[1]
rows, factors = [], {}
for name in sorted(os.listdir(vdir)):
    if not name.endswith(".json"):
        continue
    r = json.load(open(os.path.join(vdir, name)))
    rows.append((r.get("scenario_id", name), r.get("verdict", "?")))
    for f in r.get("facets", []):
        n, s = f.get("factor"), f.get("status")
        cur = factors.get(n)
        # fail dominates; pass only if nothing worse was seen
        order = {"fail": 2, "ungradable": 3, "not_implemented": 1, "pass": 0}
        if cur is None or order.get(s, 3) > order.get(cur, 3):
            factors[n] = s
print("scenario verdicts")
for sid, v in rows:
    print(f"  {sid:32} {v}")
print()
print("factor rollup (across all scenarios)")
for n in sorted(factors):
    print(f"  F{n:<3} {factors[n]}")
PYEOF

if [ "$overall" -ne 0 ]; then
  echo "MEASUREMENT FAILURE: at least one cassette was not graded" >&2
  exit 1
fi
if [ "$STRICT" = "1" ] && [ "$strict_fail" -ne 0 ]; then
  exit 2
fi
