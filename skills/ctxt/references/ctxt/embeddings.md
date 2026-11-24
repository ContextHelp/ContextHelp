# `ctxt embeddings`

Manage the full embedding-model lifecycle: inspect resolved provider, register candidate models (measuring dimensions), backfill corpus vectors in background (`migrate`), flip default gated on coverage (`set-default`), schedule retirement (`deprecate`), and purge retired vectors (`purge`).

## Execution Profile

| Subcommand | Side effect | Idempotency | Confirmation |
|---|---|---|---|
| `list` / `provider` | `read` | `yes` | — |
| `register` / `migrate` | `write-shared` | `no` | — |
| `set-default` | `write-shared` | `yes` | — |
| `deprecate` / `purge` | `destructive-shared` | `yes` | `--confirm=yes --confirm-token=<sha>` |

## Subcommands & Flags

```bash
ctxt embeddings list
ctxt embeddings provider
ctxt embeddings register <model_id> [--dimension <n>]
ctxt embeddings migrate --to <model_id> [--batch <n>] [--rate-limit <N|N/s>]
ctxt embeddings set-default <model_id> [--min-coverage <0..1>]
ctxt embeddings deprecate <model_id> [--on <date-or-rfc3339>] --confirm=yes --confirm-token=<sha>
ctxt embeddings purge <model_id> --confirm=yes --confirm-token=<sha>
```

| Flag | Subcommand | Default | Purpose |
|---|---|---|---|
| `--dimension <n>` | `register` | `0` | Expected vector dimension (fails if measured dimension differs) |
| `--to <model_id>` | `migrate` | `""` | Registered `model_id` to backfill |
| `--batch <n>` | `migrate` | config | Batch size per provider request |
| `--rate-limit <val>` | `migrate` | `0` | Max provider calls/sec (`N` or `N/s`; `0` = no cap) |
| `--min-coverage <f>` | `set-default` | config | Minimum corpus coverage (`0..1`) required before flipping default |
| `--on <date>` | `deprecate` | now | Effective date (`YYYY-MM-DD`, local datetime, or RFC3339) |

## Agent Recipes

```bash
# 1. Inspect resolved provider and current models
ctxt embeddings provider --format json
ctxt embeddings list --format json

# 2. Register candidate, backfill vectors with rate cap, and promote once coverage meets threshold
ctxt embeddings register voyage-3-large@2025-08-01 --dimension 1024
ctxt embeddings migrate --to voyage-3-large@2025-08-01 --batch 32 --rate-limit 5/s
ctxt embeddings set-default voyage-3-large@2025-08-01 --min-coverage 0.99

# 3. Retire and purge old model after grace period
ctxt embeddings deprecate old-model --on 2026-12-31 --confirm=yes --confirm-token=<sha>
ctxt embeddings purge old-model --confirm=yes --confirm-token=<sha>
```

## Next Steps

- Verify vector search quality after cutover: `ctxt find "<q>" --semantic --explain` ([find.md](find.md))
