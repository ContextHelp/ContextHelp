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
# all point inside the work dir and CTXT_* / CH_* / DPKMS_* / KIT_* /
# BUS_TOKEN env vars are scrubbed, so the only state a step can see is
# what the scenario itself creates.
#
# Server routing: ctxt commands run over the dpkms API, and with no
# config ctxt targets the built-in default endpoint (127.0.0.1:8080),
# where a developer's real instance may be listening. The recorder
# therefore writes server.url into every scenario's own user config
# ($XDG_CONFIG_HOME/contexthelp/ctxt.yaml) before recording, and
# verifies through `ctxt config show` that ctxt resolves exactly that
# URL; a scenario is never recorded against the default. Each scenario
# runs in one of two server modes (see server_mode below):
#
#   live    a throwaway `dpkms serve` started for that scenario alone:
#           private access (loopback only, no inbound tokens), its own
#           HOME/XDG tree and SQLite database, a per-run random bus
#           token held only in the daemon's env, stub providers and
#           embeddings, HTTP and gRPC on OS-assigned ports (never one
#           of the default ports). It leads its own process group and
#           is stopped, group and all, before the next scenario starts
#           and on every exit path, and the recorder waits until its
#           port no longer accepts connections.
#   closed  server.url points at a port nothing listens on, for
#           scenarios that test behavior with no server running.
#
# After each scenario the captures are checked for any default-port
# endpoint, the live instance's address, its bus token, and recording
# host paths; a hit fails the run, because the first would mean real
# data and the others would break byte stability or leak host details.
#
# The work root is a fixed path rather than $TMPDIR-derived because a
# capture can legitimately report a path inside it (`ctxt config path`
# names the scenario's config file): a fixed root keeps that capture
# byte-stable and free of host-specific temp paths. Overriding it with
# CTXT_12FCC_WORK changes those captures.
#
# Requirements:
#   - KIT_BIN: path to a kit binary that ships `conformance harness
#     record` (build from hop.top/kit cmd/kit). Defaults to `kit` on
#     PATH.
#   - curl and python3 on PATH.
#
# Usage: [KIT_BIN=/path/to/kit] scripts/12fcc-record.sh
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CONF="$REPO_ROOT/e2e/conformance"
BIN="$REPO_ROOT/bin/ctxt"
DPKMS_BIN="$REPO_ROOT/bin/dpkms"
KIT_BIN="${KIT_BIN:-kit}"
WORK="${CTXT_12FCC_WORK:-/tmp/ctxt-12fcc-record}"
OPERATOR_HOME="${HOME:-}"
OPERATOR_TMPDIR="${TMPDIR:-}"

# A loopback port nothing listens on; matches the closed endpoint the
# Go test guard (internal/testguard) routes tests to.
CLOSED_URL="http://127.0.0.1:1"
# The default local server ports (internal/testguard GuardedPorts). No
# throwaway instance may bind one, and no capture may name one.
GUARDED_PORTS="8080 8081 9090 9091 9377"

# server_mode prints the server mode for a scenario id, decided from
# what the scenario and its story say they test. An unclassified
# scenario is refused rather than given a default.
server_mode() {
  case "$1" in
    # Orientation "works with nothing running" and must not "depend on
    # a server being up": record it with no server reachable.
    offline-state-envelope) echo closed ;;
    # Read, search, resolve and delete-preview surfaces are served by
    # the API, and the profile reconcile is exercised as an operator
    # runs it, with the substrate up.
    delete-preview | format-contract | profile-idempotency | resolve-contract | state-transparency) echo live ;;
    *)
      echo "ERROR: scenario '$1' has no server mode; classify it in server_mode in $0" >&2
      return 1
      ;;
  esac
}

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

# fts5 is required: ctxt's search paths and the substrate are compiled
# against the SQLite FTS5 module, and a binary built without the tag
# fails at open time.
echo "==> building $BIN and $DPKMS_BIN"
mkdir -p "$REPO_ROOT/bin"
(cd "$REPO_ROOT" && go build -tags fts5 -buildvcs=false -o "$BIN" ./cmd/ctxt)
(cd "$REPO_ROOT" && go build -tags fts5 -buildvcs=false -o "$DPKMS_BIN" ./cmd/dpkms)
BINARY_VERSION="$(cd "$REPO_ROOT" && git rev-parse --short HEAD 2>/dev/null || echo dev)"

# Scrub ambient config sources: recorded behavior must derive from the
# scenario's own writes plus built-in defaults only. CH_* goes because
# ctxt binds that prefix to config keys, so an exported CH_SERVER_URL
# would re-route a step. KIT_* goes too -- kit echoes those back in
# some surfaces, so an operator's KIT_BIN path would otherwise be baked
# into a committed capture. The recorder path is saved first: unsetting
# KIT_BIN clears the shell variable as well as the exported one.
RECORDER="$KIT_BIN"
while IFS='=' read -r name _; do
  case "$name" in CTXT_* | CH_* | DPKMS_* | KIT_* | BUS_TOKEN) unset "$name" ;; esac
