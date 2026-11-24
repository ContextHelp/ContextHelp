# `dpkms serve`

Start the `dpkms` background worker pool and REST/gRPC/MCP API server. Required before any async capture, import, feed sync, or reminder job can execute.

Alias: `dpkms start`.

## Execution Profile

- **Side effect**: `write` (opens DB, binds ports, processes queued jobs)
- **Idempotency**: `no` (starts a daemon process; check `dpkms ps` / `dpkms healthcheck` first)

## Usage

```bash
dpkms serve [flags]
dpkms start [flags]
```

## Key Flags

| Flag | Default | Purpose |
|---|---|---|
| `--name <slug>` | `""` | Instance name (URI-safe slug, e.g. `work`) for multi-instance targeting |
| `--port <n>` | `8080` | HTTP REST + `/mcp` + `/healthz` port |
| `--grpc-port <n>` | `9090` | gRPC API port |
| `--workers <n>` | `4` | Worker thread count (env: `DPKMS_WORKERS`) |
| `--public` | `false` | Bind for remote connections (loopback-only when false) |
| `--profile <name>` | `""` | Default focus profile |
| `--steps-path <dir>` | `""` | Directory for external pipeline steps |
| `--reminder-interval <dur>` | `1m` | Poll interval for due reminders |
| `--daemon` | `false` | Detach from terminal and run as background daemon |
| `--dev` | `false` | Enable CORS for local Vite dev server (`http://localhost:5173`) |

## Agent Recipes

```bash
# Start a named background daemon on custom ports
dpkms serve --name work --port 8081 --grpc-port 9091 --daemon

# Verify readiness immediately after launch
dpkms healthcheck --server-url http://localhost:8081

# Point ctxt at the named instance
ctxt instance use work && ctxt status --format json
```

## Gotchas & Next Steps

- If event-bus auth is enabled, set `DPKMS_BUS_TOKEN` or `BUS_TOKEN` in the environment.
- For persistent login-time startup on macOS/Linux, register the service with `dpkms install` ([lifecycle.md](lifecycle.md)).
- Next: stop/restart/probe ([lifecycle.md](lifecycle.md)), monitor jobs ([job.md](job.md)), or switch instance in `ctxt` ([../ctxt/instance.md](../ctxt/instance.md)).
