# `ctxt import`

Bulk-import knowledge from external apps, export archives, and cloud drives (19 connectors + generic batch).

- **Use `import`** for one-shot or periodic bulk migrations from specific external platforms or CSV/JSONL/Markdown dumps.
- **Use `ingest`** for live external CLI adapters (`cardamum`, `himalaya`, custom JSON stdin streams) ([../ingest.md](../ingest.md)).

## Execution Profile

- **Side effect**: `write` (enqueues jobs onto `dpkms`; requires `dpkms serve`)
- **Idempotency**: `conditional` — connectors dedup where source IDs exist; preview first with global `--dry-run` where supported.

## Connector Router

| Source category | Subcommands | Reference |
|---|---|---|
| Generic files & directories (`jsonl`, `csv`, `tsv`, `markdown`) | `batch`, `batch status` | *(below)* |
| Browser & web bookmarks | `chrome`, `edge`, `firefox`, `safari`, `pinboard`, `raindrop` | [bookmarks.md](bookmarks.md) |
| Note-taking & PKM vaults | `obsidian`, `logseq`, `notion`, `evernote` | [notes-pkm.md](notes-pkm.md) |
| Email, chat & social exports | `email`, `slack`, `discord`, `twitter`, `linkedin` | [comms.md](comms.md) |
| Cloud drives & GitHub | `gdrive`, `dropbox`, `onedrive`, `github` | [cloud-code.md](cloud-code.md) |

## Shared Connector Flags

Nearly every connector accepts:
- `--server <url>` — `dpkms` server URL (default `http://localhost:8080`)
- `--pipeline <name>` — pipeline override for enqueued jobs
- `--max-items <n>` — cap imported items (`0` = all)
- `--since <date>` — filter by RFC3339 or `YYYY-MM-DD` (where supported)

## `ctxt import batch` & `batch status`

Import generic structured files (`jsonl`, `csv`, `tsv`) or a directory of Markdown files.

```bash
ctxt import batch [--file <path> | --dir <path>] [flags]
ctxt import batch status <batch-id> [--server <url>]
```

| Flag | Default | Purpose |
|---|---|---|
| `--file <path>` | `""` | Input file (`jsonl`, `csv`, `tsv`) |
| `--dir <path>` | `""` | Directory to scan for Markdown files |
| `--format <fmt>` | `""` | `jsonl\|csv\|tsv\|markdown` (auto-detected from extension) |
| `--dry-run` | `false` | Validate without importing |
| `--map-content <col>` | `""` | CSV/TSV column name for content |
| `--map-type <col>` | `""` | CSV/TSV column name for object type |
| `--map-tags <col>` | `""` | CSV/TSV column name for tags |
| `--server <url>` | `http://localhost:8080` | Target `dpkms` server URL |

### Recipes

```bash
# Dry-run validate a CSV before importing
ctxt import batch --file export.csv --map-content body --map-tags labels --dry-run

# Import a Markdown directory and check batch progress
ctxt import batch --dir ./notes --format json
ctxt import batch status <batch-id> --format json
```

## Gotchas & Next Steps

- Never pass OAuth/API tokens inline in shared logs when an env var exists (`GITHUB_TOKEN`, `NOTION_TOKEN`, `GDRIVE_ACCESS_TOKEN`, `DROPBOX_ACCESS_TOKEN`, `ONEDRIVE_TOKEN`, `PINBOARD_TOKEN`, `RAINDROP_TOKEN`).
- Next: verify imported objects ([../list.md](../list.md)), monitor jobs ([../../dpkms/job.md](../../dpkms/job.md)), or search ([../find.md](../find.md)).
