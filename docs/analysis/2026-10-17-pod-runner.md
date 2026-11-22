# Opt-in Pod Runner — Detection, Config and Process Boundary

> **Date:** 2026-10-17
> **Applies to:** ctxt, ctxd, dpkms
> **Status:** Design note. Docs only, no code. Statements about pod carry an evidence tag (below). An untagged statement about pod is UNVERIFIED until the probe in section 13 runs.
> **References:** ADR-066, `docs/ambient.md`, `internal/ambient/daemon`, `cmd/ctxd/app`, `cmd/dpkms/cmd/serve.go`, `internal/server/http/middleware_host.go`, hop-top/pod `main` @ 6e428c7

pod is a hop.top CLI that provisions remote compute behind adapters and gives sessions on it. This note designs an optional path where ctxt launches `ctxd` or `dpkms` on a pod instead of on the local machine. ctxt must keep working exactly as it does today when pod is absent, and when pod is present but not asked for.

Evidence tags:

- **[ctxt]**: read in this repo at `f0a2236a`.
- **[pod-src]**: read in hop-top/pod `main` @ 6e428c7 (2026-08-25).
- **[pod-bin]**: observed by running the binary found in that checkout's `bin/` directory (modified 2026-08-24, a day before the tip commit; its build commit is not recorded, and `pod --version` prints `pod vdev`). Only `--help`, `--help-all`, `capabilities`, `ps`, `show` on a missing pod, `create --dry-run` with no provider, `adapter list`, and `exec` / `forward` as unknown verbs were run. HOME and the XDG dirs pointed at a throwaway directory and PATH held only system dirs. No provider was contacted and nothing was created.

## 1. Decisions in short

1. Opt-in only. `daemon.runner = local | pod`, default `local`, flag `--runner`. Detection alone never changes behavior.
2. Shell-out only. A new package `internal/podrun` runs `pod` through `os/exec`. No Go import of pod, no new module dependency.
3. `runner=pod` with no `pod` binary is an error (exit 70), never a silent fallback to local. This is the same principle as ADR-077 §2 (no fallback, loud failure), which that ADR states for data paths. `runner=local` never looks for `pod` at all.
4. Every pod call has a deadline, a closed stdin and a killable process group. A pod that is present but unusable yields an error, not a hang.
5. v1 adopts an existing, named pod. ctxt does not create or destroy pods, and it starts a pod only when `pod show` says `stopped` (section 5, pod gate).
6. The run file and lock model stays on the pod. The laptop keeps a cache record and reaches ctxd's control API through a forward opened per command.
7. Implementation is blocked on pod. The pod tree reviewed has no `exec` and no `forward` verb (section 2.3). The core package, config and tests can be built now against fakes. The dpkms and ctxd phases also wait on two transport fixes in pod: host-key verification and an explicit loopback bind for its SSH forward (section 9).
8. ctxd on a pod can only run sources that do not read the user's machine. The larger payoff is dpkms on a pod (section 2.4, open question 1).

## 2. What exists today

### 2.1 ctxd [ctxt]

| Fact | Where |
|---|---|
| Launch path: `ctxt capture --ambient` → `startAmbient` → `daemon.FindCtxd` + `daemon.Start` → `ctxd` → `app.Main` → `daemon.Run`. | `cmd/ctxt/cmd/capture_ambient.go`, `internal/ambient/daemon/launch.go`, `cmd/ctxd/app/app.go` |
| `Start` detaches ctxd (`Setsid` on unix, `DETACHED_PROCESS` and `CREATE_NEW_PROCESS_GROUP` on windows), appends output to `ctxd.log`, waits up to 20 s for the lock and an answering control API, and turns an early exit into a `StartError` carrying the log. | `launch.go`, `launch_unix.go`, `launch_windows.go` |
| One ctxd per state dir. An `flock` on `ctxd.lock` guards it. A second ctxd exits 4 with `RunningError`. A lock left by a dead process is free again. | `runfile.go` |
| `ctxd.run` (mode 0600, written atomically) holds pid, start time, launcher, build, instance, endpoint, control `host:port` and a bearer token. A run file without a live lock is stale and ignored. | `runfile.go` (`Acquire`, `ReadRun`) |
| State dir is `$XDG_STATE_HOME/ctxt/ambient`. No flag changes it. | `StateDir`, `app.go` |
| Control API: HTTP on `127.0.0.1:0`, bearer token, `/v1/status`, `/v1/stop`, `/v1/tail`, `/v1/buffer/flush`, `/v1/buffer/purge`. It checks the token and nothing else (no Host check). `stop` drains for up to 10 s. | `control.go`, `internal/ambient/runner.go` |
| ctxd reads the ctxt config on the host it runs on. `ctxdArgs` forwards `-c` tokens, `--server`, `--instance`, `--profile`, `--input`, `--skip`. Server tokens come only from config. | `capture_ambient.go`, `internal/dpkmsclient/resolve.go` |
| Sources: `browserhistory` (on by default), `clipboard`, `filewatch` (off), `foreground`, `screenshot`, `meeting` (no backend). All read state of the host they run on. A default-on source that cannot run is `unavailable`. An explicit one that cannot run stops ctxd. No runnable source is exit 2. | `internal/ambient/daemon/plan.go`, `docs/ambient.md` |
| Only the local buffer is wired. An S3 buffer package exists but config cannot select it. | `host.go`, `internal/ambient/buffer/s3` |
| ctxd does not restart itself. A service manager does that for a local install. | `docs/ambient.md` |
| `ambient.Runner` is the in-process event pipeline (sources → dedup → enqueue). It is not a process launcher. | `internal/ambient/runner.go` |

### 2.2 dpkms [ctxt]

| Fact | Where |
|---|---|
| `dpkms serve` runs in the foreground. `--daemon` re-execs detached with stdin, stdout and stderr on `/dev/null` and returns without waiting for readiness, so a startup failure is invisible. | `cmd/dpkms/cmd/serve.go` (`daemonize`) |
| A port the operator sets (flag, env, config) binds exactly or serve fails. A built-in default falls back to an OS-assigned port. `--port 0` is allowed. | `serve.go`, `serve_listen.go` |
| Access class `private` binds `127.0.0.1` and needs no inbound auth. `protected` and `public` bind `0.0.0.0` and need inbound auth. There is no setting for loopback plus auth. | `serve.go`, `internal/config/config.go` |
| A private instance answers only `127.0.0.1:<bound port>` and `localhost:<bound port>` in the `Host` header, plus `server.allowed_hosts`. Anything else is 403 `HOST_NOT_ALLOWED`. | `internal/server/http/middleware_host.go`, `stack.HostAllowlist` |
| `dpkms ps --format json` scans pidfiles in `<data dir>/run/`. `dpkms shutdown` stops an instance gracefully. `/healthz` reports build and start time. | `ps.go`, `shutdown.go`, `serve.go` |
| Without a TTY and with several profiles and no default, serve starts with no default profile instead of prompting. | `serve.go` (`resolveDefaultProfile`) |

### 2.3 pod

Registered verbs: `adapter`, `agent`, `capabilities`, `create`, `destroy`, `launch`, `ps`, `show`, `start`, `status`, `stop`, `use` [pod-src] [pod-bin].

