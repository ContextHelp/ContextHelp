# `ctxt config`

Inspect, locate, validate, lint, edit, back up, and restore `ctxt` configuration (`$CTXT_CONFIG` → `-c/--config` → XDG `contexthelp/ctxt.yaml`).

## Execution Profile

| Subcommand | Side effect | Idempotency | Confirmation |
|---|---|---|---|
| `config show` / `path` | `read` | `yes` | — |
| `config validate` | `write-local` (may write migrations) | `yes` | — |
| `config doctor` | `write-local` (with `--fix`) | `yes` | — |
| `config edit` | `write-local` (`$EDITOR`) | `yes` | — |
| `config backup` | `write-local` | `no` | — |
| `config restore <bundle.zip>` | `destructive-local` | `no` | `--confirm=yes --confirm-token=<sha>` |

## Subcommands & Flags

```bash
ctxt config show
ctxt config path
ctxt config validate [--check-secrets=<true|false>]
ctxt config doctor [--fix]
ctxt config edit
ctxt config backup [--output <dir>] [--encrypt] [--passphrase <pw>]
ctxt config restore <bundle.zip> [--verify] [--dry-run] --confirm=yes --confirm-token=<sha>
```

| Flag | Subcommand | Default | Purpose |
|---|---|---|---|
| `--check-secrets` | `validate` | `true` | Scan config fields for plaintext secrets and warn |
| `--fix` | `doctor` | `false` | Auto-apply safe fixes (e.g. `chmod 600` on world-readable config) |
| `--encrypt` | `backup` | `false` | Encrypt bundle with AES-256-GCM + Argon2id |
| `--passphrase <pw>` | `backup` | `""` | Passphrase for `--encrypt` (prefer `CTXT_BACKUP_PASSPHRASE` env) |
| `--verify` | `restore` | `false` | Verify Ed25519 signature before restoring (**recommended**) |

`config backup` uses global `--output` (`-o`) for target directory; `config restore` honors global `--dry-run`.

## Agent Recipes

```bash
# Pre-flight validation in CI/agent runs
ctxt config validate --check-secrets
ctxt config doctor

# Inspect effective resolved config as JSON
ctxt config show --format json

# Create a signed, encrypted config backup using env passphrase
CTXT_BACKUP_PASSPHRASE="$PASS" ctxt config backup --encrypt --output /tmp/cfg-backup

# Preview a config restore with signature verification
ctxt config restore /tmp/cfg-backup/bundle.zip --verify --dry-run
```

## Gotchas & Next Steps

- Never pass `--passphrase` directly in CLI args; use `CTXT_BACKUP_PASSPHRASE` or keychain.
- Restored config takes effect on next process start; validate afterward with `ctxt config validate`.
- Next: inspect `dpkms` config cascade ([../dpkms/config.md](../dpkms/config.md)) or manage signing keys ([../dpkms/key.md](../dpkms/key.md)).
