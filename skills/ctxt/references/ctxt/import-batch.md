# `ctxt import batch`

Bulk-import generic structured files (`jsonl`, `csv`, `tsv`) or a directory of Markdown files, and check batch import status.

- **Use `import batch`** for generic tabular/JSONL/Markdown corpora.
- For platform-specific connectors, see [import-bookmarks.md](import-bookmarks.md), [import-notes-pkm.md](import-notes-pkm.md), [import-comms.md](import-comms.md), [import-cloud-code.md](import-cloud-code.md).

## Execution Profile

- **Side effect**: `write` (`batch`) / `read` (`batch status`)
- **Idempotency**: `conditional` — validate first with `--dry-run`.

## Usage & Flags

```bash
ctxt import batch [--file <path> | --dir <path>] [flags]
ctxt import batch status <batch-id>
```

| Flag | Default | Purpose |
|---|---|---|
| `--file <path>` | `""` | Path to import file (`jsonl`, `csv`, `tsv`) |
| `--dir <path>` | `""` | Directory to scan for Markdown files |
| `--format <fmt>` | `""` | File format: `jsonl\|csv\|tsv\|markdown` (auto-detected from extension) |
| `--dry-run` | `false` | Validate without importing (sends `dry_run=true`) |
| `--map-content <col>` | `""` | CSV/TSV column name to use as content |
| `--map-type <col>` | `""` | CSV/TSV column name to use as type |
| `--map-tags <col>` | `""` | CSV/TSV column name to use as tags |

Use global `--server <url>` to target a non-default `dpkms` instance.

## Agent Recipes

```bash
# Dry-run validate a CSV before importing
ctxt import batch --file export.csv --map-content body --map-tags labels --dry-run

# Import a Markdown directory and poll batch progress
ctxt import batch --dir ./notes --format markdown
ctxt import batch status <batch-id> --format json
```

## Next Steps

- Verify imports: [list.md](list.md) · Monitor jobs: [job.md](job.md) · Search: [find.md](find.md)