| Fact | Evidence |
|---|---|
| There is no `exec`, `ssh`, `forward`, `sync`, `top`, `model` or `config` verb. `pod exec x y` and `pod forward x 80` fail as unknown commands, exit 2. The README `Commands` block and the roadmap list them. The user stories for ssh, exec, persistent sessions, explicit port forwarding and port detection are marked Planned. `pod <name>` only prints guidance ("requires ssh-remote track"). `launch` is built with a nil SSH config and a nil forwarder. | `internal/cli/root.go`, `launch.go`, README, `docs/stories/US-0106,0107,0117,0304,0305` [pod-src] [pod-bin] |
| The machinery exists inside the module but is not reachable from the CLI: `internal/ssh` (tmux sessions named `pod-<name>`), `internal/forward` (`ssh -L`), `internal/sync`, and `domain.Connector{Exec, Upload, Download, Forward}`. All of it is under `internal/`, so no other module can import it. | [pod-src] |
| `Connector.Exec(ctx, cmd string) (stdout, stderr string, err)` buffers output and takes no stdin. | `internal/domain/connector.go` [pod-src] |
| pod's SSH paths turn host-key checking off: `-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null` (forwarder, tmux, dotfiles, file sync, the managed ssh config entry, and the runpod and exe.dev adapters; the runloop snippet sets `StrictHostKeyChecking no`). The forwarder builds `-L <lp>:localhost:<rp>` with no bind address, so the local end is loopback only while the user's ssh config leaves `GatewayPorts` off. The docker adapter's socat forward does bind `127.0.0.1` explicitly. | `internal/forward/forward.go` (`sshBaseArgs`, `forwardArgs`), `internal/ssh/tmux.go`, `internal/ssh/config.go`, `internal/sync/push.go`, `internal/adapter/docker/connector.go`, `docs/adapters.md` [pod-src] |
| `ps`, `show`, `start` and `stop` print `{name, id, provider, status, active, image, gpu, gpu_count, vllm_build, computer_use, view_url (omitted when empty), created_at, updated_at, metadata}` under `--format json`. Status is one of `provisioning`, `running`, `stopped`, `destroyed` and is the local row. `ps` and `show` never call the provider. | `internal/cli/output.go`, `ps.go`, `show.go` [pod-src] |
| `start` and `stop` call the adapter only when the status changes. Repeating either is a local no-op. `start` calls `adapter.Start` only when the previous status was `stopped`. On a `destroyed` or `provisioning` pod, `pod start` rewrites the local row to `running` and contacts no provider. `pod destroy` only soft-marks the row, so `show` keeps reporting `destroyed`. | `internal/domain/lifecycle_repo.go` (`Update`), `internal/cli/destroy_test.go` [pod-src] |
| Adapters differ. exe.dev and ssh-only: stop is advisory. Sprites: stop advisory, start wakes the sprite with an exec. Modal: stop and start return errors. Runpod: stop wipes the container disk, only `/workspace` survives. Runloop: suspend snapshots disk only, running daemons are lost. E2B: ephemeral. | `docs/adapters.md` [pod-src] |
| Most errors are a JSON envelope `{code, message, suggested_fix, exit_code}` on stderr, followed by a human block. Not all: an error with no envelope prints `Error: <message>` and exits 1 (adapter start and stop failures arrive as `start pod: ...` or `lifecycle: ...`), and a policy denial prints `Error: ...` and exits 4. Exit codes seen: 0 ok, 1 generic, 2 usage, 3 not found, 4 conflict or policy denial, 5 unauthorized (a `destroy` refused without confirmation), 130 context cancelled. kit also defines 6 (transient), 7, 64, 65 and 70, which an envelope would carry the same way. | [pod-bin], `cmd/pod/main.go`, `internal/cli/policy.go`, `internal/cli/destroy_test.go` [pod-src] |
| Useful global flags: `--format`, `--no-hints`, `--no-color`, `--quiet`, `--confirm`, `--dry-run`, `-c`, `--chdir`, `--api-version`. State-changing verbs take `--note/-n`. | `--help-all` [pod-bin] |
| `pod capabilities --format json` returns `{tool, version, schema_version: "1.0", leaves: [{path, side_effect, idempotent, dry_run_supported, flags}]}`. It is read-only. | `internal/cli/capabilities.go` [pod-src] [pod-bin] |
| Every run, even `--help`, opens a SQLite state DB (`pods.db`, WAL) under the XDG data dir and writes a default `policies.yaml` under the XDG config dir. | [pod-bin] |
| `pod create` with no `--provider` fails with `multiple adapters registered, specify --provider (available: mock, ssh)`, exit 1. `mock` and `ssh` always register. Other adapters register only when their binary or credentials exist. There is no default-provider key. | `internal/domain/registry.go` (`Default`), `internal/cli/create.go`, `cmd/pod/main.go` [pod-src] [pod-bin] |
| The adapter-install prompt is skipped when stdin is not a TTY. | `internal/cli/adapter.go` [pod-src] |
| pod runs no daemon. Idle viewer teardown happens on the next `pod start`. Nothing restarts a dead remote process. | README, `docs/adr/OPEN-DECISIONS.md` [pod-src] |
| pod's own detached-process pattern is `setsid nohup <cmd> </dev/null >log 2>&1 &` with the pid in a state dir. It is an internal API, not a verb. On Runloop a command that leaves background children never reports completed (15 minute cap). | `internal/computeruse/podexec.go`, `docs/adapters.md` [pod-src] |
| pod's own tests state that cassette replay stays green after an upstream CLI drops a verb, and add a `-tags=drift` check that asks the installed binary. | `tests/drift/clidrift_test.go` [pod-src] |

### 2.4 Where the premises and the code differ

1. The launch path is not `internal/ambient/runner.go`. That file is the in-process pipeline. The launch path is `capture_ambient.go` → `daemon.Start` → `ctxd`. The word "runner" already means `ambient.Runner`, so `daemon.runner` needs care in docs and package names.
2. ctxd's sources read the user's browser profile, clipboard and drop folders. ADR-066 puts ctxd on the user's machine for that reason. On a headless pod only `filewatch` is plausible, and only for folders that something on the pod fills. Nothing in pod today moves files onto a pod from a verb.
3. The "port forwarding for enqueue endpoints" need exists only when ctxd runs locally and dpkms runs on the pod. A ctxd on a pod needs a forward for its control API only.
4. "pod present but no provider configured" has no single signal. `mock` and `ssh` always register, so `pod adapter list` is never empty. The failures are: `pod show <name>` not found (exit 3), the pod's adapter not registered (credentials or binary missing), or `pod create` without `--provider` (exit 1). Because v1 adopts a named pod and never calls `create`, the first two matter.
5. `pod stop` and `pod start` are not uniform across adapters, and `pod show` status is a local record. "Running" does not prove the remote is up.
6. The pod verbs the integration needs most, `exec` and `forward`, do not exist in the reviewed tree.
7. `dpkms serve --daemon` hides startup output and does not wait. The remote launch cannot use it.
8. A dpkms-on-pod flow of create, exec, forward, then stop and destroy is the obvious extension. v1 does not do it: ctxt adopts an existing pod and never creates or destroys one. Reasons: `create` and `destroy` bill, need a provider choice that pod has no default for, and `destroy` is gated by notes and confirmation. Pod lifecycle stays with the user.

## 3. Detection

Rules:

- **D1.** Detection is `exec.LookPath("pod")` and nothing else. PATH only. ctxt does not look beside its own executable (`FindCtxd` does that for ctxd; pod does not ship with ctxt). A result that needs a relative PATH entry (`exec.ErrDot`) counts as absent.
- **D2.** Detection runs only when the resolved runner is `pod`. With `local`, ctxt never calls `LookPath("pod")`, never runs pod, prints nothing about pod and reads no pod state. This is what "invisible when absent" and "detection alone changes nothing" mean in code.
- **D3.** When the runner is `pod`, there are four outcomes:

| Outcome | Result |
|---|---|
| `pod` not found | Prerequisite error, exit 70. Message: `daemon.runner is "pod" but pod is not on PATH`. Fix: `install pod, or set daemon.runner: local`. |
| Found, probe fails (does not start, times out, bad JSON, wrong `tool`, `schema_version` major not 1) | Prerequisite error, exit 70, with the tail of pod's stderr. |
| Found, probe passes, a leaf ctxt needs is missing | Prerequisite error, exit 70, naming the leaf: `the installed pod has no "pod exec"`. |
| Found, probe passes | Continue. |

