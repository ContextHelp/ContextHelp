# Pod-Backed Daemon Runner

`ctxt` and `dpkms` support running long-lived daemons on an existing remote or container pod by shelling out to the standalone `pod` CLI.

This capability is **strictly opt-in**. The default runner is `local`, and when `daemon.runner` is `local` (or unset):

- `pod` is never probed on `PATH`.
- No pod code runs and no hints or warnings about `pod` are printed.
- Existing local daemon behavior is unchanged.

When you explicitly opt in (`daemon.runner: pod` or `--runner pod`) and `pod` is not installed, commands fail with exit code `70` (`PREREQUISITE`) and never silently fall back to starting a local daemon.

## Prerequisites

- The `pod` CLI on `PATH`, supporting `capabilities`, `show`, `start`, `stop`, `exec`, `forward`, and `upload`.
- An existing pod created and managed through `pod` (the daemon runner adopts existing pods and never runs `pod create` or `pod destroy`).
- `ctxd` or `dpkms` installed inside the pod image on `PATH` (or named via `daemon.pod.ctxd_bin` / `daemon.pod.dpkms_bin`).
- macOS or Linux host (Windows returns exit code `70` in v1).

## Configuration

Configure `daemon.*` in your **user config** (`~/.config/contexthelp/ctxt.yaml` or `~/.config/contexthelp/dpkms.yaml`), or pass `--runner pod --pod <name>` on the CLI.

> **Security boundary:** Project-layer config files (`.contexthelp/ctxt.yaml`, `.contexthelp/dpkms.yaml`) are refused with exit code `2` if they set any `daemon.*` key.

```yaml
daemon:
  runner: pod # local (default) | pod
  pod:
    name: dev-pod
    autostart: true
    timeouts:
      probe: 10s
      query: 15s
      exec: 90s
      lifecycle: 5m
    # ctxt (ambient daemon) only:
    ctxd_bin: ctxd
    server_url: https://dpkms.example.net
    # dpkms only:
    dpkms_bin: dpkms
    data_dir: /workspace/contexthelp
    dpkms_port: 8080
    local_port: 0
```

Precedence: `--runner` / `--pod` flags > user config (`-c`, user file) > built-in default (`local`).

## Running `dpkms` on a Pod

Running `dpkms` on a pod keeps storage and pipeline processing on remote compute while exposing a local loopback URL (`http://127.0.0.1:<local_port>`) through `pod forward`:

```bash
# Start dpkms on the pod (attached mode: Ctrl-C closes the forward and leaves dpkms running)
dpkms serve --runner pod --pod dev-pod

# Start and detach the local forward helper
dpkms serve --runner pod --pod dev-pod --daemon

# Stop remote dpkms on Ctrl-C instead of leaving it running
dpkms serve --runner pod --pod dev-pod --stop-on-exit

# Inspect remote instances and /healthz status
dpkms ps --runner pod --pod dev-pod

# Gracefully shut down remote dpkms (and optionally stop the pod after exit)
dpkms shutdown --runner pod --pod dev-pod
dpkms shutdown --runner pod --pod dev-pod --stop-pod
```

Key behaviors:

- **Config rendering:** `dpkms` uploads a minimal `0600` pod-side config containing `server.access`, `server.allowed_hosts` (always including `127.0.0.1` so forwarded `Host: 127.0.0.1:<port>` requests pass host validation), and `server.auth` for non-private access. No `daemon.*` keys or local laptop paths are sent to the pod.
- **No recursive runner:** The pod-side launch command always passes `--runner local`, `--grpc-port 0`, and `--cookie-bridge-port 0`.
- **State isolation:** Local forward metadata is stored in `$XDG_STATE_HOME/ctxt/dpkms/dpkms.pod.<pod>.json` (`0600`), separate from `<data_dir>/run/` so local `dpkms ps` never confuses remote records with local pidfiles.
- **Persistence:** `daemon.pod.data_dir` is required and must point to storage that survives pod restarts on your provider.

## Running Ambient Capture (`ctxd`) on a Pod

When `ctxd` runs on a pod (for example watching pod-side directories with `filewatch`), `ctxt` manages it over short-lived per-verb forwards:

```bash
ctxt capture --ambient --runner pod --pod dev-pod
ctxt capture --ambient status --runner pod --pod dev-pod
ctxt capture --ambient tail --runner pod --pod dev-pod
ctxt capture --ambient buffer flush --runner pod --pod dev-pod
ctxt capture --ambient stop --runner pod --pod dev-pod [--stop-pod]
```

Key behaviors:

- **Endpoint reachability:** Pods cannot reach `127.0.0.1` on your laptop without a reverse tunnel. If your locally resolved dpkms endpoint is a loopback URL and `daemon.pod.server_url` is unset, `ctxt` refuses with exit code `2` and asks you to set `daemon.pod.server_url` to a URL reachable from inside the pod.
- **No `--foreground`:** `--foreground` is refused in pod mode; use `ctxt capture --ambient tail --runner pod --pod <name>` to stream events.
- **Local cache record:** `$XDG_STATE_HOME/ctxt/ambient/ctxd.pod.json` (`0600`) caches the pod control port and bearer token; the pod-side run file remains authoritative.
- **Coexistence:** One `ctxd` runs per state directory, so a pod-hosted `ctxd` and a local `ctxd` can run concurrently without lock conflicts.

## Pod Gate and Cost Guardrails

Before launching or querying a pod, `ctxt` and `dpkms` run `pod show <name> --format json`:

| Pod status | Start (`serve`, `capture --ambient`) | Query/Stop (`ps`, `status`, `stop`, `shutdown`) |
|---|---|---|
| Missing / `destroyed` | Exit `3` (deletes stale local record; never runs `pod start` or `pod create`) | Reports not running, deletes local record, exits `0` |
| `provisioning` | Transient error (exit `6`); never runs `pod start` | Transient error (exit `6`) |
| `stopped` | Runs `pod start` only if `daemon.pod.autostart` is `true` (printing pod name and provider first); otherwise exits `2` | Reports not running (pod is stopped), exits `0` |
| `running` | Proceeds to binary check, conflict check, launch, and readiness | Proceeds to remote command |

Stopping a daemon never stops the pod unless you pass `--stop-pod`.
