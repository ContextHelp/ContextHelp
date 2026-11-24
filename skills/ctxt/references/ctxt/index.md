# `ctxt index`

View or rebuild the topic index summarizing categories across the knowledge base.

## Execution Profile

- **Side effect**: `write` (when `--refresh` is passed; read otherwise)
- **Idempotency**: `yes`

## Usage

```bash
ctxt index [topic] [flags]
```

## Key Flags

| Flag | Default | Purpose |
|---|---|---|
| `--refresh` | `false` | Regenerate the topic index from scratch |

## Agent Recipes

```bash
# View top-level topic categories as JSON
ctxt index --format json

# Drill into a specific topic
ctxt index authentication --format json

# Rebuild index after bulk imports, then view
ctxt index --refresh && ctxt index
```

## Next Steps

- Filter objects by topic facet: [list.md](list.md), [find.md](find.md)