- **D4.** The probe is `pod capabilities --format json`, run once per ctxt command, 10 s deadline. It changes no pod and contacts no provider, though like every pod run it opens pod's state DB and seeds `policies.yaml` (section 2.3). ctxt checks `leaves[].path` for the verbs the action needs: `pod show`, `pod start`, `pod stop`, plus `pod exec` and `pod forward` for anything that touches the remote. The reviewed pod reports version `dev`, so a semver gate is impossible today. Leaf presence is the gate. The document lists leaf-local flags only (`--note` on `start` and `stop`, for example). Global flags such as `--format` are not listed per leaf [pod-bin], so the drift test (section 10, L5) covers them.
- **D5.** No pod types are shared. pod's packages are all `internal/`, so ctxt could not import them anyway. The only contract is argv in, exit code and JSON out. `go.mod` gains nothing: `os/exec` and `encoding/json` are enough, and `hop.top/xrr` is already a dependency used only by tests.
- **D6.** ctxt prints no hint such as "pod found, try --runner pod". A hint is a behavior change driven by detection.

## 4. Config surface

ctxt and dpkms read one schema, each from its own file: `ctxt.yaml` for `ctxt` (and for `ctxd`, which loads the `ctxt` config) and `dpkms.yaml` for `dpkms` (`config/config.example.yaml`, `CascadeSlots`). A binary ignores keys its commands never use. The `daemon.*` keys are defined once. The table says which binary acts on each key, so a user who wants both daemons on a pod sets the keys in both files.

| Key | Type | Default | Read by | Meaning |
|---|---|---|---|---|
| `daemon.runner` | `local` \| `pod` | `local` | ctxt, dpkms | Where the launched daemon runs. |
| `daemon.pod.name` | string | empty | ctxt, dpkms | Existing pod to use. Required when the runner is `pod`. |
| `daemon.pod.autostart` | bool | `true` | ctxt, dpkms | Run `pod start` when `pod show` reports `stopped` (section 5, pod gate). |
| `daemon.pod.timeouts.probe`, `.query`, `.exec`, `.lifecycle` | duration | `10s`, `15s`, `90s`, `5m` | ctxt, dpkms | Deadlines per call class (section 5). |
| `daemon.pod.ctxd_bin` | string | `ctxd` | ctxt only | Command name of ctxd on the pod's PATH. Charset-checked. |
| `daemon.pod.server_url` | string | empty | ctxt only | The dpkms URL as the pod sees it, written into the pod-side config as ctxd's endpoint. Exempt from F18, because it is resolved on the pod, not on the laptop. For topology C: `http://127.0.0.1:<dpkms_port>`. Empty: ctxd gets the endpoint resolved on the laptop, and F18 applies. No token is sent with it; a private dpkms on the same pod needs none. |
| `daemon.pod.dpkms_bin` | string | `dpkms` | dpkms only | Command name of dpkms on the pod's PATH. Charset-checked. |
| `daemon.pod.data_dir` | string | empty | dpkms only | dpkms data dir on the pod. Required. No default, because which path survives a stop differs by adapter. |
| `daemon.pod.dpkms_port` | int | `8080` | dpkms only | Fixed HTTP port of the pod-side dpkms. |
| `daemon.pod.local_port` | int | `0` | dpkms only | Local end of the persistent forward. `0` picks a free port each launch, which changes the printed `server.urls` entry every time. Set a fixed port when that entry is saved. |

Flags:

- `--runner local|pod` overrides `daemon.runner`. It exists on `ctxt capture --ambient` (start and every verb) and on `dpkms serve`, `dpkms ps` and `dpkms shutdown`.
- `--pod <name>` overrides `daemon.pod.name`, on the same commands.
- `--stop-pod` on `ctxt capture --ambient stop` and on `dpkms shutdown`: also run `pod stop` after the daemon has stopped. Off by default, because the pod may carry other work.
- `--stop-on-exit` on attached `dpkms serve --runner pod`: Ctrl-C runs the stop sequence. Off by default (section 8.4).

Rules:

- **C1. Precedence.** Flag, then `-c` token, then user config, then system config, then default. Environment follows the existing config layering. Detection is not a layer.
- **C2. The project layer may not set `daemon.*`: refuse, as registries do.** A cloned repository can carry `.contexthelp/<bin>.yaml`. `daemon.pod.ctxd_bin` runs a command on the user's pod, and `daemon.runner: pod` starts a billable pod. Neither should come from a repo. The registry precedent is confirm-or-refuse, not ignore: `RegistriesSource` finds which layer sets a key, and `confirmLoginOrigin` (`cmd/ctxt/cmd/registry.go`) asks for a typed yes or refuses without a TTY. `RegistriesSource` returns only the highest layer that sets a key, so it cannot report a project file that a higher layer shadows. For `daemon.*`, a new helper beside it walks the same `layerSources` list, so it sees exactly the files the loader merges (including the `CTXT_CONFIG` case, where the project slot is dropped). It opens the `LayerProject` file with `OpenLayer` and reports `Has("daemon")`. A command that reads `daemon.*` then fails with a usage error, exit 2, naming the file, with the fix "move daemon.* to your user config, or pass --runner and --pod". Commands that never read `daemon.*` are unaffected. The alternative, ignoring the keys, needs the loader to re-merge without the project layer. `LoadWithOverrides` has no option for that: a single config file or `CTXT_CONFIG` replaces slots wholesale, and nothing drops only the project slot.
- **C3. Pod-side processes are never launchers.** `ctxd` has no `--runner` and never reads `daemon.*`. The pod-side `dpkms serve` is the same binary as the launcher and reads `dpkms.yaml` on the pod, which the launcher's argv does not control (synced dotfiles can carry `daemon.runner: pod`). So the remote `dpkms serve` command always ends with `--runner local`; a flag beats config under C1. The launcher also sends no `daemon.*` token.
- **C4. Validation.** Unknown runner value: usage error, exit 2, listing the allowed values. `runner=pod` with no pod name: usage error naming `daemon.pod.name` and `--pod`. `daemon.pod.*` set while the runner is `local`: ignored without a message, so a user can toggle the runner with one key.
- **C5. The flag and keys are static.** They exist whether or not pod is installed. A flag that appears only when `pod` is on PATH would make `--help`, the capabilities document and docs checks depend on the machine.
- **C6.** Files a later change touches: a new `internal/config/daemon.go` in the style of `ambient.go`, its validation, `config/config.example.yaml`, `docs/ambient.md`, `docs/ctxt/configuration.md`.

## 5. Process boundary

`internal/podrun` is the only code that starts `pod`. It exposes one seam:

```go
type Runner interface {
    Run(ctx context.Context, argv []string, o RunOpts) (Result, error)   // waits; Result has Stdout, Stderr, ExitCode
    Start(ctx context.Context, argv []string, o RunOpts) (Proc, error)   // long-lived child, for forward
}
```

Rules for every call:

