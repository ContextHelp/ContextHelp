# `ctxt doctor`

Run automated read-only health checks on the knowledge graph (orphans, missing metadata, near-duplicates, stale objects).

- **Use `ctxt doctor`** for knowledge-graph content hygiene.
- **Use `ctxt config doctor`** to lint config files and permissions ([config.md](config.md)).
- **Use `dpkms doctor`** to audit host access posture and encryption/permissions ([../dpkms/access-governance.md](../dpkms/access-governance.md)).

## Execution Profile

- **Side effect**: `read`
- **Idempotency**: `yes`

## Usage & Flags

```bash
ctxt doctor [--check <list>] [--limit <n>] [--stale-days <n>] [--duplicate-threshold <f>]
```

| Flag | Default | Purpose |
|---|---|---|
| `--check <list>` | all | `orphans`, `missing_metadata`, `duplicates`, `stale` (repeatable/CSV) |
| `--limit <n>` | `0` | Max issues to report (`0` = unlimited) |
| `--stale-days <n>` | `90` | Days without update before flagging stale |
| `--duplicate-threshold <f>` | `0.95` | Cosine similarity threshold for near-duplicates |

## Agent Recipes

```bash
ctxt doctor --format json
ctxt doctor --check duplicates,missing_metadata --duplicate-threshold 0.92 --limit 25 --format json
```

## Next Steps

- Reprocess missing metadata: [reprocess.md](reprocess.md) · Remove duplicates: [delete.md](delete.md) · Host posture: [../dpkms/access-governance.md](../dpkms/access-governance.md)
