# `dpkms` Lifecycle & Service Management (`shutdown`, `reboot`, `ps`, `healthcheck`, `install`)

List, probe, stop, restart, and install `dpkms` instances as host OS services. To launch manually, see [serve.md](serve.md).

## Execution Profile

| Command | Alias | Side effect | Idempotency |
|---|---|---|---|
| `dpkms ps` / `healthcheck` | — | `read` | `yes` |
| `dpkms shutdown` / `reboot` | `stop` / — | `write` | `yes` |
| `dpkms install` | — | `write-local` | `yes` (with `--force`) |

## Subcommands & Flags

### `dpkms ps` — List running instances

```bash
dpkms ps [--runner <local|pod>] [--pod <name>]
```

### `dpkms healthcheck` — Probe `/healthz`

```bash
dpkms healthcheck [--quiet] [--watch] [--interval <sec>]
```

| Flag | Default | Purpose |
|---|---|---|
| `--quiet` | `false` | Suppress output; rely on exit code (`0` = healthy) |
| `--watch` | `false` | Refresh every `--interval` seconds until Ctrl-C |
| `--interval <sec>` | `2` | Refresh interval when `--watch` is set |

### `dpkms shutdown` (`stop`) & `dpkms reboot`

```bash
dpkms shutdown [--port <n>] [--runner <local|pod>] [--pod <name>] [--stop-pod]
dpkms reboot   [--port <n>]
```

| Flag | Subcommand(s) | Purpose |
|---|---|---|
| `--port <n>` | `shutdown`, `reboot` | Instance port (defaults to `server.port` / `CH_SERVER_PORT`) |
| `--runner` / `--pod` | `shutdown`, `ps` | Target `local` (default) or `pod` runner |
| `--stop-pod` | `shutdown` | With `--runner pod`, also stop the pod after `dpkms` exits |

### `dpkms install` — Register OS service (launchd / systemd)

```bash
dpkms install [--launchd | --systemd] [--force]
dpkms install --status
dpkms install --uninstall
```

| Flag | Purpose |
|---|---|
| `--launchd` / `--systemd` | Install macOS LaunchAgent / Linux systemd user unit |
| `--status` | Print install + running state and exit |
| `--uninstall` | Stop service and remove unit file |
| `--force` | Overwrite existing unit file and reload service manager |

## Agent Recipes

```bash
dpkms ps --format json
dpkms healthcheck --quiet && echo "healthy"
dpkms reboot --port 8080
dpkms shutdown --runner pod --pod dev-pod --stop-pod
dpkms install --status
```

## Next Steps

- Launch daemon: [serve.md](serve.md) · Client health view: [../ctxt/status.md](../ctxt/status.md)
