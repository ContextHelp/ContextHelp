# `ctxt status`

Check reachability and health of the configured `dpkms` instance.

- **Use `ctxt status`** as a quick preflight before capture/search/compose.
- **Use `dpkms healthcheck`** for scriptable `/healthz` exit-code probes (`--quiet`) ([../dpkms/lifecycle.md](../dpkms/lifecycle.md)).
- **Use `ctxt stats`** for corpus counts ([stats.md](stats.md)).

## Execution Profile

- **Side effect**: `read`
- **Idempotency**: `yes`

## Usage

```bash
ctxt status [flags]
```

## Key Flags

| Flag | Default | Purpose |
|---|---|---|
| `--server <url>` | `http://localhost:8080` | Target `dpkms` HTTP URL |
| `--watch` | `false` | Refresh continuously until Ctrl-C (avoid in agent scripts) |
| `--interval <sec>` | `2` | Seconds between refreshes when `--watch` is set |

## Agent Recipes

```bash
# Single-shot health check as JSON
ctxt status --format json

# Check a remote or non-default instance
ctxt status --server http://127.0.0.1:8081 --format json
```

## Next Steps

- Start daemon if unreachable: [../dpkms/serve.md](../dpkms/serve.md) · Switch target instance: [instance.md](instance.md)
