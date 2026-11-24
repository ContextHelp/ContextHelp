# `dpkms backup` & `dpkms restore`

Create full backups of `dpkms` user data (SQLite DB, optional blobs and configs/keys) and restore from a `.tar.gz` archive or backup directory.

- **Use `dpkms backup` / `restore`** for full knowledge-base & blob disaster recovery.
- **Use `ctxt config backup` / `restore`** for signed/encrypted config-only bundles ([../ctxt/config.md](../ctxt/config.md)).

## Execution Profile

| Command | Side effect | Idempotency |
|---|---|---|
| `dpkms backup` | `write-local` (creates archive) | `no` |
| `dpkms restore <src>` | `destructive-local` (overwrites target data) | `yes` (when restoring same snapshot) |

## Usage & Flags

### `dpkms backup`

```bash
dpkms backup [flags]
```

| Flag | Default | Purpose |
|---|---|---|
| `--output-dir <dir>` | `""` | Override output directory path |
| `--include-blobs` | `false` | Include local blob files in archive |
| `--include-configs` | `false` | Include config files and keys in archive |
| `--skip-blob-errors` | `false` | Skip unreadable blobs instead of failing |
| `--async` | `false` | Enqueue backup as a background job and exit |

### `dpkms restore`

```bash
dpkms restore <archive.tar.gz | directory> [flags]
```

| Flag | Default | Purpose |
|---|---|---|
| `--dry-run` | `false` | Show what would be restored without writing |
| `--skip-configs` | `false` | Restore data only; skip overwriting config files |

## Agent Recipes

```bash
# Full synchronous backup including blobs and configs
dpkms backup --include-blobs --include-configs --output-dir /tmp/dpkms-backups --format json

# Always dry-run a restore before overwriting live data
dpkms restore /tmp/dpkms-backups/backup.tar.gz --dry-run

# Restore data while preserving current machine's config
dpkms restore /tmp/dpkms-backups/backup.tar.gz --skip-configs
```

## Gotchas & Next Steps

- Stop the running daemon (`dpkms shutdown`) before restoring over a live SQLite database, then start it again (`dpkms serve`).
- Next: lifecycle control ([lifecycle.md](lifecycle.md)) or DB integrity maintenance ([housekeeping.md](housekeeping.md)).
