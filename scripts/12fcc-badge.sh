#!/usr/bin/env bash
# Regenerate the `.12fc.json` conformance badge from real
# measurements only.
#
# The badge on the default branch is the source of truth for all 12
# factors. It is rebuilt from:
#
#   - verify-no-leak + verify-stories, executed here against
#     e2e/stories/                             -> F1, F2, F9, F10, F12
#   - e2e/conformance/verdicts/*.json written
#     by `scripts/12fcc-grade.sh`              -> F3-F8, F11
#
# A factor with no measurement stays `skip`; a failing leaf or verdict
# marks its factors `fail`; an ungradable verdict aborts (measurement
# itself broke -- re-run the grade script). Verdict/colour rules come
# from `kit conformance badge`, never from this script.
#
# Requirements:
#   - KIT_BIN: path to a kit binary that ships the `conformance`
#     command group (build from hop.top/kit cmd/kit). Defaults to
#     `kit` on PATH.
#
# Usage (after 12fcc-record.sh + 12fcc-grade.sh):
#   KIT_BIN=/path/to/kit scripts/12fcc-badge.sh
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CONF="$REPO_ROOT/e2e/conformance"
KIT_BIN="${KIT_BIN:-kit}"
STORY_PATHS="e2e/stories"

# The probe requires real help output (not just exit 0) so a wrong
# binary can never masquerade as a conformance-capable kit. Captured
# into a variable rather than piped: `grep -q` under pipefail would
# SIGPIPE the probe on success.
probe="$("$KIT_BIN" conformance badge --help 2>/dev/null || true)"
case "$probe" in
  *--matrix*) ;;
  *)
    echo "ERROR: '$KIT_BIN' does not ship 'conformance badge'. Set KIT_BIN to a kit binary built from hop.top/kit cmd/kit." >&2
    exit 1
    ;;
esac

TMP="$(mktemp -d "${TMPDIR:-/tmp}/ctxt-12fcc-badge.XXXXXX")"
trap 'rm -rf "$TMP"' EXIT

# Run a verify leaf and map kit's exit contract (0 clean / 2 findings)
# to a factor status. Any other exit means measurement broke: abort
# rather than guess.
run_leaf() {
  local leaf="$1" rc
  set +e
  (cd "$REPO_ROOT" && "$KIT_BIN" conformance "$leaf" \
    --paths="$STORY_PATHS" --format=json --no-hints -o "$TMP/$leaf.json") \
    >/dev/null 2>&1
  rc=$?
  set -e
  case "$rc" in
    0) echo pass ;;
    2) echo fail ;;
    *)
      echo "ERROR: '$KIT_BIN conformance $leaf' exited $rc (want 0 or 2):" >&2
      cat "$TMP/$leaf.json" >&2
      exit 1
      ;;
  esac
}

