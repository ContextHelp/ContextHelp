# Pod CLI Surface Probe — Long-Lived Daemon Runs

> **Date:** 2026-11-22
> **Subject:** Empirical verification of the `pod` CLI surface for persistent daemon runs (`ctxd`, `dpkms`) and ephemeral execution.
> **Target:** `hop-top/pod` `main` @ `489167b` (built from source, `pod vdev`, Darwin/ARM64 with Docker 29.8.2).
> **Environment:** Isolated throwaway `HOME` and XDG directories (`XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME`, `XDG_CACHE_HOME`), stdin redirected to `/dev/null` for every invocation.

## Summary

| Question | Finding |
|---|---|
| Can a long-lived process survive `pod exec` disconnect (tmux-backed)? | **CLI verb absent.** `pod exec` does not exist yet (exit 2, unknown command). Internally, `internal/ssh/tmux.go` (`pod-<name>`) and `internal/computeruse/podexec.go` (`setsid nohup ... </dev/null >log 2>&1 &`) exist, and direct `docker exec` with `setsid nohup ... </dev/null >log 2>&1 &` returns in 48 ms while the background process survives disconnect. |
| Which pod verbs map to daemon start/stop/status? | `pod capabilities`, `pod show`, `pod start`, `pod stop`, `pod create`, `pod destroy` exist. Missing for remote process control: `pod exec`, `pod forward`, and a file-upload verb. |
| Local reachability of a daemon via `pod forward`? | **CLI verb absent.** `pod forward` exits 2 (unknown command). Internally, `internal/forward/forward.go` now pins host keys via `conn.HostKeyArgs()` (`StrictHostKeyChecking=accept-new`, `UserKnownHostsFile`, `HostKeyAlias`), resolving the earlier host-key gap. However, `forwardArgs` still passes `-L <local>:localhost:<remote>` without an explicit `127.0.0.1:` bind address or `-o ExitOnForwardFailure=yes`. |
| Gaps | Five gaps identified (below) and filed upstream in `hop-top/pod`; `ctxt` keeps a strict process-boundary shell-out contract and never works around missing verbs via direct `ssh`/`docker`. |

## 1. CLI Surface (`pod capabilities --format json`)

Running `pod capabilities --format json` exits `0` with `tool: "pod"`, `version: "dev"`, `schema_version: "1.0"`, and 17 invocable leaves:

- Lifecycle: `pod create`, `pod destroy`, `pod ps`, `pod show`, `pod start`, `pod stop`, `pod use`
- Adapters & metadata: `pod adapter install`, `pod adapter list`, `pod capabilities`, `pod status`
- Tooling & sandbox: `pod agent`, `pod launch`, `pod secret list|rm|set`, `pod supervisor down|status|up`

Probing the required process-boundary verbs against the compiled binary:

| Command | Exit | Observed stderr |
|---|---|---|
| `pod exec test-pod -- echo hi` | `2` | `USAGE: unknown command "exec" for "pod"` |
| `pod forward test-pod 8080:8080` | `2` | `USAGE: unknown command "forward" for "pod"` |
| `pod show nonexistent --format json --no-hints --no-color` | `3` | JSON envelope `{"code":"NOT_FOUND","message":"pod \"nonexistent\" not found","suggested_fix":"run \`pod ps\` to list known pods","exit_code":3}` followed by rendered human error block |

Global flags (`--format`, `--no-hints`, `--no-color`, `--quiet`, `--confirm`, `--dry-run`, `-c`) are not repeated inside each leaf's `flags` array in `capabilities --format json`; only leaf-local flags appear there (for example `--note` on `start`, `stop`, `create`, `destroy`).

## 2. Lifecycle Behavior and Measured Latencies

Tested non-interactively (`</dev/null`) against both the built-in `mock` adapter and the `docker` adapter (`alpine:3.21`):

| Verb | Mock adapter | Docker adapter (`alpine:3.21`) | Notes |
|---|---|---|---|
| `pod create` | `42 ms` (exit 0) | `1,606 ms` (exit 0) | Returns JSON pod detail including `id`, `name`, `provider`, `status: "running"`, `metadata.container`. |
| `pod show` | `47 ms` (exit 0) | `43 ms` (exit 0) | Reads local SQLite row only; never contacts the provider. |
| `pod stop` | `43 ms` (exit 1 on mock: `adapter stop: domain: not found`) | `62 ms` (exit 0, status → `stopped`) | Calls `adapter.Stop` only on status transition. |
| `pod start` | `59 ms` (exit 0) | `130 ms` (exit 0, status → `running`) | Calls `adapter.Start` only when previous status was `stopped`. |
| `pod destroy --force --note ...` | `43 ms` (exit 1, policy eval error) | `43 ms` (exit 1, policy eval error) | Blocked by default CEL policy bug (gap 4 below). |

### Detached Process Survival

Pod runs no daemon of its own and does not supervise or restart remote user processes (the `pod supervisor` command manages an opt-in session supervisor for coding harnesses, not arbitrary user daemons).

Empirical check of `setsid nohup` inside an `alpine:3.21` Docker container (mirroring `internal/computeruse/podexec.go` and `internal/adapter/docker/connector.go`):

