# `ctxt edit`

Modify metadata fields on an existing knowledge object by ID.

- **Use `edit`** for direct metadata updates (title, summary, tags, mentions, subtype).
- **Use `reprocess`** to re-run AI enrichment steps ([reprocess.md](reprocess.md)).

## Execution Profile

- **Side effect**: `write` (blocked if object is under an active hold, see [hold.md](hold.md))
- **Idempotency**: `yes` (replaces specified fields)

## Usage & Flags

```bash
ctxt edit <id> [flags]
```

| Flag | Default | Purpose |
|---|---|---|
| `--title <text>` | `""` | Replace summaries with this title |
| `--summary <text>` | `""` | Replace the first summary |
| `--tags <csv>` | `""` | **Replace** tags (comma-separated) |
| `--mention <@slug>` | `[]` | **Replace** mentions with `@namespace.slug`; repeatable, comma- or space-separated |
| `--subtype <val>` | `""` | Update subtype |

## Agent Recipes

```bash
# Inspect existing fields before replacing
ctxt show obj_12345678 --format json

# Replace title, tags, and mentions
ctxt edit obj_12345678 --title "Rate limit policy v2" --tags api,limits --mention @stripe.api,@team.platform
```

## Gotchas & Next Steps

- `--tags` and `--mention` **replace** existing values; inspect with `ctxt show` first to preserve prior items.
- Edits fail if the object is protected by a hold ([hold.md](hold.md)).
- Next: verify changes ([show.md](show.md)).
