# `ctxt status`

Show reachability and health of the configured `dpkms` instance. Use global `--server <url>` or `--instance <name>` to target a specific instance.

- **Use `ctxt status`** as a quick client preflight.
- **Use `dpkms healthcheck --quiet`** for exit-code `/healthz` probes ([../dpkms/lifecycle.md](../dpkms/lifecycle.md)).

## Execution Profile

- **Side effect**: `read`
- **Idempotency**: `yes`

## Usage & Flags

```bash
ctxt status [--watch] [--interval <sec>]
```

| Flag | Default | Purpose |
|---|---|---|
| `--watch` | `false` | Refresh until interrupted (avoid in agent scripts) |
| `--interval <sec>` | `2` | Refresh interval when `--watch` is set |

## Agent Recipes

```bash
ctxt status --format json
ctxt status --server http://127.0.0.1:8081 --format json
```

## Next Steps

- Start daemon if unreachable: [../dpkms/serve.md](../dpkms/serve.md) · Switch target instance: [instance.md](instance.md)