- **P1.** argv is a slice. No local shell. `argv[0]` is the `LookPath` result.
- **P2.** stdin is `/dev/null`. Nothing can prompt.
- **P3.** Environment is the parent environment plus `NO_COLOR=1`. pod finds its adapters through that environment (`E2B_API_KEY`, cloud credentials), so it must not be scrubbed. ctxt adds no secrets of its own. ctxt does not override the XDG dirs, because pod's state (its registry of pods) belongs to the user.
- **P4.** Document verbs get `--format json --no-hints --no-color`. `exec` and `forward` output is the remote's, so they do not.
- **P5.** Each call has a deadline from `daemon.pod.timeouts`: probe and `show` use `probe` and `query`, `start` and `stop` use `lifecycle`, `exec` uses `exec`. On unix the child runs in its own process group. On deadline the helper sends SIGTERM to the group (`cmd.Cancel`) and starts a 5 s timer. When the timer fires, the helper itself sends SIGKILL to the group. It cannot leave that to `cmd.WaitDelay`: after the delay, Go's `Process.Kill` signals only the lead process, and adapters spawn `sky`, `gcloud`, `ssh` and others that would survive. `WaitDelay` is still set, to 6 s, so pipe readers are released. Windows is different: there is no SIGTERM and no group signal, and `launch_windows.go` sets only creation flags (`DETACHED_PROCESS`, `CREATE_NEW_PROCESS_GROUP`), no job object. Killing a process tree there needs a job object with kill-on-close. v1 does not support `runner=pod` on Windows (exit 70, "not supported on windows yet") unless that helper is built (open question 18).
- **P6.** Stdout is capped at 1 MiB and stderr at 64 KiB. Excess is dropped.
- **P7.** Result handling: exit code first. On success, parse stdout as JSON. On failure, decode the first JSON object on stderr as pod's envelope if there is one. Many failures have none (section 2.3), so ctxt then takes the last lines of stderr as the message and classes the failure by exit code alone.
- **P8.** No secret in argv. A remote command line is visible in the pod's process list and may reach audit logs. Secrets travel in a file the pod user owns (open question 4).
- **P9.** Remote command strings come from fixed templates in one function. Every interpolated value must match `^[A-Za-z0-9._/:@=+-]+$` or the call is refused before it starts. pod itself records that writing commands through `Exec` is quoting-fragile.
- **P10.** A remote launch redirects all three file descriptors, so the exec call can return. UNVERIFIED on adapters such as Runloop that wait on background children.

Error mapping. pod's own `suggested_fix` is always kept; ctxt adds to it, never replaces it.

| pod result | ctxt result |
|---|---|
| Cannot start the process | Prerequisite, exit 70 |
| Deadline exceeded, or the pod is unreachable | Transient, exit 6, like the local stop's `TransientError`: `pod <verb> did not finish within <d>; the pod may be half-changed, check "pod show <name>"` |
| Exit 2 (unknown verb or flag) | Prerequisite, exit 70: pod surface does not match what ctxt expects |
| Exit 3 | Not found, exit 3, pod's message and pod's own fix (`pod ps` for a missing pod, `pod adapter list` for a missing adapter) |
| Exit 4 | Conflict, exit 4, pod's message unchanged (policy denial or conflict) |
| Exit 5 | Unauthorized, exit 5, pod's message |
| Exit 6 | Transient, exit 6, pod's message |
| Exit 70 | Prerequisite, exit 70, pod's message |
| Exit 130 | pod was cancelled. ctxt follows its own cancel path. |
| Exit 1, or any code not listed | Generic, exit 1. With an envelope: pod's `message` and `suggested_fix`. Without one: `pod <verb> failed: <last stderr line>` plus the stderr tail. |

**Pod gate.** Every action that touches the remote runs `pod show <name> --format json` first and acts on the status. This matters because `pod start` on a `destroyed` or `provisioning` row would rewrite it to `running` with no provider call (section 2.3), and `show` would then report a pod that does not exist.

| `pod show` result | Start paths (`start`, `dpkms serve`) | Other paths (`status`, `stop`, `ps`, `shutdown`) |
|---|---|---|
| Exit 3 | Exit 3. Delete the local record for this pod. Never create. | Same. |
| `destroyed` | Treated as not found: exit 3, "pod <name> was destroyed". Delete the local record. Never `pod start`. | Report "not running", delete the local record, exit 0. |
| `provisioning` | Transient, exit 6: "pod <name> is still provisioning". No `pod start`. | Same error. |
| `stopped` | `pod start` when `daemon.pod.autostart`. Otherwise a usage error: "pod <name> is stopped; run `pod start <name>` or set daemon.pod.autostart". | Report "not running (pod is stopped)", exit 0. A stopped pod has no daemon. |
| `running` | Continue. The row is a local record, so F8 still applies. | Continue. |
| Any other value | Generic, exit 1, naming the value. | Same. |

## 6. Failure modes

| # | Condition | ctxt result | Never |
|---|---|---|---|
| F1 | `runner=pod`, `pod` absent | Exit 70 (D3) | Fall back to local |
| F2 | `runner=local`, `pod` absent or present | No pod code runs, no output | Look for pod, or print about it |
| F3 | Probe fails, times out or returns bad JSON | Exit 70 with stderr tail | Wait past 10 s |
| F4 | Needed leaf missing (today: `exec`, `forward`) | Exit 70 naming the leaf | Try the verb anyway |
| F5 | Named pod not found, or `destroyed` | Exit 3. Not found: pod's own fix. `destroyed` comes from a successful `show` with no fix attached, so ctxt gives its own: "pod <name> was destroyed; create a pod with `pod create` and set daemon.pod.name". The local record is deleted (pod gate). | Create one, or run `pod start` |
| F6 | Pod's adapter not registered (credentials or binary missing) | pod's error shown, usually exit 1 or 3 | Prompt (stdin is closed, pod also skips prompts without a TTY) |
| F7 | `pod start` fails or times out | pod's message (failure), or exit 6 (deadline). Both add "pod status is a local record". | Wait without a bound |
| F8 | Status says running, remote is dead (exec or forward fails) | Exit 6: `pod <name> is not reachable: <msg>`; fix: `pod stop` / `pod start`, or inspect the provider | Heal by itself |
| F9 | `ctxd` or `dpkms` not on the pod's PATH (exec exit 127) | Exit 70: `ctxd not found on pod <name>`; fix: put it in the pod image | Upload a binary |
| F10 | A daemon is already running on the pod | Exit 4, same message and fix as the local `RunningError` | Start a second one |
| F11 | The daemon exits during start | Error with the tail of the pod-side log, like `StartError` | Report success |
| F12 | Readiness deadline | Exit 6 naming the pod-side log path | Poll forever |
| F13 | Forward cannot bind the local port | Retry with a new port up to 3 times, then exit 1 | Bind a non-loopback address |
| F14 | ctxt interrupted mid-start | Context cancel kills the child group. The daemon may be running on the pod. The next start sees it (F10) and adopts it. | Leave a forward process behind |
| F15 | pod denies the action by policy | Exit 4, pod's text (no envelope: `Error: ...`) | Retry with other flags |
| F16 | Pod is `provisioning` | Exit 6 (pod gate) | `pod start` it |
| F17 | Pod is `stopped` and `autostart` is off | Usage error, exit 2, with the `pod start` command | Start it anyway |
| F18 | ctxd would run on the pod, `daemon.pod.server_url` is empty, and the endpoint resolved on the laptop is a loopback or local-instance URL | Usage error, exit 2: the pod cannot reach `127.0.0.1` on the laptop; fix: set `daemon.pod.server_url` to the URL the pod sees | Send an unreachable URL to the pod |
| F19 | `runner=pod` on Windows | Prerequisite, exit 70 (P5) | Run without a kill path |

Why nothing hangs: a deadline on every call (P5), closed stdin (P2), group kill (P5), bounded readiness loops (section 7), a remote launch with detached file descriptors (P10), and the control client's existing one-minute call timeout (`control.go`).

## 7. Lifecycle mapping: ctxd

### 7.1 Where ctxd can run

| Topology | ctxd | dpkms | Forward needed | Fit |
|---|---|---|---|---|
| L (today) | local | local or remote URL | none | default |
| A | pod | remote URL the pod can reach | control API only | `filewatch` over pod-side folders. Other sources are `unavailable` or fail the plan with their reason. |
| B | local | pod | persistent, to the dpkms port | The main payoff. This is the "enqueue endpoint" forward. See section 8. |
| C | pod | same pod, via `127.0.0.1` | control API only | A headless node. Same source limits as A. Needs `daemon.pod.server_url`, since the laptop's URL for that dpkms is its forward, which the pod cannot use. |

pod's forward is local-to-remote (`ssh -L`). A pod cannot reach a dpkms on the user's laptop without a reverse tunnel, which pod does not offer [pod-src]. Topology A therefore needs a dpkms with a URL the pod can reach.

### 7.2 Verbs

