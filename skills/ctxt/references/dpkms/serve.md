# `dpkms serve`

Start the `dpkms` background worker pool, expiry sweeper, and REST/gRPC/MCP API server (locally or on a configured pod runner). Required before any async capture, import, feed sync, expiry sweep, or reminder runs.

Alias: `dpkms start`.

## Execution Profile

- **Side effect**: `write` (opens/unlocks DB, binds ports, processes queued jobs)
- **Idempotency**: `no` (check `dpkms ps` / `dpkms healthcheck` before launching)

## Usage & Flags

```bash
dpkms serve [flags]
dpkms start [flags]
```

| Flag | Default | Purpose |
|---|---|---|
| `--name <slug>` | DB basename | Instance name (URI-safe slug, e.g. `work`) |
| `--port <n>` | `8080` | HTTP REST + `/mcp` + `/healthz` port (env: `CH_SERVER_PORT`) |
| `--grpc-port <n>` | `9090` | gRPC API port (env: `CH_GRPC_PORT`) |
| `--cookie-bridge-port <n>` | config | Loopback-only browser-extension cookie bridge port (`CH_COOKIE_BRIDGE_PORT`) |
| `--workers <n>` | `4` | Worker thread count |
| `--public` | `false` | Allow remote connections (loopback-only when false) |
| `--profile <name>` | `""` | Default focus profile |
| `--steps-path <dir>` | `""` | External pipeline steps directory |
| `--reminder-interval <dur>` | `1m` | Poll interval for due reminders |
| `--daemon` | `false` | Detach from terminal and run as background daemon |
| `--runner <kind>` | `local` | Daemon runner: `local` or `pod` (overrides `daemon.runner`) |
| `--pod <name>` | config | Existing pod name for `--runner pod` |
| `--stop-on-exit` | `false` | With `--runner pod`, shut down remote `dpkms` on interrupt |
| `--strict-permissions` | `false` | Refuse to start protected/public instance if DB/WAL/sidecars/blobs are group/world readable |
| `--strict-sharing` | `false` | Return HTTP 500 instead of stripping if visibility backstop detects out-of-scope data |
| `--dev` | `false` | Enable CORS for local Vite dev server (`http://localhost:5173`) |

## Agent Recipes

```bash
# Start a named background daemon locally
dpkms serve --name work --port 8081 --grpc-port 9091 --daemon
dpkms healthcheck --server-url http://localhost:8081

# Start a protected instance with strict permission & sharing guards
dpkms serve --strict-permissions --strict-sharing --daemon

# Launch via pod runner
dpkms serve --runner pod --pod dev-pod
```

## Gotchas & Next Steps

- When `storage.encryption.enabled: true`, `serve` unlocks the configured `KeyProvider` at startup and fails closed if keys are missing ([storage.md](storage.md)).
- Next: stop/restart/probe ([lifecycle.md](lifecycle.md)), monitor jobs ([job.md](job.md)), audit access posture ([access-governance.md](access-governance.md)).
