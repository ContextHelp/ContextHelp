# `ctxt index`

View or rebuild the topic index summarizing categories across the knowledge base.

## Execution Profile

- **Side effect**: `write` (with `--refresh`; `read` otherwise)
- **Idempotency**: `yes`

## Usage & Flags

```bash
ctxt index [topic] [--refresh]
```

| Flag | Default | Purpose |
|---|---|---|
| `--refresh` | `false` | Regenerate the index from scratch |

## Agent Recipes

```bash
ctxt index --format json
ctxt index --refresh && ctxt index
```

## Next Steps

- Filter objects by topic facet: [list.md](list.md), [find.md](find.md)
