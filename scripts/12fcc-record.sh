#!/usr/bin/env bash
# Record 12fcc conformance cassettes from real ctxt runs.
#
# For every scenario under e2e/conformance/scenarios/ctxt/, drives
# `kit conformance harness record` against a freshly built binary:
# each step executes as a real subprocess; exit code, stdout, stderr,
# and duration are captured verbatim into the svc upload layout the
# grading service consumes:
#
#   e2e/conformance/cassettes/<scenario-id>/
#     manifest.yaml
#     story.yaml
#     steps/<step-id>/{result.json,stdout.txt,stderr.txt}
#
# Captures are never edited after the fact; re-running re-records
# everything from scratch. The recorder refuses to proceed when the
# story bytes do not hash to the scenario's declared content_hash.
#
# Isolation: every scenario gets its own work dir under a pinned root
# so the ctxt knowledge database and any config writes never leak
# between scenarios or into the developer's real data.
# XDG_CONFIG_HOME, XDG_DATA_HOME, XDG_STATE_HOME, and XDG_CACHE_HOME
# all point inside the work dir and CTXT_* / DPKMS_* / KIT_* env vars
# are scrubbed, so the only state a step can see is what the scenario
# itself creates.
#
# Requirements:
#   - KIT_BIN: path to a kit binary that ships `conformance harness
#     record` (build from hop.top/kit cmd/kit). Defaults to `kit` on
#     PATH.
#
# Usage: [KIT_BIN=/path/to/kit] scripts/12fcc-record.sh
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CONF="$REPO_ROOT/e2e/conformance"
BIN="$REPO_ROOT/bin/ctxt"
KIT_BIN="${KIT_BIN:-kit}"
WORK="${CTXT_12FCC_WORK:-${TMPDIR:-/tmp}/ctxt-12fcc-record}"

# The probe requires real help output (not just exit 0) so a wrong
# binary can never masquerade as a conformance-capable kit.
probe="$("$KIT_BIN" conformance harness record --help 2>/dev/null || true)"
case "$probe" in
  *--scenario*) ;;
  *)
    echo "ERROR: '$KIT_BIN' does not ship 'conformance harness record'. Set KIT_BIN to a kit binary built from hop.top/kit cmd/kit." >&2
    exit 1
    ;;
esac

# fts5 is required: ctxt's search paths are compiled against the SQLite
# FTS5 module, and a binary built without the tag fails at open time.
echo "==> building $BIN"
mkdir -p "$REPO_ROOT/bin"
(cd "$REPO_ROOT" && go build -tags fts5 -buildvcs=false -o "$BIN" ./cmd/ctxt)
BINARY_VERSION="$(cd "$REPO_ROOT" && git rev-parse --short HEAD 2>/dev/null || echo dev)"

# Scrub ambient config sources: recorded behavior must derive from the
# scenario's own writes plus built-in defaults only. KIT_* goes too --
# kit echoes those back in some surfaces, so an operator's KIT_BIN path
# would otherwise be baked into a committed capture. The recorder path
# is saved first: unsetting KIT_BIN clears the shell variable as well
# as the exported one.
RECORDER="$KIT_BIN"
while IFS='=' read -r name _; do
  case "$name" in CTXT_*|DPKMS_*|KIT_*) unset "$name" ;; esac
done < <(env)

rm -rf "$WORK"
mkdir -p "$WORK"

for scenario in "$CONF"/scenarios/ctxt/*/*/scenario.yaml; do
  [ -f "$scenario" ] || continue
  sid="$(basename "$(dirname "$(dirname "$scenario")")")"
  echo "==> recording $sid"
  proj="$WORK/$sid"
  mkdir -p "$proj/xdg" "$proj/data" "$proj/state" "$proj/cache"
  rm -rf "$CONF/cassettes/$sid"
  # USER is pinned to a fixed token: some surfaces report the invoking
  # user, and captures are committed, so the recording host's account
  # name must never reach the repo.
  HOME="$proj" \
  USER="conformance" \
  LOGNAME="conformance" \
  XDG_CONFIG_HOME="$proj/xdg" \
  XDG_DATA_HOME="$proj/data" \
  XDG_STATE_HOME="$proj/state" \
  XDG_CACHE_HOME="$proj/cache" \
    "$RECORDER" conformance harness record \
      --scenario "$scenario" \
      --binary "$BIN" \
      --binary-version "$BINARY_VERSION" \
      --out "$CONF/cassettes/$sid" \
      --workdir "$proj" \
      --no-hints >/dev/null
done

echo "==> cassettes written to $CONF/cassettes"
