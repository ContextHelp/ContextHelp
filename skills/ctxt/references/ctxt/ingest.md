# `ctxt ingest`

Pull structured objects from external CLI adapters (`cardamum`, `himalaya`, or any JSON-emitting adapter via stdin) with built-in deduplication.

## Execution Profile

- **Side effect**: `write`
- **Idempotency**: `yes` — deduplicates against existing objects, so re-running converges without duplicating records.

## Usage & Flags

```bash
ctxt ingest --source <adapter> [flags]
```

| Flag | Default | Purpose |
|---|---|---|
| `--source <name>` | `""` | Adapter name (**required**; built-ins: `cardamum`, `himalaya`) |
| `--stdin` | `false` | Read JSON array of objects from stdin |
| `--every <dur>` | `0` | Continuous polling cadence (one-shot when absent) |
| `--type <kind>` | `""` | Override object type |
| `--addressbook <id>` | `default` | `cardamum` addressbook ID |
| `--account <name>` | `""` | Adapter account name |
| `--binary <path>` | `""` | Path to adapter binary |
| `--folder <name>` | `INBOX` | `himalaya` IMAP folder |
| `--max-items <n>` | `0` | Max items to fetch (`0` = all) |

## Agent Recipes

```bash
# Ingest contacts from cardamum
ctxt ingest --source cardamum --addressbook default --format json

# Ingest messages from himalaya
ctxt ingest --source himalaya --account work --folder INBOX --max-items 50

# Pipe a custom adapter's JSON array via stdin
my-adapter --json | ctxt ingest --source my-adapter --stdin
```

## Next Steps

- Browse ingested objects: [list.md](list.md) · Inspect stalled jobs: [job.md](job.md)
