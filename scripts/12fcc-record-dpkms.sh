#!/usr/bin/env bash
# Record 12fcc conformance cassettes from real dpkms runs.
#
# Sibling of scripts/12fcc-record.sh, which records the ctxt binary.
# The two are kept separate because they build different binaries from
# different entry points and iterate different scenario namespaces;
# grading (scripts/12fcc-grade.sh) then covers every cassette under
# e2e/conformance/cassettes/ regardless of which script recorded it.
#
# For every scenario under e2e/conformance/scenarios/dpkms/, drives
# `kit conformance harness record` against a freshly built binary:
# each step executes as a real subprocess; exit code, stdout, stderr,
# and duration are captured verbatim into the svc upload layout the
# grading service consumes.
#
# Captures are never edited after the fact; re-running re-records
# everything from scratch. The recorder refuses to proceed when the
# story bytes do not hash to the scenario's declared content_hash.
#
# Isolation: every scenario gets its own work dir under a pinned root
# so the substrate's database and any config writes never leak between
# scenarios or into the developer's real data. XDG_CONFIG_HOME,
# XDG_DATA_HOME, XDG_STATE_HOME, and XDG_CACHE_HOME all point inside
# the work dir and CTXT_* / CH_* / DPKMS_* / KIT_* / BUS_TOKEN env vars
# are scrubbed, so the only state a step can see is what the scenario
# itself creates.
#
# Scrubbing DPKMS_* is also why no scenario here starts a daemon:
# `dpkms serve` requires DPKMS_BUS_TOKEN, which the scrub removes by
# design. Steps that need a live instance would have to reintroduce
# ambient env, so the dpkms scenarios probe the substrate in its
# stopped state instead and say so in their own descriptions.
#
# Server routing: the client commands (healthcheck among them) dial
# server.url, and with no config they target the built-in default
# endpoint (127.0.0.1:8080), where a developer's real instance may be
# listening. The recorder therefore writes server.url into every
# scenario's own user config ($XDG_CONFIG_HOME/contexthelp/dpkms.yaml),
# pointing at a loopback port nothing listens on, and before recording
# checks that dpkms resolves exactly that file (`dpkms config path`)
# and dials exactly that URL (a healthcheck that must be refused there);
# a scenario is never recorded against the default. Routing goes
# through config rather than --server-url or env so the scenario steps
# run unmodified.
#
# After each scenario the captures are checked for any default-port
# endpoint and for recording-host paths; a hit fails the run, because
# the first would mean real data from a developer's instance and the
# second would break byte stability or leak host details. The one
# exemption is the --server-url help text, which documents the
# default endpoint and is matched as that exact phrase.
#
# The work root is a fixed path rather than $TMPDIR-derived because
# captures legitimately report paths inside it (housekeeping previews
# name the scenario's database file): a fixed root keeps those captures
# byte-stable and free of host-specific temp paths. Overriding it with
# DPKMS_12FCC_WORK changes those captures.
#
# Requirements:
#   - KIT_BIN: path to a kit binary that ships `conformance harness
#     record` (build from hop.top/kit cmd/kit). Defaults to `kit` on
#     PATH.
#   - python3 on PATH.
#
# Usage: [KIT_BIN=/path/to/kit] scripts/12fcc-record-dpkms.sh
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CONF="$REPO_ROOT/e2e/conformance"
BIN="$REPO_ROOT/bin/dpkms"
KIT_BIN="${KIT_BIN:-kit}"
WORK="${DPKMS_12FCC_WORK:-/tmp/dpkms-12fcc-record}"
OPERATOR_HOME="${HOME:-}"
OPERATOR_TMPDIR="${TMPDIR:-}"

# A loopback port nothing listens on; matches the closed endpoint the
# Go test guard (internal/testguard) routes tests to.
CLOSED_URL="http://127.0.0.1:1"
CLOSED_PORT=1
# The default local server ports (internal/testguard GuardedPorts). No
# capture may name one.
GUARDED_PORTS_RE='8080|8081|9090|9091|9377'
# The --server-url usage text documents the default endpoint; it is the
# only default-port mention a capture may carry (help steps record it).
DEFAULT_URL_HELP='(default http://127.0.0.1:8080 when nothing is configured)'

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

# fts5 is required: the substrate's search paths are compiled against
# the SQLite FTS5 module, and a binary built without the tag fails at
# open time.
echo "==> building $BIN"
mkdir -p "$REPO_ROOT/bin"
(cd "$REPO_ROOT" && go build -tags fts5 -buildvcs=false -o "$BIN" ./cmd/dpkms)
BINARY_VERSION="$(cd "$REPO_ROOT" && git rev-parse --short HEAD 2>/dev/null || echo dev)"

# Scrub ambient config sources: recorded behavior must derive from the
# scenario's own writes plus built-in defaults only. CH_* goes because
# the shared config loader binds that prefix to config keys, so an
# exported CH_SERVER_URL would re-route a step. KIT_* goes too --
# kit echoes those back in some surfaces, so an operator's KIT_BIN path
# would otherwise be baked into a committed capture. The recorder path
# is saved first: unsetting KIT_BIN clears the shell variable as well
# as the exported one.
RECORDER="$KIT_BIN"
while IFS='=' read -r name _; do
  case "$name" in CTXT_* | CH_* | DPKMS_* | KIT_* | BUS_TOKEN) unset "$name" ;; esac
