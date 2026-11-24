# `dpkms config`

Inspect the resolved `dpkms` configuration file path and full 4-layer config cascade (project markers `.ctxt/config.yaml`, `.ctxt.yaml`, `ctxt.yaml` → user XDG → system).

- **Use `dpkms config`** to verify which config files `dpkms` resolves from a given directory.
- **Use `ctxt config`** to show values, validate/lint, edit, back up, or restore configuration ([../ctxt/config.md](../ctxt/config.md)).

## Execution Profile

- **Side effect**: `read`
- **Idempotency**: `yes`

## Subcommands & Flags

```bash
dpkms config path  [--from <dir>] [--format <text|json|yaml>]
dpkms config paths [--from <dir>] [--format <text|json|yaml>]
```

| Flag | Default | Purpose |
|---|---|---|
| `--from <dir>` | current working directory | Resolve config cascade as if invoked from `<dir>` |

## Agent Recipes

```bash
# Print highest-precedence existing config file
dpkms config path --format json

# Inspect full precedence chain from a specific project directory
dpkms config paths --from /path/to/project --format json
```

## Next Steps

- Validate and lint config contents: [../ctxt/config.md](../ctxt/config.md)