# verify-no-leak takes explicit FILE paths: handed a directory it
# reports "unsupported extension" and scans nothing, which would make
# F10 evidence vacuous. Expand the story files and assert a non-zero
# scan count so a silent no-op can never read as a pass.
echo "==> verify-no-leak ($STORY_PATHS)"
# Shell glob, not `ls`: an interactive `ls` alias (exa/eza and friends)
# would otherwise inject a long-format listing into the path list.
STORY_FILES=""
for story in "$REPO_ROOT/$STORY_PATHS"/*.yaml; do
  [ -f "$story" ] || continue
  STORY_FILES="${STORY_FILES:+$STORY_FILES,}$story"
done
if [ -z "$STORY_FILES" ]; then
  echo "ERROR: no story files under $STORY_PATHS" >&2
  exit 1
fi
set +e
(cd "$REPO_ROOT" && "$KIT_BIN" conformance verify-no-leak \
  --paths="$STORY_FILES" --format=json --no-hints -o "$TMP/verify-no-leak.json") \
  >/dev/null 2>&1
leak_rc=$?
set -e
case "$leak_rc" in
  0) NO_LEAK_STATUS=pass ;;
  2) NO_LEAK_STATUS=fail ;;
  *)
    echo "ERROR: verify-no-leak exited $leak_rc (want 0 or 2):" >&2
    cat "$TMP/verify-no-leak.json" >&2
    exit 1
    ;;
esac
scanned="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("scanned_files",0))' "$TMP/verify-no-leak.json")"
if [ "$scanned" -eq 0 ]; then
  echo "ERROR: verify-no-leak scanned 0 files -- F10 would be unmeasured" >&2
  exit 1
fi
echo "    $NO_LEAK_STATUS ($scanned files scanned)"

echo "==> verify-stories ($STORY_PATHS)"
STORIES_STATUS="$(run_leaf verify-stories)"
stories_scanned="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("scanned_files",0))' "$TMP/verify-stories.json" 2>/dev/null || echo 0)"
if [ "$stories_scanned" -eq 0 ]; then
  echo "ERROR: verify-stories scanned 0 files -- F1/F2/F9/F12 would be unmeasured" >&2
  exit 1
fi
echo "    $STORIES_STATUS ($stories_scanned files scanned)"

echo "==> assembling per-factor matrix from verdicts/"
python3 - "$CONF/verdicts" "$TMP/matrix.json" "$NO_LEAK_STATUS" "$STORIES_STATUS" <<'PYEOF'
import json, os, sys

vdir, out, no_leak, stories = sys.argv[1:5]

FACTORS = [
    (1,  "Capability Introspection", "must"),
    (2,  "Intent Clarity",           "must"),
    (3,  "Structured I/O",           "must"),
    (4,  "Corrective Error Model",   "must"),
    (5,  "Explicit Contracts",       "must"),
    (6,  "Previewability",           "must"),
    (7,  "Idempotency",              "must"),
    (8,  "State Transparency",       "must"),
    (9,  "Contextual Guidance",      "should"),
    (10, "Delegation Safety",        "must"),
    (11, "Exit Code Semantics",      "must"),
    (12, "Evolution Guarantees",     "must"),
]

status = {n: "skip" for n, _, _ in FACTORS}
evidence = {n: "" for n, _, _ in FACTORS}

for n in (1, 2, 9, 12):
    status[n] = stories
    evidence[n] = "verify-stories " + ("clean" if stories == "pass" else "reported findings")
status[10] = no_leak
evidence[10] = "verify-no-leak " + ("clean" if no_leak == "pass" else "reported findings")

names = sorted(f for f in os.listdir(vdir) if f.endswith(".json")) if os.path.isdir(vdir) else []
if not names:
    sys.exit(f"ERROR: no verdicts in {vdir}; run scripts/12fcc-grade.sh first")

for name in names:
    sid = name[:-len(".json")]
    with open(os.path.join(vdir, name)) as f:
        v = json.load(f)
    for facet in v.get("facets", []):
        n, s = facet.get("factor"), facet.get("status")
        if n not in status:
            sys.exit(f"ERROR: {name}: facet for unknown factor {n!r}")
        if s not in ("pass", "fail"):
            sys.exit(
                f"ERROR: {name}: F{n} is {s!r} -- measurement broke; re-run scripts/12fcc-grade.sh"
            )
        if s == "fail":
            status[n] = "fail"
            evidence[n] = f"tier-3 verdict fail ({sid})"
        elif status[n] == "skip":
            status[n] = "pass"
            evidence[n] = "tier-3 cassette verdicts"

matrix = {
    "schemaVersion": 1,
    "factors": [
        {"n": n, "name": name, "tier": tier,
         "status": status[n], "evidence": evidence[n]}
        for n, name, tier in FACTORS
    ],
}
with open(out, "w") as f:
    json.dump(matrix, f, indent=2)

for n, name, tier in FACTORS:
    print(f"    F{n:<3} {status[n]:<5} {name}")
PYEOF

echo "==> rendering badge (.12fc.json)"
(cd "$REPO_ROOT" && "$KIT_BIN" conformance badge \
  --matrix="$TMP/matrix.json" --output=".12fc.json" >/dev/null)
python3 - "$REPO_ROOT/.12fc.json" <<'PYEOF'
import json, sys
with open(sys.argv[1]) as f:
    b = json.load(f)
print(f"    {b['label']}: {b['message']} ({b['color']})")
PYEOF
