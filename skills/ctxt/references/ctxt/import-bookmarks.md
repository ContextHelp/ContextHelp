# `ctxt import` — Bookmarks (`chrome`, `edge`, `firefox`, `safari`, `pinboard`, `raindrop`)

Import browser HTML bookmark exports and bookmarking services (`pinboard`, `raindrop`). Use global `--server <url>` for non-default instances and global `--dry-run` to preview.

## Execution Profile

- **Side effect**: `write` (enqueues jobs onto `dpkms`)
- **Idempotency**: `conditional`

## Browser HTML Exports (`chrome`, `edge`, `firefox`, `safari`)

```bash
ctxt import chrome  --file <bookmarks.html> [--max-items <n>] [--pipeline <name>]
ctxt import edge    --file <bookmarks.html> [--max-items <n>] [--pipeline <name>]
ctxt import firefox --file <bookmarks.html> [--max-items <n>] [--pipeline <name>]
ctxt import safari  --file <bookmarks.html> [--max-items <n>] [--pipeline <name>]
```

## `ctxt import pinboard`

| Flag | Default | Purpose |
|---|---|---|
| `--token <tok>` | `""` | Pinboard API token (prefer `PINBOARD_TOKEN` env) |
| `--file <path>` | `""` | Path to Pinboard JSON export file (offline alternative) |
| `--since <date>` | `""` | Bookmarks saved on/after RFC3339 or `YYYY-MM-DD` |
| `--tagged <t>` | `[]` | Require bookmarks to include all specified tags |
| `--max-items <n>` | `0` | Max bookmarks (`0` = all) |
| `--pipeline <name>` | `""` | Pipeline override |
| `--pinboard-base-url <url>` | `""` | API base URL override (dev/tests only) |

## `ctxt import raindrop`

| Flag | Default | Purpose |
|---|---|---|
| `--token <tok>` | `""` | Raindrop API token (prefer `RAINDROP_TOKEN` env) |
| `--collection-id <id>` | `[]` | Raindrop collection ID (repeatable) |
| `--all` | `false` | Import from all collections |
| `--query <text>` | `""` | Text filter over title/url/excerpt/note/tags/highlights |
| `--tagged <tag>` | `""` | Exact tag filter (case-insensitive) |
| `--since <date>` | `""` | Items updated since RFC3339 or `YYYY-MM-DD` |
| `--max-items <n>` | `0` | Max items (`0` = all) |
| `--pipeline <name>` | `""` | Pipeline override |

## Agent Recipes

```bash
# Import first 50 Chrome bookmarks
ctxt import chrome --file ~/Downloads/bookmarks.html --max-items 50 --format json

# Import Pinboard bookmarks via env token
PINBOARD_TOKEN="$TOK" ctxt import pinboard --since 2026-01-01 --tagged architecture,go

# Import all Raindrop collections matching a tag
RAINDROP_TOKEN="$TOK" ctxt import raindrop --all --tagged research --since 2026-01-01
```

## Next Steps

- Verify imports: [list.md](list.md) · Search: [find.md](find.md) · Live browser tabs/history: [capture.md](capture.md)
