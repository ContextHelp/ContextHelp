# `ctxt log`

Query the knowledge changelog (chronological record of object creates, edits, deletes, and enrichments).

- **Use `log`** for knowledge-graph mutation history.
- **Use `audit`** for SIEM-formatted compliance/security audit exports ([audit.md](audit.md)).

## Execution Profile

- **Side effect**: `read`
- **Idempotency**: `yes`

## Usage

```bash
ctxt log [flags]
```

## Key Flags

| Flag | Default | Purpose |
|---|---|---|
| `--limit <n>` | `20` | Max entries to return |
| `--type <csv>` | `""` | Filter by event type (comma-separated, e.g. `delete,edit`) |
| `--since <YYYY-MM-DD>` | `""` | Entries after date |
| `--until <YYYY-MM-DD>` | `""` | Entries before date |
| `--object <id>` | `""` | Filter by knowledge object ID |
| `--actor <name>` | `""` | Filter by actor |
| `--server <url>` | `http://localhost:8080` | Target `dpkms` HTTP URL |

## Agent Recipes

```bash
# Review recent deletions after a cleanup
ctxt log --type delete --limit 50 --format json

# Trace all changes to a single object
ctxt log --object obj_12345678 --format json

# Review mutations in a date range by actor
ctxt log --actor agent-bot --since 2026-02-01 --until 2026-02-28 --format json
```

## Next Steps

- Inspect a logged object: [show.md](show.md) · Export SIEM audit logs: [audit.md](audit.md)
