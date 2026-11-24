# `ctxt show`

Display detailed information for a single knowledge object, raw JSON payload, or plugin-rendered output.

- **Use `show`** to inspect an object's content, tags, mentions, and metadata (replaces legacy `open`).
- **Use `export`** when passing a `--dest` output path hint to an output-generator plugin ([export.md](export.md)).

## Execution Profile

- **Side effect**: `read`
- **Idempotency**: `yes`

## Usage

```bash
ctxt show [id] [flags]
```

If `[id]` is omitted, `show` reads the object ID from the clipboard.

## Key Flags

| Flag | Default | Purpose |
|---|---|---|
| `--raw` | `false` | Emit raw object JSON (including full body/metadata) |

Global `--format`:
- `--format json` — structured JSON representation
- `--format <plugin-name>` — delegates rendering to a loaded output-generator plugin

## Agent Recipes

```bash
# Inspect full structured object before composing
ctxt show obj_12345678 --format json

# Dump raw object payload
ctxt show obj_12345678 --raw

# Render through a loaded output-generator plugin
ctxt show obj_12345678 --format markdown
```

## Gotchas & Next Steps

- Prefer `--format json` or `--raw` when extracting `mentions`, `tags`, or source provenance in scripts.
- Next: edit metadata ([edit.md](edit.md)), inspect links ([link.md](link.md)), reprocess enrichment ([reprocess.md](reprocess.md)), or compose ([compose.md](compose.md)).
