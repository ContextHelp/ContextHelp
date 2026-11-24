# `ctxt expire`

Schedule or clear automatic expiration on an existing knowledge object.

By default, the entire object (plus derived objects/blobs) is erased when the expiry timestamp passes. Pass `--scope` with a natural-language description (e.g. `"PII and phone numbers"` or `"raw content, keep summary"`) to selectively redact only matching fields/spans in place.

- **Use `expire`** to add, update, or remove expiration on an already-stored object.
- To set expiration at capture time, use `ctxt capture --ttl|--expires [--expires-scope]` ([capture.md](capture.md)) or `ctxt watch start --ttl` ([watch.md](watch.md)).

## Execution Profile

| Subcommand | Side effect | Idempotency |
|---|---|---|
| `expire set <id> <time-or-ttl>` | `write` | `yes` |
| `expire clear <id>` | `write` | `yes` |

## Subcommands & Flags

```bash
ctxt expire set <id> <time-or-ttl> [--scope "<natural-language-scope>"]
ctxt expire clear <id>
```

`<time-or-ttl>` accepts `RFC3339` timestamps, Go durations + `d`/`w` (`30m`, `24h`, `7d`, `2w`), or natural-language dates (must be in the future).

| Flag | Subcommand | Default | Purpose |
|---|---|---|---|
| `--scope <nl>` | `set` | `""` (whole object) | Natural-language description of what to expire in place (e.g. `"PII and phone numbers"`, `"raw content, keep summary"`) |

## Agent Recipes

```bash
# Expire entire object in 7 days
ctxt expire set obj_12345678 7d

# Selectively redact PII in 30 days while keeping the rest of the object
ctxt expire set obj_12345678 30d --scope "PII and phone numbers"

# Schedule expiry at an exact RFC3339 timestamp
ctxt expire set obj_12345678 "2026-12-31T00:00:00Z" --scope "raw content, keep summary"

# Retain indefinitely (remove scheduled expiry and scope)
ctxt expire clear obj_12345678
```

## Gotchas & Next Steps

- `dpkms serve`'s background `ExpirySweeper` executes expirations and skips any object under an active hold ([hold.md](hold.md)).
- Changing expiry on a held object is blocked by the storage-layer hold guard.
- Next: verify object metadata ([show.md](show.md)).