| Verb | Local today | With `runner=pod` |
|---|---|---|
| start | `FindCtxd`, `Start`, wait for lock and status | Section 7.3 |
| status, sources, tail, buffer flush/purge | `ReadRun` → `NewClient` → call | Read the local record (7.4), open a forward, call through the same `Client`, close the forward |
| stop | `Client.Stop`; when the API does not answer, signal the pid; poll the lock, up to 30 s (`capture_ambient_manage.go`) | `Client.Stop` through a forward. When the API does not answer, `pod exec ... kill -TERM <pid>` with the pid from the remote run file, after `ps -p <pid> -o comm=` shows ctxd. Poll until the API refuses and the run file is gone (30 s), else transient error, exit 6. Delete the record. With `--stop-pod`, then `pod stop <name> --note ...` |
| start `--foreground` | ctxd attached to the terminal | Usage error in v1. The internal `Connector.Exec` buffers output and cannot stream a log. Whether a future `pod exec` streams is UNVERIFIED. |

### 7.3 Start sequence

| Step | Action | pod call |
|---|---|---|
| 1 | Resolve runner, pod name and the dpkms endpoint. With `daemon.pod.server_url` set, that URL is ctxd's endpoint, taken as given. Otherwise the endpoint is resolved locally (`dpkmsclient.Resolve`) and a loopback or local-instance URL is refused (F18). Apply every `-c` token, key=value or file, to ctxt's own config. No `-c` token, `--server` or `--instance` reaches the pod. Only `--input`, `--skip` and `--profile` are passed on as flags, P9-checked. | none |
| 2 | Detect and probe (section 3). | `pod capabilities --format json` |
| 3 | Pod gate (section 5): `show`, then exit 3 for missing or `destroyed`, exit 6 for `provisioning`. | `pod show <name> --format json` |
| 4 | Only when the status is `stopped` and `autostart` is on, start the pod. Print the pod name and provider first, since this can bill. | `pod start <name> --note "ctxt ambient daemon" --format json` |
| 5 | Check the binary on the pod. A build differing from the local ctxd is a warning, not an error. | `pod exec <name> -- ctxd --version` |
| 6 | Look for a running instance: read the remote run file, open a forward, call `/v1/status`. If it answers, fail with the local `RunningError` text (F10). This must come before the launch, because a backgrounded launch hides ctxd's own exit 4. | `pod exec <name> -- cat <run file>`, `pod forward ...` |
| 7 | Render the pod-side `ctxt.yaml` from ctxt's resolved config and put it on the pod as a 0600 file (open question 4). It holds the keys ctxd reads (the server endpoint and token, `ambient`, `capture`, `profile`; the exact set is confirmed when the renderer is written). It never holds `daemon.*` or a laptop path. ctxd starts with `-c <that path>`, P9-checked. The pod's own config files stay as lower layers. | blocked on a pod upload or stdin path |
| 8 | Launch detached (script below). | `pod exec <name> -- sh -c '<script>'` |
| 9 | Poll every 2 s, one exec that prints the launch marker and the remote run file, until the run file holds a control address and a `started_at` not older than the marker. | `pod exec ... cat` |
| 10 | Open a forward to the control port, wait for the local TCP accept (15 s), poll `/v1/status` until 200. Overall deadline 90 s (local `Start` uses 20 s). | `pod forward <name> <lp>:<cp>` |
| 11 | On failure at 9 or 10, fetch the log tail and return it as the error. | `pod exec ... tail -c 16384 <log>` |
| 12 | Write the local record, close the forward, print the same summary as `renderAmbientStarted` plus the pod name. | none |

Launch script (template; `<...>` values pass the P9 check):

```sh
export XDG_STATE_HOME="${XDG_STATE_HOME:-$HOME/.local/state}"
S="$XDG_STATE_HOME/ctxt/ambient"
mkdir -p "$S" && date +%s > "$S/ctxd.launch" &&
setsid nohup <ctxd_bin> --launcher "pod <name>" --log-file "$S/ctxd.log" <flags> \
  </dev/null >>"$S/ctxd.out" 2>&1 &
```

The script exports `XDG_STATE_HOME` for ctxd instead of recomputing ctxd's default path. ctxd resolves its state dir with the XDG library, which honours that variable on every OS and otherwise uses an OS-native path (under `~/Library/Application Support` on macOS). With the export, `$S` is the directory ctxd really uses. ctxd already accepts `--launcher` and `--log-file`, and `signal.NotifyContext` handles SIGTERM, so no ctxd change is needed.

### 7.4 Run file, lock and local record

- The authority is the pod-side `ctxd.lock` and `ctxd.run`, in the pod user's state dir. An `flock` does not cross hosts, so the laptop never takes or probes it. Liveness from the laptop means "the control API answers through a forward".
- A pod that restarts leaves a stale run file and a free lock. A new ctxd takes the lock and rewrites the file, exactly as on a laptop reboot (`runfile.go`). ctxt never trusts the file alone. The pre-launch check (step 6) counts a run file only if its control API answers. After its own launch (step 9), ctxt requires a `started_at` not older than the launch marker. Both timestamps come from the pod's clock. A stale file is therefore never taken for a live daemon.
- The laptop keeps `ctxd.pod.json` in its ambient state dir: mode 0600, atomic write like `writeRunFile`, fields `version`, `pod`, `provider`, `remote_state_dir`, `control_port`, `token`, `started_at`. It is a cache. If a forward or a call fails, ctxt re-reads the remote run file once and rewrites the record. It is deleted by `stop`, and by any action whose pod gate finds the pod missing or `destroyed`.
- The seam for everything downstream is `RunInfo`. In pod mode the resolver returns a `RunInfo` whose `Control` is the local end of the forward and whose `Token` is the cached token. `Client`, `Status` rendering and the verbs then need no change. `RunInfo` gains an optional `Pod` field; `Launcher` text says `pod <name>`.
- Each verb opens its own forward and closes it on return. Nothing outlives the ctxt process, so there is no forward pid to track. `tail` holds its forward as long as it streams.
- The token is the same secret the local 0600 run file holds. It is stored with the same exposure, but it travels further than the local file's. It crosses the forward, and it also crosses the `pod exec` channel whenever ctxt reads the remote run file (steps 6 and 9, and the refresh above). On adapters whose exec goes through the provider (Runloop's API, the Modal and Sprites SDKs, E2B's SDK or HTTP client [pod-src]), the token is visible to that provider path. Open question 4 covers a way to avoid reading it back.
- A pod-side ctxd and a local ctxd may run together. Their state dirs differ, so the lock does not conflict. The "one ctxd per user" sentence in `docs/ambient.md` becomes "one per state dir".

### 7.5 Pod stop, restart and loss

- pod runs no daemon and ctxd does not restart itself, so a crashed or stopped ctxd stays down until the next `ctxt capture --ambient`. `status` says so. A restart loop in a pod-side tmux session is possible later (open question 7).
- A process is expected not to survive `pod stop` on most adapters. That is UNVERIFIED per adapter. What the adapter docs say: Runloop snapshots disk only, Modal cannot stop, Sprites suspend themselves when idle [pod-src].
- ctxd's buffer is on the pod disk. A pod that wipes its container disk (Runpod outside `/workspace`) loses buffered events. Point the `buffer:` `path` of the ambient policy (`policy/ambient.yaml`) at a persistent path for those pods. ctxd cannot select the S3 buffer yet.

## 8. Lifecycle mapping: dpkms

The launcher is the local `dpkms` binary. ADR-077 makes ctxt a pure API client and an instance a URL plus a token, so process management stays in the `dpkms` operator CLI, and the result is an ordinary `server.urls` entry. The pod-side `dpkms serve` is unchanged and unaware of pod (and always runs with `--runner local`, C3).

Three commands take `--runner pod --pod <name>`:

| Command | Local meaning | With `runner=pod` |
|---|---|---|
| `dpkms serve` | Start in the foreground, or detached with `--daemon` | Start sequence below |
| `dpkms ps` | Scan this machine's pidfiles | Status mapping below |
| `dpkms shutdown` | SIGTERM to the pid in the pidfile | Stop sequence below |

