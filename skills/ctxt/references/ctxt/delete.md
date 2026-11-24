# `ctxt delete`

Permanently delete knowledge objects by ID, filter, or `source_key` (with optional erasure tombstone to block future re-ingestion). **Irreversible.**

## Execution Profile

- **Side effect**: `destructive` (blocked if any matching object is under an active hold, see [hold.md](hold.md))
- **Idempotency**: `yes`
- **Confirmation**: requires `--confirm=yes --confirm-token=<sha>` (run once to inspect scope and obtain the token).

## Usage & Flags

```bash
ctxt delete <filter-flags> --confirm=yes --confirm-token=<sha>
```

| Flag | Default | Purpose |
|---|---|---|
| `--id <id>` | `""` | Delete specific knowledge object |
| `--tagged <tag>` | `""` | Delete objects carrying this **one** literal tag (no list, no leading `+`/`-`; spaces allowed) |
| `--mention <@slug>` | `""` | Delete by mention |
| `--type <kind>` | `""` | Delete by object type |
| `--source-key <key>` | `""` | Erase all objects (and derived data/blobs) matching this `source_key` |
| `--tombstone` | `false` | With `--source-key`, record an erasure tombstone so the `source_key` cannot be re-ingested |
| `--all` | `false` | Delete all active objects |

## Agent Recipes

```bash
# 1. Inspect matching objects before deleting
ctxt list --q "source_key==slack:C123:1700000000" --format json

# 2. Erase by source_key and tombstone against re-ingestion
ctxt delete --source-key "slack:C123:1700000000" --tombstone --confirm=yes --confirm-token=<sha>

# Delete one object by ID
ctxt delete --id obj_12345678 --confirm=yes --confirm-token=<sha>
```

## Gotchas & Next Steps

- Active legal/admin holds block deletion transactionally; inspect holds via `ctxt hold list` ([hold.md](hold.md)).
- For automatic scheduled erasure/redaction instead of immediate deletion, use [expire.md](expire.md).
- Next: verify removal ([list.md](list.md)) or review audit trail ([log.md](log.md)).
