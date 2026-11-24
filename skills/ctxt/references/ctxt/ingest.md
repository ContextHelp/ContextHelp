# `ctxt ingest`

Pull structured objects from external CLI adapters (`cardamum`, `himalaya`, or any JSON-emitting adapter via stdin) with built-in deduplication.

- **Use `ingest`** for live/recurring structured adapter feeds (`--every`) or custom script JSON arrays.
- **Use `import`** for platform exports and cloud APIs ([import/index.md](import/index.md)).

## Execution Profile

- **Side effect**: `write`
- **Idempotency**: `yes` — deduplicates against existing objects, so re-running converges without duplicating records.

## Usage

```bash
ctxt ingest --source <adapter> [flags]
```

## Key Flags

| Flag | Default | Purpose |
|---|---|---|
| `--source <name>` | `""` | Adapter name (**required**; built-ins: `cardamum`, `himalaya`) |
| `--stdin` | `false` | Read JSON array of objects from stdin |
| `--every <duration>` | `0` | Continuous polling cadence (e.g. `5m`; one-shot if omitted) |
| `--type <kind>` | `""` | Override object type |
| `--addressbook <id>` | `default` | `cardamum` addressbook ID |
| `--account <name>` | `""` | Adapter account name |
| `--binary <path>` | `""` | Explicit path to adapter binary |
| `--folder <name>` | `INBOX` | `himalaya` IMAP folder |
| `--max-items <n>` | `0` | Max items to fetch (`0` = all) |

## Agent Recipes

```bash
# Ingest contacts from cardamum
ctxt ingest --source cardamum --addressbook default --format json

# Ingest recent messages from himalaya
ctxt ingest --source himalaya --account work --folder INBOX --max-items 50

# Pipe a custom adapter's JSON array via stdin
my-adapter --json | ctxt ingest --source my-adapter --stdin

# Poll continuously every 5 minutes
ctxt ingest --source cardamum --addressbook default --every 5m
```

## Gotchas & Next Steps

- Always pass `--source <name>` even with `--stdin` — it tags provenance and dedup keys.
- Next: browse ingested objects ([list.md](list.md)) or inspect stalled jobs ([../dpkms/job.md](../dpkms/job.md)).
