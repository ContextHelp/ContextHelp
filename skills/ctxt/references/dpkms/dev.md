# `dpkms dev`

Developer and maintenance utilities: backfill missing vector embeddings, lint registry YAML schemas, scaffold new plugins, and generate CLI docs. Replaces deprecated `ctxt dev`.

## Execution Profile

| Subcommand | Side effect | Idempotency |
|---|---|---|
| `dev validate-registry <file>` | `read` | `yes` |
| `dev reindex-vectors` | `write` (computes & stores embeddings) | `yes` |
| `dev init-plugin <slug>` | `write-local` (scaffolds files) | `no` |
| `dev gen-docs` | `write-local` (writes markdown docs) | `yes` |

## Subcommands & Flags

```bash
dpkms dev reindex-vectors
dpkms dev validate-registry <file>
dpkms dev init-plugin <slug> [--dir <path>]
dpkms dev gen-docs [--dir <path>]
```

| Flag | Subcommand | Default | Purpose |
|---|---|---|---|
| `--dir <path>` | `init-plugin` | `plugins` | Parent directory for the scaffolded plugin |
| `--dir <path>` | `gen-docs` | `docs/cli` | Output directory for generated CLI reference docs |

## Agent Recipes

```bash
# Backfill missing vector embeddings after changing embedding provider/model
dpkms dev reindex-vectors

# Lint a registry definition before submitting
dpkms dev validate-registry ./registry.yaml

# Scaffold a new plugin (manifest.json + Go stub + README)
dpkms dev init-plugin pdf-table-extractor --dir ./plugins

# Regenerate CLI markdown docs from Cobra command tree
dpkms dev gen-docs --dir docs/cli
```

## Next Steps

- Register/inspect embedding models: [../ctxt/embeddings.md](../ctxt/embeddings.md) · Submit validated registry bundle: [../ctxt/registry.md](../ctxt/registry.md)
