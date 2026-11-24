# `ctxt import` — Notes & PKM (`obsidian`, `logseq`, `notion`, `evernote`)

Import personal knowledge vaults and note workspaces. Use global `--server <url>` for non-default instances and global `--dry-run` to preview.

## Execution Profile

- **Side effect**: `write` (enqueues jobs onto `dpkms`)
- **Idempotency**: `conditional`

## `ctxt import obsidian`

| Flag | Default | Purpose |
|---|---|---|
| `--vault <dir>` | `""` | Path to Obsidian vault directory |
| `--since <date>` | `""` | Notes modified since RFC3339 or `YYYY-MM-DD` |
| `--max-items <n>` | `0` | Max notes (`0` = all) |
| `--pipeline <name>` | `""` | Pipeline override |

## `ctxt import logseq`

| Flag | Default | Purpose |
|---|---|---|
| `--graph <dir>` | `""` | Path to Logseq graph directory |
| `--kind <k>` | `""` | Filter by page kind: `journal\|page` (default: all) |
| `--since <date>` | `""` | Pages modified since RFC3339 or `YYYY-MM-DD` |
| `--max-items <n>` | `0` | Max pages (`0` = all) |
| `--pipeline <name>` | `""` | Pipeline override |

## `ctxt import notion`

| Flag | Default | Purpose |
|---|---|---|
| `--token <tok>` | `""` | Notion integration token (prefer `NOTION_TOKEN` env) |
| `--page-id <id>` | `[]` | Notion page ID (repeatable) |
| `--database-id <id>` | `[]` | Notion database ID (repeatable) |
| `--all-shared` | `false` | Import all pages shared with the integration |
| `--query <text>` | `""` | Optional query for `--all-shared` search |
| `--since <rfc3339>` | `""` | Pages edited since RFC3339 timestamp |
| `--max-items <n>` | `0` | Max pages (`0` = all) |
| `--pipeline <name>` | `""` | Pipeline override |

## `ctxt import evernote`

| Flag | Default | Purpose |
|---|---|---|
| `--file <path>` | `""` | Path to Evernote export (`.enex` or `.html`) |
| `--tagged <t>` | `[]` | Include notes with at least one of these tags (repeatable) |
| `--since <date>` | `""` | Notes modified since RFC3339 or `YYYY-MM-DD` |
| `--max-items <n>` | `0` | Max notes (`0` = all) |
| `--pipeline <name>` | `""` | Pipeline override |

## Agent Recipes

```bash
# Import recently edited Obsidian notes
ctxt import obsidian --vault ~/Notes/Vault --since 2026-01-01 --format json

# Import Logseq journals only
ctxt import logseq --graph ~/logseq --kind journal --since 2026-02-01

# Import a Notion database via env token
NOTION_TOKEN="$TOK" ctxt import notion --database-id 1234abcd --since 2026-01-01T00:00:00Z

# Import tagged Evernote ENEX export
ctxt import evernote --file notes.enex --tagged work --tagged architecture
```

## Next Steps

- Verify imports: [list.md](list.md) · Search: [find.md](find.md)