### 8.1 Start sequence

| Step | Action |
|---|---|
| 1 | Resolve runner and pod. Apply `-c` tokens to dpkms's own config. Nothing from `-c` is sent to the pod (the launch flags are the fixed set below). |
| 2 to 4 | As ctxd 7.3 steps 2 to 4: detect, probe, pod gate, `pod start` only for a `stopped` pod. |
| 5 | Binary check: `pod exec ... dpkms --version`. |
| 6 | Look for an instance: `pod exec ... dpkms ps --format json --data-dir <data_dir>`. One on the chosen port is exit 4, with the local conflict message. |
| 7 | Render a small pod-side `dpkms.yaml` (`server.access`, `server.allowed_hosts`, and `server.auth` for `protected`) and put it on the pod (open question 4). |
| 8 | Launch in the foreground form under `setsid nohup ... >>log 2>&1 &`, not `--daemon`: `dpkms serve --name <n> --port <dpkms_port> --grpc-port 0 --cookie-bridge-port 0 --data-dir <data_dir> -c <rendered path> --runner local`, plus `--profile` when the user set one. The cookie bridge serves a local browser extension and is useless on a pod. An explicit `--port` binds exactly or fails, so a taken port is an error and not a surprise. |
| 9 | Open a forward `<lp>:<dpkms_port>` and poll `GET /healthz` until 200, 90 s deadline. On failure return the log tail. |
| 10 | Write the local record (below), print the URL for `server.url` or a `server.urls` entry. ctxt does not edit the user's config. |

The local record is a 0600 file under the laptop's state dir, not in the pidfile dir that `dpkms ps` scans. Fields: `pod`, `provider`, `dpkms_port`, `local_port`, `forward_pid` with the process start time (so a reused pid is never signalled), `data_dir`, `started_at`.

### 8.2 Status mapping (`dpkms ps --runner pod --pod <name>`)

| Observed | Reported |
|---|---|
| Pod gate: missing, `provisioning` | The gate's error (exit 3, exit 6) |
| Pod `destroyed` or `stopped` | "No running dpkms instances (pod <name> is destroyed / is stopped)", exit 0. Same envelope as an empty local scan: `{"instances": []}`. |
| Pod `running`, `pod exec ... dpkms ps --format json --data-dir <data_dir>` lists instances | The pidfile rows (`name`, `pid`, `port`, `grpc_port`, `started_at`), each with `host: "pod <name>"`. The table and JSON keep the shape of a local `ps`. |
| Listed, and a forward is up or can be opened | Add `health` from `/healthz` (`healthy`, `degraded`, or `unreachable`). Without a forward the field is `unknown`. |
| Lists nothing | "No running dpkms instances on pod <name>.", exit 0. If the local record says one should run, add "last launched <t>". |
| The exec fails | F8, exit 6 |

### 8.3 Stop sequence (`dpkms shutdown --runner pod --pod <name>`)

1. Pod gate (other paths). A `stopped` or `destroyed` pod means nothing runs: print it, delete the record, exit 0.
2. `pod exec ... dpkms shutdown --port <dpkms_port> --data-dir <data_dir>`. The port is explicit, because `shutdown` otherwise uses the config's `server.port`. `shutdown` finds the instance through the pidfile in the data dir, so it needs the same `--data-dir`. It returns once SIGTERM is sent, not once the instance has exited.
3. Poll up to 30 s until `dpkms ps --format json --data-dir <data_dir>` over exec no longer lists the instance (and, when a forward is up, until `/healthz` refuses).
4. If step 2 failed (no SIGTERM was sent), `pod exec ... kill -TERM <pid>` with the pid from the `ps` JSON, then poll for 30 s. If the instance is still listed after a SIGTERM, from step 2 or this step, escalate once with `kill -KILL <pid>` and poll again. Still running: transient error, exit 6. A SIGKILL skips the graceful drain, so the error says so.
5. Kill the recorded forward helper after checking its start time, and delete the record.
6. With `--stop-pod`, then `pod stop <name> --note ...`. A `pod stop` without steps 2 to 4 can cut off a WAL checkpoint.

### 8.4 Attached mode and Ctrl-C

`dpkms serve --runner pod` is attached by default. The launcher process owns the forward and stays in the foreground. Ctrl-C (SIGINT), or SIGTERM to the launcher, **closes the forward and leaves the pod-side dpkms running**, then prints `dpkms shutdown --runner pod --pod <name>` as the way to stop it. `--stop-on-exit` makes the interrupt run the stop sequence instead. The reason: the pod-side dpkms is a detached service (`setsid nohup`) that outlives its launcher by design, and a dropped terminal or an accidental Ctrl-C must not shut a data service down. This differs from a foreground local `dpkms serve`, where Ctrl-C stops the server, and the exit message says so. If the forward dies while attached, the launcher exits with exit 6 and the pod-side dpkms keeps running. With `--daemon`, a detached `pod forward` child is started instead, its pid goes in the local record, and the launcher exits after printing the URL. The forwarder inside pod is an `ssh -N` process [pod-src]. Whether a future `pod forward` keeps that shape and recovers after a network drop is UNVERIFIED, so `dpkms ps` reports a dead forward and `dpkms serve` re-opens it.

### 8.5 Differences from ctxd

- **Forward lifetime.** Every ctxt command that uses this instance needs the forward, so ctxd's forward-per-verb does not fit (sections 8.3 and 8.4).
- **Host allowlist.** A private instance answers only `127.0.0.1:<bound port>` and `localhost:<bound port>` plus `server.allowed_hosts`, so a forwarded `Host: 127.0.0.1:<lp>` gets 403 `HOST_NOT_ALLOWED` unless `lp` is the pod-side port. `ParseAllowedHost` accepts a bare host, which matches that host on any port (`internal/config/config.go`, `validate.go`). Recommendation: render `server.allowed_hosts: ["127.0.0.1"]`. A forward port that changes (a retry after F13, a re-opened `--daemon` forward) then never causes a 403. The allowlist exists against DNS rebinding, where a hostile page's own name reaches the listener (`middleware_host.go`). An IP-literal `Host` is not such a name. `daemon.pod.local_port` fixes the port when the printed `server.urls` entry is saved. With `0`, the port and so the entry change on each launch.
- **Auth and transport.** `private` has no inbound auth and trusts the tunnel. Today the tunnel does not authenticate the pod: pod's SSH forwarder runs with `StrictHostKeyChecking=no` and `UserKnownHostsFile=/dev/null`, so a path attacker can pose as the pod, receive every capture and write, and return forged results (section 9). Modal's forward ends on a public, unauthenticated URL [pod-src]. `protected` binds `0.0.0.0` on the pod with auth, which on a pod with a public IP exposes the port, and there is no loopback-plus-auth mode. This is open question 5.
- **Persistence.** The data dir must sit on a path that survives the adapter's stop and destroy semantics. ctxt cannot know which path that is, so `daemon.pod.data_dir` is required. `dpkms backup` before any destroy stays the operator's job in v1, since ctxt never destroys.

## 9. Port forwarding and trust

