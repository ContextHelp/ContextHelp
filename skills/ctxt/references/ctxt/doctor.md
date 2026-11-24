# `ctxt doctor`

Run automated read-only health checks on the knowledge graph (orphans, missing metadata, near-duplicates, stale objects).

- **Use `ctxt doctor`** for knowledge-graph data quality checks.
- **Use `ctxt config doctor`** to lint configuration files, permissions, and plaintext secrets ([config.md](config.md)).
- **Use `ctxt status`** to probe `dpkms` daemon reachability ([status.md](status.md)).

## Execution Profile

- **Side effect**: `read`
- **Idempotency**: `yes`

## Usage

```bash
ctxt doctor [flags]
```

## Key Flags

| Flag | Default | Purpose |
|---|---|---|
| `--check <list>` | all | Checks to run: `orphans`, `missing_metadata`, `duplicates`, `stale` (repeatable/CSV) |
| `--limit <n>` | `0` | Max issues to report (`0` = unlimited) |
| `--stale-days <n>` | `90` | Days without update before flagging as stale |
| `--duplicate-threshold <f>` | `0.95` | Cosine similarity threshold for near-duplicate detection |

## Agent Recipes

```bash
# Full graph health report as JSON
ctxt doctor --format json

# Check only near-duplicates and missing metadata with tighter threshold
ctxt doctor --check duplicates,missing_metadata --duplicate-threshold 0.92 --limit 25 --format json

# Find stale objects untouched for 180+ days
ctxt doctor --check stale --stale-days 180 --format json
```

## Next Steps

- Re-enrich objects missing metadata: [reprocess.md](reprocess.md) · Remove confirmed duplicates: [delete.md](delete.md) · Optimize DB: [../dpkms/housekeeping.md](../dpkms/housekeeping.md)
