# `ctxt page`

List, view, and regenerate compiled entity pages (living wiki summaries built from sources mentioning an entity).

## Execution Profile

| Subcommand | Side effect | Idempotency |
|---|---|---|
| `page list` / `page show <slug>` | `read` | `yes` |
| `page refresh <slug>` | `write` | `yes` |

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
# List pages as JSON
ctxt page list --limit 50 --format json

# Regenerate after new captures, then read
ctxt page refresh stripe.api && ctxt page show stripe.api
```

## Next Steps

- Inspect raw entity metadata & backlinks: [entity.md](entity.md)
