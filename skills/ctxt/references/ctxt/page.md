# `ctxt page`

List, read, and regenerate compiled entity pages (living wiki summaries built from sources mentioning an entity).

- **Use `page`** for synthesized narrative pages per entity.
- **Use `entity`** for raw entity metadata, aliases, and backlink lists ([entity.md](entity.md)).

## Execution Profile

| Subcommand | Side effect | Idempotency |
|---|---|---|
| `page list` | `read` | `yes` |
| `page show <entity_slug>` | `read` | `yes` |
| `page refresh <entity_slug>` | `write` | `yes` |

## Subcommands & Flags

```bash
ctxt page list [--limit <n>]
ctxt page show <entity_slug>
ctxt page refresh <entity_slug>
```

| Flag | Subcommand | Default | Purpose |
|---|---|---|---|
| `--limit <n>` | `list` | `50` | Maximum pages to list |

## Agent Recipes

```bash
# List existing entity pages as JSON
ctxt page list --limit 50 --format json

# Read an entity page
ctxt page show stripe.api

# Regenerate after ingesting new sources, then read
ctxt page refresh stripe.api && ctxt page show stripe.api
```

## Next Steps

- Inspect underlying entity & backlinks: [entity.md](entity.md)
