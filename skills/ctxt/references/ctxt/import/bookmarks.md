# `ctxt import` — Bookmarks (`chrome`, `edge`, `firefox`, `safari`, `pinboard`, `raindrop`)

Import browser HTML bookmark exports and bookmarking services. See [index.md](index.md) for shared execution profile and `--server`/`--pipeline`/`--max-items`.

## Browser HTML Exports (`chrome`, `edge`, `firefox`, `safari`)

All four share the exact same interface:

```bash
ctxt import chrome  --file <bookmarks.html> [--max-items <n>] [--pipeline <name>] [--server <url>]
ctxt import edge    --file <bookmarks.html> [--max-items <n>] [--pipeline <name>] [--server <url>]
ctxt import firefox --file <bookmarks.html> [--max-items <n>] [--pipeline <name>] [--server <url>]
ctxt import safari  --file <bookmarks.html> [--max-items <n>] [--pipeline <name>] [--server <url>]
```

## `ctxt import pinboard`

Import from the Pinboard API or a Pinboard JSON export file.

| Flag | Default | Purpose |
|---|---|---|
| `--token <tok>` | `""` | Pinboard API token (prefer `PINBOARD_TOKEN` env) |
| `--file <path>` | `""` | Path to Pinboard JSON export file (offline alternative to API) |
| `--since <date>` | `""` | Bookmarks saved on/after RFC3339 or `YYYY-MM-DD` |
| `--tagged <t>` | `[]` | Require bookmarks to include all specified tags |
| `--max-items <n>` | `0` | Max bookmarks (`0` = all) |
| `--pipeline <name>` | `""` | Pipeline override |
| `--server <url>` | `http://localhost:8080` | Target `dpkms` URL |
| `--pinboard-base-url <url>` | `""` | API base URL override (dev/tests only) |

## `ctxt import raindrop`

Import bookmarks and highlights from Raindrop.io.

| Flag | Default | Purpose |
|---|---|---|
| `--token <tok>` | `""` | Raindrop API token (prefer `RAINDROP_TOKEN` env) |
| `--collection-id <id>` | `[]` | Raindrop collection ID to import (repeatable) |
| `--all` | `false` | Import from all collections |
| `--query <text>` | `""` | Text filter across title/url/excerpt/note/tags/highlights |
| `--tagged <tag>` | `""` | Exact tag filter (case-insensitive) |
| `--since <date>` | `""` | Items updated since RFC3339 or `YYYY-MM-DD` |
| `--max-items <n>` | `0` | Max items (`0` = all) |
| `--pipeline <name>` | `import.raindrop` | Pipeline override |
| `--server <url>` | `http://localhost:8080` | Target `dpkms` URL |

## Agent Recipes

```bash
# Import first 50 Chrome bookmarks
ctxt import chrome --file ~/Downloads/bookmarks.html --max-items 50 --format json

# Import Pinboard bookmarks since a date via env token
PINBOARD_TOKEN="$TOK" ctxt import pinboard --since 2026-01-01 --tagged architecture,go

# Import all Raindrop collections matching a tag
RAINDROP_TOKEN="$TOK" ctxt import raindrop --all --tagged research --since 2026-01-01
```

## Next Steps

- Verify imports: [../list.md](../list.md) · Search: [../find.md](../find.md) · Back to router: [index.md](index.md)
