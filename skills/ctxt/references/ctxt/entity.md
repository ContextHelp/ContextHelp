# `ctxt entity`

List, inspect, search, and trace backlinks for canonical `@namespace.slug` entities.

- **Use `entity`** for canonical IDs, aliases, and backlinks.
- **Use `resolve @<slug>`** for pipe-friendly entity body + provenance ([resolve.md](resolve.md)) or `page` for synthesized wiki pages ([page.md](page.md)).

## Execution Profile

- **Side effect**: `read` (all subcommands)
- **Idempotency**: `yes`

## Subcommands & Flags

```bash
ctxt entity list [--namespace <ns>] [--limit <n>]
ctxt entity show <slug>
ctxt entity search <query>
ctxt entity backlinks <slug>
```

| Flag | Subcommand | Default | Purpose |
|---|---|---|---|
| `--limit <n>` | `list` | `50` | Maximum results |
| `--namespace <ns>` | `list` | `""` | Filter by namespace (e.g. `project`, `stripe`) |

## Agent Recipes

```bash
ctxt entity list --namespace project --limit 50 --format json
ctxt entity show stripe.api --format json
ctxt entity backlinks stripe.api --format json
ctxt entity search "payment" --format json
```

## Next Steps

- Resolve entity body + provenance: [resolve.md](resolve.md) · Filter objects by mention: [list.md](list.md) · Synthesized page: [page.md](page.md)
