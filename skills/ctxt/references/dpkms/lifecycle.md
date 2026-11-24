# `dpkms` Lifecycle & Service Management (`shutdown`, `reboot`, `ps`, `healthcheck`, `install`)

Inspect, probe, stop, restart, and install `dpkms` instances as host OS services. To launch an instance manually, see [serve.md](serve.md).

## Execution Profile

| Command | Alias | Side effect | Idempotency |
|---|---|---|---|
| `dpkms ps` | — | `read` | `yes` |
| `dpkms healthcheck` | — | `read` | `yes` |
| `dpkms shutdown` | `stop` | `write` (stops daemon) | `yes` |
| `dpkms reboot` | — | `write` (graceful restart) | `yes` |
| `dpkms install` | — | `write-local` (writes launchd/systemd unit) | `yes` (with `--force`) |

## Subcommands & Flags

### `dpkms ps` — List running instances

```bash
dpkms ps [-o|--output json]
```

| Flag | Default | Purpose |
|---|---|---|
| `-o`, `--output <fmt>` | `""` | Output format (`json`) |

### `dpkms healthcheck` — Probe `/healthz`

```bash
dpkms healthcheck [--quiet] [--watch] [--interval <sec>]
```

| Flag | Default | Purpose |
|---|---|---|
| `--quiet` | `false` | Suppress output; rely on exit code (`0` = healthy) |
| `--watch` | `false` | Poll every `--interval` seconds until Ctrl-C |
| `--interval <sec>` | `2` | Refresh interval when `--watch` is set |

### `dpkms shutdown` (`stop`) & `dpkms reboot`

```bash
dpkms shutdown [--port <n>]
dpkms reboot   [--port <n>]
```

| Flag | Default | Purpose |
|---|---|---|
| `--port <n>` | `0` | Port of target instance (defaults to `--server-url` port) |

### `dpkms install` — Register OS service (launchd / systemd)

```bash
dpkms install [--launchd | --systemd] [--force]
dpkms install --status
dpkms install --uninstall
```

| Flag | Default | Purpose |
|---|---|---|
| `--launchd` | `false` | Install macOS LaunchAgent (`darwin` only) |
| `--systemd` | `false` | Install Linux systemd user service (`linux` only) |
| `--status` | `false` | Print current install and running state and exit |
| `--uninstall` | `false` | Stop the service and remove the unit file |
| `--force` | `false` | Overwrite existing unit file and reload service manager |

## Agent Recipes

```bash
# Discover running instances and probe health quietly
dpkms ps --output json
dpkms healthcheck --quiet && echo "healthy"

# Gracefully restart or stop a specific port
dpkms reboot --port 8080
dpkms shutdown --port 8081

# Check OS service status on macOS
dpkms install --status
```

## Next Steps

- Launch an instance manually: [serve.md](serve.md) · Check client view: [../ctxt/status.md](../ctxt/status.md)
