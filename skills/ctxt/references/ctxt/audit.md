# `ctxt audit`

Page through or export the append-only security/compliance audit log in SIEM-ready formats.

- **Use `ctxt audit`** for security audit logs and SIEM export (`json` NDJSON, `cef`, `syslog`).
- **Use `ctxt share audit`** for sharing visibility decisions ([share.md](share.md)) or `dpkms audit verify` to verify the steward audit hash chain ([../dpkms/access-governance.md](../dpkms/access-governance.md)).

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
| `--event-type <type>` | `""` | Filter by event type (`create`, `delete`, etc.) |
| `--object-id <id>` | `""` | Filter by object ID |
| `--since <duration>` | `""` | Entries within duration ago (e.g. `24h`, `7d`) |
| `--limit <n>` | `100` | Max entries (`0` = unlimited) |
| `--offset <n>` | `0` | Pagination offset |

## Agent Recipes

```bash
ctxt audit list --actor alice@example.com --since 24h --limit 50 --format json
ctxt audit export --format cef --since 24h
```

## Next Steps

- Knowledge changelog: [log.md](log.md) · Steward chain verification: [../dpkms/access-governance.md](../dpkms/access-governance.md)
