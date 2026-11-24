# `dpkms config`

Inspect the resolved `dpkms` configuration file cascade (`path`, `paths`) and validate the effective `dpkms` configuration (`validate`).

## Execution Profile

- **Side effect**: `read`
- **Idempotency**: `yes`

## Subcommands & Flags

```bash
dpkms config path     [--from <dir>]
dpkms config paths    [--from <dir>]
dpkms config validate
```

| Flag | Subcommand(s) | Default | Purpose |
|---|---|---|---|
| `--from <dir>` | `path`, `paths` | CWD | Resolve 4-layer config cascade from `<dir>` |

## Agent Recipes

```bash
dpkms config path --format json
dpkms config paths --from /path/to/project --format json
dpkms config validate
```

## Next Steps

- Edit, lint, back up, or restore `ctxt` config: [../ctxt/config.md](../ctxt/config.md)
