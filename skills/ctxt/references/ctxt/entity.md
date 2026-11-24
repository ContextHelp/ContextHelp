# `ctxt entity`

Query, inspect, search, and trace backlinks for canonical `@namespace.slug` entities in the knowledge graph.

- **Use `entity`** for canonical IDs, aliases, relationships, and backlinks.
- **Use `page`** for synthesized narrative wiki pages about an entity ([page.md](page.md)).

## Execution Profile

- **Side effect**: `read` (all subcommands)
- **Idempotency**: `yes`

## Subcommands & Flags

```bash
ctxt entity list [--namespace <ns>] [--limit <n>]
ctxt entity show <slug>
ctxt entity search <query>
ctxt entity backlink <slug>
```

| Flag | Subcommand | Default | Purpose |
|---|---|---|---|
| `--limit <n>` | `list` | `50` | Maximum results |
| `--namespace <ns>` | `list` | `""` | Filter entities by namespace (e.g. `project`, `stripe`) |

## Agent Recipes

```bash
# List entities in a namespace as JSON
ctxt entity list --namespace project --limit 50 --format json

# Inspect aliases and metadata for one entity
ctxt entity show stripe.api --format json

# Find all knowledge objects mentioning an entity
ctxt entity backlink stripe.api --format json

# Fuzzy-find entity slugs before writing a --mention filter
ctxt entity search "payment" --format json
```

## Next Steps

- Filter objects by mention: [list.md](list.md) · Read synthesized entity page: [page.md](page.md) · Sync external entity definitions: [registry.md](registry.md)