done < <(env)

# --- throwaway dpkms lifecycle --------------------------------------

DAEMON_PID=""
DAEMON_PORT=""
DAEMON_TOKEN=""
DAEMON_ROOT=""

# port_open reports whether something accepts connections on a
# loopback port.
port_open() { (exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null; }

# alive reports whether pid is running (a reaped or zombie process is
# not).
alive() {
  local st
  st="$(ps -o stat= -p "$1" 2>/dev/null || true)"
  [ -n "$st" ] && [ "${st#Z}" = "$st" ]
}

# stop_daemon sends SIGTERM to the instance's process group, escalates
# to SIGKILL after a grace period, reaps the leader, kills anything left
# in its group, and waits until its port stops accepting connections.
# Idempotent; a no-op when no instance is running.
stop_daemon() {
  local pid="$DAEMON_PID" port="$DAEMON_PORT"
  [ -n "$pid" ] || return 0
  DAEMON_PID=""
  DAEMON_PORT=""
  kill -TERM -- "-$pid" 2>/dev/null || kill -TERM "$pid" 2>/dev/null || true
  for _ in $(seq 1 100); do
    alive "$pid" || break
    sleep 0.1
  done
  if alive "$pid"; then
    kill -KILL -- "-$pid" 2>/dev/null || kill -KILL "$pid" 2>/dev/null || true
  fi
  wait "$pid" 2>/dev/null || true
  kill -KILL -- "-$pid" 2>/dev/null || true
  if [ -n "$port" ]; then
    for _ in $(seq 1 100); do
      port_open "$port" || return 0
      sleep 0.1
    done
    echo "ERROR: port $port still accepts connections after dpkms (pid $pid) stopped" >&2
    return 1
  fi
}

on_exit() {
  local rc=$?
  stop_daemon || rc=1
  exit "$rc"
}
trap on_exit EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# start_daemon starts a private `dpkms serve` rooted at $1 and waits
# until /health answers. Sets DAEMON_PID, DAEMON_PORT, DAEMON_TOKEN.
start_daemon() {
  local dd="$1" f deadline
  DAEMON_ROOT="$dd"
  mkdir -p "$dd/home" "$dd/xdg/contexthelp" "$dd/data" "$dd/state" "$dd/cache" "$dd/run"
  DAEMON_TOKEN="$(od -An -N16 -tx1 /dev/urandom | tr -d ' \n')"
  (
    umask 077
    cat >"$dd/xdg/contexthelp/dpkms.yaml" <<EOF
storage:
  type: sqlite
  path: $dd/data/dpkms.db
server:
  access: private
providers:
  video: {backend: stub}
  document: {backend: stub}
  ocr: {backend: stub}
  transcription: {backend: stub}
  vision: {backend: stub}
  diarization: {backend: stub}
  llm: {backend: stub}
browser:
  enabled: false
jobs:
  drain_timeout: 2s
EOF
  )
  # Job control puts the background job in its own process group, so
  # stop_daemon can signal the instance together with anything it
  # spawns. The subshell execs, so $! is dpkms itself.
  set -m
  (
    cd "$dd"
    exec env \
      HOME="$dd/home" \
      XDG_CONFIG_HOME="$dd/xdg" \
      XDG_DATA_HOME="$dd/data" \
      XDG_STATE_HOME="$dd/state" \
      XDG_CACHE_HOME="$dd/cache" \
      XDG_RUNTIME_DIR="$dd/run" \
      DPKMS_BUS_TOKEN="$DAEMON_TOKEN" \
      CTXT_EMBEDDING_PROVIDER=stub \
      CTXT_EMBEDDING_ENDPOINT="$CLOSED_URL" \
      "$DPKMS_BIN" serve --port 0 --grpc-port 0 --workers 1
  ) >"$dd/serve.log" 2>&1 &
  DAEMON_PID=$!
  set +m

  # The pidfile is written once the HTTP listener is bound and names
  # the port the OS assigned.
  deadline=$((SECONDS + 60))
  while :; do
    if ! alive "$DAEMON_PID"; then
      echo "ERROR: dpkms serve exited before it was ready:" >&2
      cat "$dd/serve.log" >&2
      return 1
    fi
    if [ "$SECONDS" -ge "$deadline" ]; then
      echo "ERROR: dpkms serve not ready within 60s:" >&2
      cat "$dd/serve.log" >&2
      return 1
    fi
    if [ -z "$DAEMON_PORT" ]; then
      for f in "$dd"/data/contexthelp/run/*.pid; do
        [ -f "$f" ] || continue
        grep -q "\"pid\":$DAEMON_PID," "$f" || continue
        DAEMON_PORT="$(sed -n 's/.*"port":\([0-9][0-9]*\).*/\1/p' "$f")"
      done
    fi
    if [ -n "$DAEMON_PORT" ] && curl -fsS --max-time 2 "http://127.0.0.1:$DAEMON_PORT/health" >/dev/null 2>&1; then
      break
    fi
    sleep 0.1
  done
  for p in $GUARDED_PORTS; do
    if [ "$DAEMON_PORT" = "$p" ]; then
      echo "ERROR: throwaway dpkms bound default port $p" >&2
      return 1
    fi
  done
}

# --- per-scenario routing checks ------------------------------------

# write_ctxt_config writes the scenario's user-level ctxt config,
# routing ctxt to $2.
write_ctxt_config() {
  local proj="$1" url="$2"
  mkdir -p "$proj/xdg/contexthelp"
  (umask 077 && printf 'server:\n  url: %s\n' "$url" >"$proj/xdg/contexthelp/ctxt.yaml")
}

# assert_routing fails unless ctxt, run exactly as the scenario's steps
# are, resolves server.url to $2. Data/state/cache go to a scratch tree
# so the check leaves no trace in the scenario's own dirs.
assert_routing() {
  local proj="$1" want="$2" pre="$1.preflight" got
  mkdir -p "$pre/data" "$pre/state" "$pre/cache"
  got="$(cd "$proj" && HOME="$proj" USER="conformance" LOGNAME="conformance" \
    XDG_CONFIG_HOME="$proj/xdg" XDG_DATA_HOME="$pre/data" \
    XDG_STATE_HOME="$pre/state" XDG_CACHE_HOME="$pre/cache" \
    "$BIN" config show --format json --no-hints 2>/dev/null |
    python3 -c 'import json,sys; d=json.load(sys.stdin); s=d.get("server") or d.get("Server") or {}; print(s.get("url") or s.get("URL") or "")')"
  rm -rf "$pre"
  if [ "$got" != "$want" ]; then
    echo "ERROR: ctxt resolves server.url to '${got:-<unset>}', want '$want'; refusing to record" >&2
    return 1
  fi
}