- **Bind address.** The local end must be `127.0.0.1` only. Today that holds in three ways: docker's socat sets `bind=127.0.0.1` explicitly; the Modal and Sprites relays bind loopback by their docs; and pod's SSH forwarder passes no bind address (`-L <lp>:localhost:<rp>`), which is loopback only while `GatewayPorts` is off in the user's ssh config [pod-src]. The last is a default, not a guarantee.
- **Host keys.** pod's SSH paths run with host-key checking off (section 2.3). Nothing authenticates the pod end of the tunnel. An on-path attacker can pose as the pod and see everything that crosses the forward: the ctxd control token and captured content, or all dpkms traffic, including the data a private dpkms accepts without auth. ctxt cannot add this check around a verb it does not run. It has to come from pod: verify the host key (pin it when the pod is created or first connected, then `StrictHostKeyChecking=yes` against a pod-owned known-hosts file), and pass an explicit `127.0.0.1:<lp>` bind. Both are upstream asks and probe items (section 13, item 5). Until they exist, the dpkms and ctxd phases in section 12 are gated, and open question 5 depends on them.
- ctxt picks the local port by binding `127.0.0.1:0`, reading the port and closing it, then passes it to `pod forward`, unless `daemon.pod.local_port` is set. There is a race window, so F13 retries.
- A forward ctxt opens is a child in its own process group. ctxt closes it with the P5 sequence, on success, error and interrupt.
- Some adapters cannot carry a forward. `pod create --computer-use` refuses E2B (no forwarding), Daytona (no forwarding in its CLI) and Runpod (not yet measured) [pod-src, `internal/cli/create.go`]. `docs/adapters.md` still lists Daytona as supported, so pod's docs and code disagree. Whether a client can see this signal for a plain forward is UNVERIFIED.

## 10. Testing

Goal: hermetic. No real pod, no cloud, no network, no real HOME.

| Layer | What | How |
|---|---|---|
| L1 | Config parse, precedence, project-layer rule, validation, argv builders, remote-command templates and the P9 charset check, envelope parser, error table | Plain table tests. |
| L2 | Start, status, stop, the pod gate table and F1 to F19 as state machines | An in-process fake `podrun.Runner` scripted per argv, an injected clock, and an `httptest` server standing in for the forwarded control API or `/healthz` (OS-assigned port). `Start` returns a fake `Proc`. |
| L3 | The shell-outs themselves | xrr exec cassettes at the one `Runner.Run` seam, with the `hop.top/xrr` exec adapter already in `go.mod` and already used in `internal/adapter/mic`. Cassettes live in `internal/podrun/cassettes/`, one directory per test package. Covered: `capabilities`, `show` (running, stopped, missing), `start`, `stop`, the pod's error exits 1, 2, 3, 4, and the remote `cat`, `tail` and launch calls. |
| L4 | Black-box CLI paths | A fake `pod` shell script in a temp dir that holds the whole test PATH, driven through the cobra command. |
| L5 | Drift guard, opt-in | A `-tags=drift` test that runs the installed `pod capabilities --format json` and asserts the leaves and flags ctxt uses, skipping when `pod` is absent. Run outside the default suite. |

Cassette limits. A cassette replays recorded output for a fingerprint of argv and stdin (`hop.top/xrr/adapters/exec`; the environment is not part of it). It cannot replay PIDs, OS-assigned ports or timing, so those are L2 fakes. Replay returns an untyped `*xrr.RawResponse` (`session.go`), so the seam wrapper handles both response types. A recording stays green after pod drops a verb, because replay is faithful to the recording and not to pod. That is why L5 exists. Until a pod with `exec` and `forward` exists, any exec cassette is hand-written: mark those files synthetic and re-record them live as the exit gate for the pod work. A cassette that holds a run file must have the token scrubbed before commit.

Record mode. Default runs cannot enter it. It lives behind a build tag (`podlive`) and an explicit gate variable named `PODRUN_RECORD=1`. The name has no `CTXT_`, `CH_` or `DPKMS_` prefix on purpose: `testguard.Isolate` unsets every variable with those prefixes in `TestMain`, so a `CTXT_` gate would never be seen. The same isolation also moves HOME and the XDG dirs to a throwaway directory, which hides the user's pod registry and policies from a live `pod`. So in record mode only, the recorder names the real directories in `PODRUN_POD_HOME` and `PODRUN_POD_XDG_*`, and the harness hands them to the `pod` child through `RunOpts.Env`. Everything else in the test stays isolated.

Guarantee tests (the behavior this design promises):

1. **Detection alone does nothing.** PATH holds a `pod` script that writes a marker file when run. With `runner=local`, the ctxd and dpkms launch paths must not create the marker.
2. **Absent is an error only when asked.** PATH is an empty dir. `runner=pod` gives exit 70 and the F1 text. `runner=local` gives the same output as today.
3. **No silent fallback.** With `runner=pod` and no pod, no local ctxd and no local dpkms listener is started.
4. **No hang, no leftovers.** A fake `pod` that sleeps, one that ignores SIGTERM, one that waits on stdin, and one that spawns a child which outlives it: each ends inside its deadline plus the kill delay, and the child is gone (group SIGKILL, P5).
5. **Project layer refused.** `.contexthelp/ctxt.yaml` with `daemon.runner: pod` makes `ctxt capture --ambient` fail with exit 2 naming the file. The same for `.contexthelp/dpkms.yaml` and `dpkms serve`. A command that never reads `daemon.*` is unaffected.
6. **Idempotent start.** A running remote daemon produces exit 4 before any launch call.
7. **Pod gate.** A fake `pod show` reporting `destroyed` or `provisioning` never sees a `pod start`. `stopped` sees exactly one.
8. **Pod-side commands.** The remote `dpkms serve` command ends with `--runner local`. No `-c` token, file path or `daemon.*` value from the laptop appears in any remote argv.

Tests 1 to 3 must not run real processes or bind real ports (repo rule: tests never use real services). Today `startAmbient` calls `daemon.FindCtxd` and `daemon.Start` directly (`capture_ambient.go`), and with `runner=local` that path detaches a real ctxd, and `dpkms serve` binds listeners. Phase 1 therefore adds seams: the ctxd launch path takes injected `findCtxd` and `start` functions, and the runner decision in `dpkms serve` is a pure function that runs before `acquireListeners`. The tests call the decision, assert on the fakes (the local start was or was not called), and stop there. The marker-script test works because the script is a trap that nothing may run, not because anything real starts.

Every test that touches detection sets PATH explicitly with `t.Setenv`. `testguard` isolates HOME, XDG and the CTXT env but does not touch PATH, so a developer's real `pod` would otherwise leak into the run.

Isolation of each new test package: `internal/podrun` and the daemon pod host call `testguard.Main` from `TestMain`. They use `t.TempDir()`, scripted fakes, cassettes in replay mode and `httptest` on OS-assigned ports. They start no real pod, no cloud, no real `ctxd` or `dpkms`, and read no real HOME, config or keychain.

When the code lands, re-break each guarantee once (for example, run detection for `runner=local`, drop a deadline, add a local fallback) and confirm the matching test fails for the right reason.

## 11. Open questions

