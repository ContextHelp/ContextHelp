# `dpkms dev`

Developer utilities for linting registry YAML manifests and generating CLI reference docs from the Cobra tree.

## Execution Profile

| Subcommand | Side effect | Idempotency |
|---|---|---|
| `dev validate-registry <file>` | `read` | `yes` |
| `dev gen-docs` | `write-local` | `yes` |

## Subcommands & Flags

```bash
dpkms dev validate-registry <file>
dpkms dev gen-docs [--dir <path>]
```

| Flag | Subcommand | Default | Purpose |
|---|---|---|---|
| `--dir <path>` | `gen-docs` | `docs/cli` | Output directory for generated docs |

## Agent Recipes

```bash
dpkms dev validate-registry ./registry.yaml
dpkms dev gen-docs --dir docs/cli
```

## Next Steps

- Submit validated registry bundle: [../ctxt/registry.md](../ctxt/registry.md)
