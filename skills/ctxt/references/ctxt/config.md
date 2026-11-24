# `ctxt config`

Inspect, locate, validate, lint, edit, back up (with optional encryption), and restore `ctxt` configuration.

## Execution Profile

| Subcommand | Side effect | Idempotency | Confirmation |
|---|---|---|---|
| `show`, `path`, `paths` | `read` | `yes` | — |
| `validate`, `doctor`, `edit` | `write-local` | `yes` | — |
| `backup` | `write-local` | `no` | — |
| `restore <bundle.zip>` | `destructive-local` | `no` | `--confirm=yes --confirm-token=<sha>` |

## Subcommands & Flags

```bash
ctxt config show
ctxt config path  [--from <dir>]
ctxt config paths [--from <dir>]
ctxt config validate [--check-secrets=<true|false>]
ctxt config doctor [--fix]
ctxt config edit
ctxt config backup [--output <dir>] [--encrypt] [--passphrase <pw>]
ctxt config restore <bundle.zip> [--verify] [--dry-run] --confirm=yes --confirm-token=<sha>
```

| Flag | Subcommand | Default | Purpose |
|---|---|---|---|
| `--from <dir>` | `path`, `paths` | CWD | Resolve config cascade from `<dir>` |
| `--check-secrets` | `validate` | `true` | Scan config fields for plaintext secrets and warn |
| `--fix` | `doctor` | `false` | Auto-apply safe fixes (e.g. `chmod 600` on config file) |
| `--encrypt` | `backup` | `false` | Encrypt bundle with AES-256-GCM + Argon2id |
| `--passphrase <pw>` | `backup` | `""` | Passphrase for `--encrypt` (prefer `CTXT_BACKUP_PASSPHRASE` env or keychain) |
| `--verify` | `restore` | `false` | Verify Ed25519 signature before restoring (**recommended**) |

## Agent Recipes

```bash
ctxt config validate --check-secrets
ctxt config doctor
ctxt config paths --format json
CTXT_BACKUP_PASSPHRASE="$PASS" ctxt config backup --encrypt --output /tmp/cfg-backup
ctxt config restore /tmp/cfg-backup/bundle.zip --verify --dry-run
```

## Gotchas & Next Steps

- Never pass `--passphrase` in CLI args; use `CTXT_BACKUP_PASSPHRASE` or keychain.
- Next: `dpkms` config validation ([../dpkms/config.md](../dpkms/config.md)) or signing keys ([../dpkms/key.md](../dpkms/key.md)).
