# `dpkms housekeeping`

Run database maintenance, search index rebuilds, storage compaction, old-data pruning, unreferenced blob GC, and zombie-job reaping.

- **Use `dpkms housekeeping`** for storage-layer optimization and cleanup.
- **Use `ctxt doctor`** for knowledge-graph content hygiene ([../ctxt/doctor.md](../ctxt/doctor.md)) or `dpkms doctor` for host access posture ([access-governance.md](access-governance.md)).

## Execution Profile

| Subcommand | Side effect | Idempotency |
|---|---|---|
| `vacuum` / `reindex` / `compact` | `destructive` | `yes` |
| `prune` | `destructive` | `yes` |
| `run` | `destructive` (all maintenance tasks; supports global `--dry-run`) | `yes` |

## Subcommands & Flags

```bash
dpkms housekeeping vacuum
dpkms housekeeping reindex
dpkms housekeeping compact
dpkms housekeeping prune --before <iso-date>
dpkms housekeeping run [--zombie-threshold <dur>] [--blob-grace <dur>] [--dry-run]
```

| Flag | Subcommand | Default | Purpose |
|---|---|---|---|
| `--before <iso>` | `prune` | `""` | Remove data before ISO date (required unless `--dry-run`) |
| `--zombie-threshold <dur>` | `run` | built-in | Jobs running longer than this are treated as zombies |
| `--blob-grace <dur>` | `run` | built-in | Keep unreferenced blobs younger than this (protects in-flight ingests) |

## Agent Recipes

```bash
# Always preview full maintenance with global --dry-run first
dpkms housekeeping run --dry-run

# Run full maintenance with explicit thresholds
dpkms housekeeping run --zombie-threshold 30m --blob-grace 1h

# Rebuild FTS indexes or reclaim space after bulk deletes
dpkms housekeeping reindex
dpkms housekeeping vacuum && dpkms housekeeping compact
```

## Gotchas & Next Steps

- Take a backup ([backup-restore.md](backup-restore.md)) before running `housekeeping prune --before <date>`.
- For embedding-model backfills, use `ctxt embeddings migrate` ([../ctxt/embeddings.md](../ctxt/embeddings.md)).
