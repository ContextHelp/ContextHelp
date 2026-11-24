# `dpkms storage`

Manage storage encryption at rest: migrate an existing plaintext SQLite database and local blobs to encrypted storage, and rotate encryption keys (O(1) KEK envelope rewrap or full SQLite DEK page rekey).

- **Use `dpkms storage`** for whole-database and blob encryption at rest (configured via `storage.encryption` in `dpkms.yaml`).
- **Use `ctxt capture --encrypt` / `--encrypt-scope`** or `ctxt watch start --encrypt` for opt-in per-capture selective encryption ([../ctxt/capture.md](../ctxt/capture.md), [../ctxt/watch.md](../ctxt/watch.md)).

## Execution Profile

| Subcommand | Side effect | Idempotency |
|---|---|---|
| `storage encrypt` | `write-local` (encrypts DB + blobs + writes `<db>.key` envelope) | `conditional` (fails closed if missing key provider or non-encrypted build) |
| `storage rekey` | `write-local` (rotates KEK envelope or re-encrypts DB pages) | `yes` |

## Subcommands & Flags

```bash
dpkms storage encrypt
dpkms storage rekey [--rewrap-only=<true|false>] [--to-key <kek-id>]
```

| Flag | Subcommand | Default | Purpose |
|---|---|---|---|
| `--rewrap-only` | `rekey` | `true` | Rotate only the KEK sidecar envelope (O(1), no page rewrite); pass `--rewrap-only=false` for full SQLite DEK rekey |
| `--to-key <id>` | `rekey` | provider current key | Target KEK identifier |

## Agent Recipes

```bash
# 1. Stop daemon and take a backup before migrating plaintext storage
dpkms shutdown && dpkms backup --include-blobs --include-configs

# 2. Encrypt plaintext SQLite DB and local blobs at rest
dpkms storage encrypt

# 3. Fast O(1) KEK envelope rotation to a new key ID
dpkms storage rekey --rewrap-only --to-key kek-2026-q4

# 4. Full DEK rekey (rewrites SQLite pages)
dpkms storage rekey --rewrap-only=false

# 5. Verify host encryption & permissions posture
dpkms doctor --strict
```

## Gotchas & Next Steps

- Whole-database SQLite encryption requires an encrypted binary (`SQLite3MultipleCiphers`, built via `make build-encrypted`); standard builds fail closed with `ErrEncryptionUnsupported`.
- Encryption is fail-closed: backups, ambient spool, pre-migration snapshots, and daemon startup refuse to run if `storage.encryption.enabled: true` and no `KeyProvider` is unlocked.
- Next: verify host posture ([access-governance.md](access-governance.md)) or back up data ([backup-restore.md](backup-restore.md)).
