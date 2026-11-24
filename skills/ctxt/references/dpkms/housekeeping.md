# `dpkms housekeeping`

Run database maintenance, search index rebuilds, storage compaction, zombie-job cleanup, and old-data pruning.

- **Use `dpkms housekeeping`** for storage-layer optimization and cleanup.
- **Use `ctxt doctor`** for knowledge-graph content hygiene (orphans, duplicates, stale objects) ([../ctxt/doctor.md](../ctxt/doctor.md)).

## Execution Profile

| Subcommand | Side effect | Idempotency |
|---|---|---|
| `housekeeping vacuum` | `write` | `yes` |
| `housekeeping reindex` | `write` | `yes` |
| `housekeeping compact` | `write` | `yes` |
| `housekeeping prune` | `destructive` (deletes old data) | `yes` |
| `housekeeping run` | `write` / `destructive` (runs all tasks) | `yes` |

## Subcommands & Flags

```bash
dpkms housekeeping vacuum
dpkms housekeeping reindex
dpkms housekeeping compact
dpkms housekeeping prune --before <iso-date>
dpkms housekeeping run [--zombie-threshold <dur>] [--dry-run]
```

| Flag | Subcommand | Default | Purpose |
|---|---|---|---|
| `--before <iso>` | `prune` | `""` | Remove data created before this ISO date |
| `--zombie-threshold <dur>` | `run` | built-in default | Jobs running longer than this are treated as zombies |
| `--dry-run` | `run` | `false` | Report maintenance actions without executing them |

## Agent Recipes

```bash
# Always preview full maintenance before running
dpkms housekeeping run --dry-run

# Run full maintenance with a 30m zombie-job threshold
dpkms housekeeping run --zombie-threshold 30m

# Rebuild FTS/search indexes after manual DB repair
dpkms housekeeping reindex

# Reclaim space after bulk deletes
dpkms housekeeping vacuum && dpkms housekeeping compact
```

## Gotchas & Next Steps

- Take a backup ([backup-restore.md](backup-restore.md)) before running `housekeeping prune --before <date>`.
- To re-embed missing vector embeddings (distinct from SQLite/FTS `reindex`), use `dpkms dev reindex-vectors` ([dev.md](dev.md)).