1. **Order.** ctxd on a pod is narrow (section 7.1, A and C). dpkms on a pod is the payoff. Keep ctxd first and share `internal/podrun`, or build dpkms first? Recommendation: build `internal/podrun` and config first, then dpkms, then ctxd for `filewatch` hosts.
2. **pod gaps.** The CLI needs `exec`, `forward`, a way to put a file on the pod, and a forward that verifies the pod's host key and binds `127.0.0.1` explicitly (section 9). The alternative, calling `ssh` directly with pod's config, breaks the "pod is ctxt-agnostic, shell-out only" boundary. Recommendation: raise the gaps upstream in hop-top/pod and wait.
3. **Adopt or own.** Should ctxt ever create and destroy pods (ephemeral, cost ceiling, orphan cleanup)? Recommendation: adopt only in v1. Ephemeral pods belong to the job-executor design.
4. **Secrets to and from the pod.** ctxd needs a config with the server token, and `Connector.Exec` takes no stdin. Options: an upload verb in pod, or a heredoc inside the command string, which exposes the secret in the process list for a moment. Recommendation: an upload verb. Without one, step 7 stays blocked. The token also comes back: ctxd generates its control token and ctxt reads it from the remote run file over `pod exec`, which on provider-API adapters crosses the provider (section 7.4). A small ctxd change would remove that: `--control-token-file`, so ctxt generates the token, uploads it with the config, and never reads it back. That conflicts with "no ctxd change" in 7.3, so it is a decision, not a default.
5. **dpkms access class and transport over a pod.** `private` has no auth and trusts the tunnel. Today pod's SSH tunnel does not authenticate the pod (host-key checking off, section 9), so an on-path attacker can pose as the pod and take every capture and write. `protected` does not fix that: it adds a token, sent over the same tunnel, and binds `0.0.0.0`. There is no loopback-plus-auth mode. Recommendation: dpkms and ctxd on a pod are unsupported until pod verifies host keys and binds `127.0.0.1` explicitly. After that, support `private` over providers whose forward is that SSH tunnel, and refuse `modal` (public tunnel). A `server.bind` key is a separate dpkms decision.
6. **Host allowlist.** Same port on both sides, or `server.allowed_hosts` set at launch? Recommendation: `["127.0.0.1"]`, which matches any port, plus a `daemon.pod.local_port` key for a stable printed URL (section 8.5). Not `127.0.0.1:<lp>`, which breaks on a port change.
7. **Supervision.** A pod-side restart loop under tmux, or none? Recommendation: none in v1.
8. **Forward owner and Ctrl-C for dpkms.** Attached by default, detached helper with `--daemon`. Ctrl-C closes the forward and leaves the pod-side dpkms running; `--stop-on-exit` stops it (section 8.4). Agree that interrupt does not stop a data service?
9. **`--foreground` with pod.** Unsupported in v1. Agree?
10. **Project layer.** Refuse `daemon.*` set in a project config, as registries refuse (C2). The alternative, ignoring it, needs a loader change to re-merge without the project layer. Agree?
11. **Hints.** None when pod is detected (D6). Agree?
12. **Cost.** `pod start` can bill. Recommendation: print pod and provider before it, never auto-stop on idle, `--stop-pod` opt-in.
13. **Drift test and the hermetic rule.** L5 runs a real `pod`. It reads no external data and runs under `testguard`, but it is a real binary. Recommendation: build-tagged, outside `make test`.
14. **Naming.** `daemon.runner` and `ambient.Runner` clash in speech. Recommendation: keep the key, name the code `podrun` and `PodHost`.
15. **ADR.** The config surface (section 4) and the boundary rules (sections 3 and 5) are stable enough to record as one ADR before code starts. It should include an amendment to ADR-066, which says ambient capture "must run locally on the user's machine". Topologies A and C put ctxd on a pod, so the amendment limits that to sources that read the user's machine, and allows `filewatch` over folders on the pod.
16. **Rendered pod config.** ctxt renders the pod-side `ctxt.yaml` or `dpkms.yaml` from its resolved config instead of forwarding `-c` tokens (7.3 step 7, 8.1 step 7). A `-c` file path would name a laptop file, and list values contain characters the P9 charset rejects. Confirm the key set each daemon needs.
17. **Stale rows.** `show` can report `running` for a pod deleted at the provider, and the only live check is an exec. Should the pod gate run a cheap `pod exec ... true` before launch, at the cost of one more round trip?
18. **Windows.** v1 refuses `runner=pod` on Windows because a process tree cannot be killed without a job-object helper (P5). Build that helper, or leave Windows out?

## 12. Effort and sequencing

Estimates are focused days for one engineer. They exclude the pod-side work that section 13 may uncover.

| Phase | Scope | Estimate | Depends on |
|---|---|---|---|
| 0 | Pod probe (section 13). Upstream asks: `exec`, `forward`, upload, and a forward that verifies host keys and binds `127.0.0.1` | S | none |
| 1 | `internal/podrun` (detect, probe, `Runner`, error map, pod gate, deadlines and group kill), `daemon.*` config with validation and the project-layer refusal, launch-path seams (section 10), L1, L2 and guarantee tests 1 to 5, 7 and 8 | 4 to 5 days | none. Fully testable with fakes. |
| 2 | dpkms pod host: `serve`, `ps` and `shutdown` with `--runner pod`, launch template, readiness, forward, rendered config, attached and detached modes, L3 cassettes | 5 to 6 days | Phase 0 results, pod `exec`, `forward`, upload, and the host-key and bind asks (section 9) |
| 3 | ctxd pod host: start sequence, local record, `RunInfo` seam, verbs, stop fallback, L3 cassettes, guarantee test 6 | 4 to 5 days | Phase 1, pod `exec` and `forward`, upload for secrets, and the host-key and bind asks |
| 4 | Docs (`docs/ambient.md`, configuration, release notes). Expected `Operator-Impact: none`: opt-in, no data or index change. | 1 day | Phases 2 and 3 |

Roughly 14 to 18 days of ctxt work once pod ships the verbs and the transport asks. Phase 1 can start now and delivers the guarantees (invisible when absent, no silent fallback, no hang) before any pod verb exists.

## 13. Checklist the pod probe must confirm

The probe must run against the pod build users will have, not only the reviewed tree. Each item needs evidence (command, output, adapter).

1. **Surface.** `pod capabilities --format json` lists `exec` and `forward`, or states they are absent. Note `--version`, `schema_version` and `--api-version` behavior.
2. **Exec.** Argument form (argv or one shell string), which shell, exit-code passthrough (including 127), stdout and stderr kept apart, behavior with stdin closed, no TTY, streaming or buffered output, per-adapter time limits.
3. **Detach.** Does `setsid nohup <cmd> </dev/null >log 2>&1 &` through exec survive the exec return and a client disconnect on docker, ssh, skypilot, sprites, runloop, exedev, e2b and modal? Does the exec return promptly, or wait on the child as Runloop is documented to?
4. **Stop and start.** Per adapter, are they real, advisory or errors? Does a process survive them? Does the data dir? Does `show` status ever reflect the remote? What happens when the pod was deleted at the provider? `pod start` on a `destroyed` or `provisioning` row rewrites it to `running` with no provider call (section 2.3). Will pod refuse that, and is a `destroyed` row ever reused?
5. **Forward.** Exact syntax. Foreground or detached. Bind address. Multiple mappings. Exit code and message on a taken local port. How to learn the port when asked for `0`. Reconnect after a drop. Which adapters can forward (Runpod is unmeasured, E2B cannot). Whether Modal needs `modal.unencrypted_ports` for a plain forward. Host keys: is the pod's key verified (pinned at create or first connect, `StrictHostKeyChecking=yes` against a pod-owned known-hosts file) or is checking off, as in the reviewed SSH paths? Bind: does the forward pass an explicit `127.0.0.1:<lp>` and fail if it cannot bind (`ExitOnForwardFailure`), or does it depend on the user's `GatewayPorts`?
6. **Leftover forwards.** Does `pod start` leave a forward process running for computer-use pods, and can it collide with ctxt's forward?
7. **File transfer.** Is there a verb, or stdin on `exec`, to put a 0600 file on the pod, and does it keep the mode?
8. **Errors.** Is the stderr JSON envelope stable under `--format json` for `exec` and `forward` failures? Exit codes for a remote command that fails versus pod failing?
9. **Non-interactive.** No prompt with stdin on `/dev/null` for every verb ctxt calls. Meaning of `--confirm` for `start` and `stop`.
10. **Side effects.** DB and `policies.yaml` creation on any run. Safe under concurrent invocations? Behavior with a read-only HOME.
11. **Policy.** Whether default policies require a note for `start` and `stop` (the changelog only names `destroy`, `--region` and prod tags).
12. **Latency.** Typical and worst-case duration of `show`, `start`, `stop`, `exec` and a forward handshake per adapter, to set the defaults in section 4.
13. **Platforms.** Which local OSes ship a `pod` binary? ctxt launches ctxd on Windows, but v1 refuses `runner=pod` there (P5).
14. **Sessions.** pod's design gives each pod a tmux session named `pod-<name>` that `pod ssh` attaches or creates (`internal/ssh/tmux.go`; the stories for persistent sessions are Planned). Does a process started inside such a session survive a client disconnect? Is the session reachable without a terminal, through `pod exec` or `pod ssh -- <cmd>`? How is it listed, attached read-only and killed? Is it a better home for a long-lived daemon than the detached launch in 7.3?
