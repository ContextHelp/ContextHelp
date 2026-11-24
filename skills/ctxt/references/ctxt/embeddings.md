# `ctxt embeddings`

Manage the embedding-model registry (list, register candidate models, migrate corpus, flip active default, deprecate, and purge retired models).

## Execution Profile

| Subcommand | Side effect | Idempotency | Confirmation |
|---|---|---|---|
| `embeddings list` | `read` | `yes` | — |
| `embeddings register <model_id>` | `write-shared` | `no` | — |
| `embeddings migrate` | `write-shared` | `no` | — |
| `embeddings set-default <model_id>` | `write-shared` | `yes` | — |
| `embeddings deprecate <model_id>` | `destructive-shared` | `yes` | `--confirm=yes --confirm-token=<sha>` |
| `embeddings purge <model_id>` | `destructive-shared` | `yes` | `--confirm=yes --confirm-token=<sha>` |

## Subcommands & Flags

```bash
ctxt embeddings list
ctxt embeddings register <model_id> [--model-config <json>] [--provider <p>] [--dimension <n>] [--make-default]
ctxt embeddings migrate <model_id>
ctxt embeddings set-default <model_id>
ctxt embeddings deprecate <model_id> --confirm=yes --confirm-token=<sha>
ctxt embeddings purge <model_id> --confirm=yes --confirm-token=<sha>
```

### `register` flags

| Flag | Default | Purpose |
|---|---|---|
| `--model-config <path>` | `""` | Path to JSON model-config file (omitted opens `$EDITOR` skeleton) |
| `--provider <name>` | `""` | Provider name (e.g. `openai`, `ollama`, `voyage`) |
| `--dimension <n>` | `0` | Embedding vector dimension |
| `--make-default` | `false` | Immediately mark as active default (bypasses coverage/recall guards) |

> Note: `--model-config` is used instead of `--config` (which is reserved as the global config flag).

## Agent Recipes

```bash
# Check registered models and corpus coverage
ctxt embeddings list --format json

# Register a candidate embedding model non-interactively
ctxt embeddings register voyage-3-large@2025-08-01 \
  --model-config ./voyage.json --provider voyage --dimension 1024
```

## Gotchas & Next Steps

- In the current build, `migrate`, `set-default`, `deprecate`, and `purge` are phased stubs and return a not-yet-implemented error; check `ctxt embeddings list` first and use `dpkms dev reindex-vectors` ([../dpkms/dev.md](../dpkms/dev.md)) to re-embed missing vectors.
- Never omit `--model-config` in non-interactive scripts or `register` will try to launch `$EDITOR`.