done < <(env)

# port_open reports whether something accepts connections on a
# loopback port.
port_open() { (exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null; }

# --- per-scenario routing checks ------------------------------------

# write_dpkms_config writes the scenario's user-level dpkms config,
# routing the client commands to $2.
write_dpkms_config() {
  local proj="$1" url="$2"
  mkdir -p "$proj/xdg/contexthelp"
  (umask 077 && printf 'server:\n  url: %s\n' "$url" >"$proj/xdg/contexthelp/dpkms.yaml")
}

# as_scenario runs dpkms with the env a scenario's steps get (HOME and
# config in work dir $1), but data/state/cache under scratch tree $2.
as_scenario() {
  local proj="$1" pre="$2"
  shift 2
  (cd "$proj" && HOME="$proj" USER="conformance" LOGNAME="conformance" \
    XDG_CONFIG_HOME="$proj/xdg" XDG_DATA_HOME="$pre/data" \
    XDG_STATE_HOME="$pre/state" XDG_CACHE_HOME="$pre/cache" \
    "$BIN" "$@")
}

# assert_routing fails unless dpkms, run exactly as the scenario's
# steps are, reads the scenario's own config file and dials $2 (and is
# refused there). The file check runs first and needs no network, so
# the dial only happens once the routing source is confirmed.
# Data/state/cache go to a scratch tree so the check leaves no trace
# in the scenario's own dirs.
assert_routing() {
  local proj="$1" want="$2" pre="$1.preflight" got rc=0 err
  local want_cfg="$proj/xdg/contexthelp/dpkms.yaml"
  mkdir -p "$pre/data" "$pre/state" "$pre/cache"
  got="$(as_scenario "$proj" "$pre" config path --format json --no-hints 2>/dev/null |
    python3 -c 'import json,sys; print(json.load(sys.stdin).get("path") or "")' || true)"
  if [ "$got" != "$want_cfg" ]; then
    rm -rf "$pre"
    echo "ERROR: dpkms reads config '${got:-<none>}', want '$want_cfg'; refusing to record" >&2
    return 1
  fi
  err="$(as_scenario "$proj" "$pre" healthcheck --format json --no-color --no-hints 2>&1 >/dev/null)" || rc=$?
  rm -rf "$pre"
  case "$rc:$err" in
    6:*"healthcheck $want: "*"connection refused"*) ;;
    *)
      echo "ERROR: dpkms healthcheck did not dial '$want' and get refused (exit $rc); refusing to record:" >&2
      echo "$err" >&2
      return 1
      ;;
  esac
}

# check_captures fails when a capture under $1 names a default-port
# endpoint (real data from a developer's instance; the --server-url
# help phrase excepted) or a recording-host path.
check_captures() {
  local dir="$1" bad=0 hits f s
  local re="(127\.0\.0\.1|localhost|0\.0\.0\.0|\[::1\]):($GUARDED_PORTS_RE)([^0-9]|\$)"
  hits=""
  while IFS= read -r f; do
    [ -n "$f" ] || continue
    if awk -v p="$DEFAULT_URL_HELP" '{ while ((i = index($0, p)) > 0) $0 = substr($0, 1, i - 1) substr($0, i + length(p)); print }' "$f" |
      grep -qE "$re"; then
      hits="$hits$f"$'\n'
    fi
  done < <(grep -rlE "$re" "$dir" || true)
  if [ -n "$hits" ]; then
    echo "ERROR: captures name a default local server endpoint:" >&2
    printf '%s' "$hits" >&2
    bad=1
  fi
  for s in \
    "$REPO_ROOT" \
    ${OPERATOR_HOME:+"$OPERATOR_HOME/"} \
    ${OPERATOR_TMPDIR:+"${OPERATOR_TMPDIR%/}/"}; do
    hits="$(grep -rlF -e "$s" "$dir" || true)"
    if [ -n "$hits" ]; then
      echo "ERROR: captures contain '$s' (host path):" >&2
      echo "$hits" >&2
      bad=1
    fi
  done
  return "$bad"
}

if port_open "$CLOSED_PORT"; then
  echo "ERROR: something accepts connections on 127.0.0.1:$CLOSED_PORT; the closed endpoint must have no listener" >&2
  exit 1
fi

rm -rf "$WORK"
mkdir -p "$WORK"

for scenario in "$CONF"/scenarios/dpkms/*/*/scenario.yaml; do
  [ -f "$scenario" ] || continue
  sid="$(basename "$(dirname "$(dirname "$scenario")")")"
  echo "==> recording $sid"
  proj="$WORK/$sid"
  mkdir -p "$proj/xdg" "$proj/data" "$proj/state" "$proj/cache"
  rm -rf "$CONF/cassettes/$sid"
  write_dpkms_config "$proj" "$CLOSED_URL"
  assert_routing "$proj" "$CLOSED_URL"
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
  check_captures "$CONF/cassettes/$sid"
done

echo "==> cassettes written to $CONF/cassettes"
