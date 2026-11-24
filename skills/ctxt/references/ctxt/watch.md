# `ctxt watch`

Manage passive background watchers (clipboard and configured directories), with optional automatic expiry (`--ttl`/`--expires-scope`) and encryption (`--encrypt`/`--encrypt-scope`) applied to every watched capture.

## Execution Profile

| Subcommand | Side effect | Idempotency |
|---|---|---|
| `watch status` | `read` | `yes` |
| `watch start` | `write` (blocks foreground) | `yes` |
| `watch stop` | `write` | `yes` |
| `watch enable <watcher>` | `write` | `yes` |
| `watch disable <watcher>` | `write` | `yes` |

## Subcommands & Flags

```bash
ctxt watch start [--ttl <dur>] [--expires-scope <nl>] [--encrypt] [--encrypt-scope <nl>]
ctxt watch status
ctxt watch stop
ctxt watch enable <watcher>
ctxt watch disable <watcher>
```

### `watch start` flags

| Flag | Default | Purpose |
|---|---|---|
| `--ttl <dur>` | `""` | Auto-expire every watched capture after duration (e.g. `24h`, `7d`) |
| `--expires-scope <nl>` | `""` | Optional natural-language description of what to expire on watched captures |
| `--encrypt` | `false` | Encrypt every watched capture at rest using configured key provider |
| `--encrypt-scope <nl>` | `""` | Optional natural-language scope to seal on watched captures; implies `--encrypt` |

## Agent Recipes

```bash
# Inspect local watcher state
ctxt watch status --format json

# Start directory watchers with 7d TTL and PII encryption, clipboard disabled
CTXT_NO_CLIPBOARD=1 ctxt watch start --ttl 7d --encrypt-scope "PII and phone numbers"

# Stop running watcher for the resolved instance
ctxt watch stop
```

## Gotchas & Next Steps

- Set `CTXT_NO_CLIPBOARD=1` in headless/agent runs to prevent unintended clipboard ingestion.
- Encryption flags fail closed if no key provider is configured on `dpkms`.
- Next: configure watched dirs ([config.md](config.md)), manage expiry ([expire.md](expire.md)), or inspect storage encryption ([../dpkms/storage.md](../dpkms/storage.md)).