- Command: `docker exec <cid> sh -c 'setsid nohup sleep 60 </dev/null >/tmp/daemon.log 2>&1 & echo $!'`
- Return latency: `48 ms`, printing child PID (`11`) immediately without waiting for `sleep 60`.
- Follow-up `docker exec <cid> sh -c 'ps aux'` after 500 ms confirmed PID `11` (`sleep 60`) still running after the launching exec session closed.
- For SSH-backed adapters, `internal/ssh/tmux.go` provides `pod-<name>` tmux sessions (`tmux has-session` / detached session creation), which likewise survive SSH disconnect once exposed through a CLI verb.
- Adapter caveats remain per `hop-top/pod` `docs/adapters.md`: Runloop exec waits on background children (15 min cap); Runpod wipes container disk outside `/workspace` on stop; Modal does not support stop/start; E2B is ephemeral and lacks port forwarding.

## 3. Port Forwarding and Host-Key Verification

- **Fixed since October 2026:** `internal/forward/forward.go` (`sshBaseArgs`) now emits `conn.HostKeyArgs()` (`StrictHostKeyChecking=accept-new` or `yes`, `UserKnownHostsFile`, `HostKeyAlias`), verified by `internal/forward/hostkey_test.go`. Host-key verification is no longer disabled on SSH forwards.
- **Still missing on SSH forward:** `forwardArgs` emits `-L <local>:localhost:<remote>` without an explicit `127.0.0.1:` bind prefix and without `-o ExitOnForwardFailure=yes`. Loopback binding therefore still relies on the user's `ssh_config` leaving `GatewayPorts` off, and a busy local port does not guarantee a fast non-zero exit.
- **Docker forward:** `internal/adapter/docker/connector.go` explicitly sets `TCP-LISTEN:<port>,bind=127.0.0.1,fork,reuseaddr` via `socat`.

## 4. Non-Interactive Behavior, Policy, and Side Effects

- **Stdin closed:** Every tested verb (`capabilities`, `adapter list`, `create`, `show`, `start`, `stop`, `destroy`) completed without hanging or prompting when stdin was `/dev/null`.
- **Default policies (`$XDG_CONFIG_HOME/pod/policies.yaml`):**
  - `delete-requires-note`: requires `--note` on `destroy` (not on `start` or `stop`).
  - `delete-prod-vm-requires-admin`: checks `resource.fields.Spec.Tags` on delete; currently throws a CEL evaluation error (`no such key: Spec`) on every `pod destroy`.
  - `cross-region-launch-requires-note`: requires `--note` when `--region` is set on `create`.
- **Filesystem side effects:** Even read-only `pod capabilities --format json` opens/creates `$XDG_DATA_HOME/pod/pods.db` (WAL) and writes `$XDG_CONFIG_HOME/pod/policies.yaml` (mode `0600`). Running with a read-only `HOME`/`XDG_*` fails with exit `1` (`create data dir: permission denied`).
- **Stderr error shape:** Structured errors emit a JSON object `{code, message, suggested_fix, exit_code}` on stderr followed by a human-formatted error block. Unstructured errors (such as read-only data dir) emit plain `Error: ...` with exit `1`. Parsers must decode the first JSON object from stderr when present and fall back to trailing stderr lines otherwise.

## 5. Gaps Filed Upstream in `hop-top/pod`

All gaps belong in `hop-top/pod`; `ctxt` does not bypass `pod` by shelling out to `ssh` or `docker` directly:

1. **Expose `pod exec` CLI verb** — non-interactive execution over `Connector.Exec` with separated stdout/stderr, remote exit-code passthrough (including 127), and optional stdin piping.
2. **Expose `pod forward` CLI verb with explicit `127.0.0.1` bind and `ExitOnForwardFailure=yes`** — expose `internal/forward` as a CLI verb and harden `forwardArgs` to `-L 127.0.0.1:<local>:localhost:<remote>` + `-o ExitOnForwardFailure=yes`.
3. **Expose file upload (`pod upload` / `pod cp`) or stdin streaming on `pod exec`** — required to transfer `0600` config/secret files without placing secrets in remote argv.
4. **Fix `delete-prod-vm-requires-admin` CEL policy bug** — `pod destroy` fails with `no such key: Spec` under the seeded default `policies.yaml` because `Spec` is missing from delete pre-persisted payloads.
5. **Refuse `pod start` on `destroyed` or `provisioning` pods** — `LifecycleRepository.Update` currently rewrites local status to `running` without contacting a provider when `prev.Status` is not `stopped`.

## 6. Impact on `ctxt` Implementation

`ctxt` implements the full opt-in process boundary (`internal/podrun`), config surface, ambient daemon pod host, `dpkms` pod host, and pod job executor against the documented `pod` CLI contract (`capabilities`, `show`, `start`, `stop`, `create`, `destroy`, `exec`, `forward`). Because `internal/podrun` probes `pod capabilities --format json` before invoking remote verbs, invoking `--runner pod` or `jobs.executor=pod` against a `pod` build lacking `exec`/`forward` fails fast and loudly with exit `70` naming the missing leaf rather than hanging or falling back to local. Unit, state-machine, cassette (live-recorded for shipped verbs, synthetic and marked for `exec`/`forward`), and black-box fake-binary tests validate the full integration hermetically, while an opt-in `-tags=drift` test checks the installed `pod` binary's live capability document.
