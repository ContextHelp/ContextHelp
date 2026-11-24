# `ctxt edit`

Modify metadata fields on an existing knowledge object by ID.

- **Use `edit`** for targeted metadata updates (title, summary, tags, hints, mentions, decisions, subtype).
- **Use `reprocess`** to re-run AI enrichment steps instead of manually editing ([reprocess.md](reprocess.md)).

## Execution Profile

- **Side effect**: `write`
- **Idempotency**: `yes` (replaces specified fields with provided values)

## Usage

```bash
ctxt edit <id> [flags]
```

## Key Flags

| Flag | Default | Purpose |
|---|---|---|
| `--title <text>` | `""` | Update title |
| `--summary <text>` | `""` | Update summary |
| `--tags <csv>` | `""` | **Replace** tags (comma-separated) |
| `--hints <val>` | `""` | **Replace** hints |
| `--mention <val>` | `""` | **Replace** mentions |
| `--decisions <json>` | `""` | **Replace** decisions (JSON array/payload) |
| `--subtype <val>` | `""` | Update subtype |

## Agent Recipes

```bash
# Inspect current tags first (edit replaces, does not append)
ctxt show obj_12345678 --format json

# Update title and replace tags
ctxt edit obj_12345678 --title "Rate limit policy v2" --tags api,limits,decision

# Verify updated metadata
ctxt show obj_12345678 --format json
```

## Gotchas & Next Steps

- `--tags`, `--hints`, `--mention`, and `--decisions` **replace** the existing field values; inspect with `ctxt show <id> --format json` before editing if you want to preserve prior entries.
- Note the plural flag names here (`--tags`, `--hints`), distinct from `--tagged`/`--hint` on `list`/`capture`.
- Next: verify changes ([show.md](show.md)).
