# `dpkms backup` & `dpkms restore`

Create full backups of `dpkms` user data (DB, optional blobs and configs/keys — encrypted `.tar.gz.enc` when storage encryption is enabled) and restore from an archive or directory.

- **Use `dpkms backup` / `restore`** for full knowledge-base & blob disaster recovery.
- **Use `ctxt config backup` / `restore`** for signed/encrypted config-only bundles ([../ctxt/config.md](../ctxt/config.md)).

## Execution Profile

| Command | Side effect | Confirmation |
|---|---|---|
| `dpkms backup` | `write-local` | — |
| `dpkms restore <src>` | `destructive-local` | `--confirm=yes --confirm-token=<sha>` (preview with global `--dry-run`) |

## Usage & Flags

### `dpkms backup`

```bash
dpkms backup [--output-dir <dir>] [--include-blobs] [--include-configs] [--skip-blob-errors] [--async]
```

| Flag | Default | Purpose |
|---|---|---|
| `--output-dir <dir>` | `""` | Override output directory path |
| `--include-blobs` | `false` | Include local blob files in archive |
| `--include-configs` | `false` | Include config files and keys in archive |
| `--skip-blob-errors` | `false` | Skip unreadable blobs instead of failing |
| `--async` | `false` | Enqueue backup as background job and exit |

### `dpkms restore`

```bash
dpkms restore <archive.tar.gz | directory> [--skip-configs] [--dry-run] --confirm=yes --confirm-token=<sha>
```

| Flag | Default | Purpose |
|---|---|---|
| `--skip-configs` | `false` | Restore data only; keep existing config files |

## Agent Recipes

```bash
# Synchronous full backup with blobs and configs
dpkms backup --include-blobs --include-configs --output-dir /tmp/dpkms-backups --format json

# Preview restore first with global --dry-run
dpkms restore /tmp/dpkms-backups/backup.tar.gz --dry-run

# Restore data only while keeping local config
dpkms restore /tmp/dpkms-backups/backup.tar.gz --skip-configs --confirm=yes --confirm-token=<sha>
```

## Gotchas & Next Steps

- Stop `dpkms` (`dpkms shutdown`) before restoring over a live SQLite DB, then restart (`dpkms serve`).
- When storage encryption is enabled, backups fail closed if the configured `KeyProvider` is unavailable.
- Next: lifecycle ([lifecycle.md](lifecycle.md)), encryption ([storage.md](storage.md)), or maintenance ([housekeeping.md](housekeeping.md)).
