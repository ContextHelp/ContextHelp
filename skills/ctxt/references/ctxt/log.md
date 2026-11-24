# `ctxt log`

Query the knowledge changelog (chronological history of object creates, edits, deletes, and enrichments).

- **Use `log`** for knowledge-graph mutation history.
- **Use `audit`** for SIEM compliance exports ([audit.md](audit.md)) or `share audit` for sharing read decisions ([share.md](share.md)).

## Execution Profile

- **Side effect**: `read`
- **Idempotency**: `yes`

## Usage & Flags

```bash
ctxt log [flags]
```

| Flag | Default | Purpose |
|---|---|---|
| `--limit <n>` | `20` | Max entries to return |
| `--type <csv>` | `""` | Filter by event type (comma-separated, e.g. `delete,edit`) |
| `--since <YYYY-MM-DD>` | `""` | Entries after date |
| `--until <YYYY-MM-DD>` | `""` | Entries before date |
| `--object <id>` | `""` | Filter by object ID |
| `--actor <name>` | `""` | Filter by actor |

## Agent Recipes

```bash
# Review recent deletions
ctxt log --type delete --limit 50 --format json

# Trace all mutations on one object
ctxt log --object obj_12345678 --format json
```

## Next Steps

- Inspect a logged object: [show.md](show.md) · Export SIEM audit logs: [audit.md](audit.md)