# check_captures fails when a capture under $1 names a default-port
# endpoint (real data from a developer's instance), the live instance's
# address on port $2 or its bus token (not byte-stable), or a
# recording-host path.
check_captures() {
  local dir="$1" port="$2" bad=0 hits s
  hits="$(grep -rlE '(127\.0\.0\.1|localhost|0\.0\.0\.0|\[::1\]):(8080|8081|9090|9091|9377)([^0-9]|$)' "$dir" || true)"
  if [ -n "$hits" ]; then
    echo "ERROR: captures name a default local server endpoint:" >&2
    echo "$hits" >&2
    bad=1
  fi
  for s in \
    ${DAEMON_TOKEN:+"$DAEMON_TOKEN"} \
    ${port:+"127.0.0.1:$port"} \
    ${port:+"localhost:$port"} \
    ${DAEMON_ROOT:+"$DAEMON_ROOT"} \
    "$REPO_ROOT" \
    ${OPERATOR_HOME:+"$OPERATOR_HOME/"} \
    ${OPERATOR_TMPDIR:+"${OPERATOR_TMPDIR%/}/"}; do
    hits="$(grep -rlF -e "$s" "$dir" || true)"
    if [ -n "$hits" ]; then
      echo "ERROR: captures contain '$s' (instance address/token or host path):" >&2
      echo "$hits" >&2
      bad=1
    fi
  done
  return "$bad"
}

rm -rf "$WORK"
mkdir -p "$WORK"

for scenario in "$CONF"/scenarios/ctxt/*/*/scenario.yaml; do
  [ -f "$scenario" ] || continue
  sid="$(basename "$(dirname "$(dirname "$scenario")")")"
  mode="$(server_mode "$sid")"
  echo "==> recording $sid (server: $mode)"
  proj="$WORK/$sid"
  mkdir -p "$proj/xdg" "$proj/data" "$proj/state" "$proj/cache"
  rm -rf "$CONF/cassettes/$sid"
  DAEMON_TOKEN=""
  DAEMON_ROOT=""
  port=""
  case "$mode" in
    live)
      start_daemon "$WORK/$sid.dpkms"
      url="http://127.0.0.1:$DAEMON_PORT"
      ;;
    closed) url="$CLOSED_URL" ;;
  esac
  write_ctxt_config "$proj" "$url"
  assert_routing "$proj" "$url"
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
  port="$DAEMON_PORT"
  stop_daemon
  check_captures "$CONF/cassettes/$sid" "$port"
done

echo "==> cassettes written to $CONF/cassettes"
