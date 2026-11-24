# `ctxt audit`

Page through or export the append-only security/compliance audit log in SIEM-ready formats.

- **Use `audit`** for principal/security audit trails and SIEM exports (`json` NDJSON, `cef`, `syslog`).
- **Use `log`** for knowledge-object changelog inspection ([log.md](log.md)).

## Execution Profile

- **Side effect**: `read` (both `list` and `export`)
- **Idempotency**: `yes`

## Subcommands & Flags

```bash
ctxt audit list   [filter flags]
ctxt audit export [filter flags] [--format <json|cef|syslog>]
```

Both `list` and `export` share:

| Flag | Default | Purpose |
|---|---|---|
| `--actor <principal>` | `""` | Filter by actor |
| `--event-type <type>` | `""` | Filter by event type (e.g. `create`, `delete`) |
| `--object-id <id>` | `""` | Filter by object ID |
| `--since <duration>` | `""` | Entries within duration ago (e.g. `24h`, `7d`) |
| `--limit <n>` | `100` | Max entries (`0` = unlimited) |
| `--offset <n>` | `0` | Pagination offset |

`audit export` reads global `--format` (defaults to `json` NDJSON when unset):
- `json` — NDJSON (one JSON object per line)
- `cef` — ArcSight Common Event Format v0
- `syslog` — RFC 5424 structured-data lines

## Agent Recipes

```bash
# List recent audit events for one actor over the last 24h
ctxt audit list --actor alice@example.com --since 24h --limit 50 --format json

# Export last 24h as NDJSON or CEF for SIEM ingestion
ctxt audit export --format json --since 24h
ctxt audit export --format cef --since 24h
```

## Next Steps

- Inspect object-level changelog: [log.md](log.md)
