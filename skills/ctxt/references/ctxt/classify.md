# `ctxt classify`

Classify raw text or an existing knowledge object using `c12n` signals.

- **Use `classify`** for `c12n` signal classification.
- **Use `reprocess`** to re-run LLM enrichment steps ([reprocess.md](reprocess.md)).

## Execution Profile

- **Side effect**: `write` (persists signals when given an object ID)
- **Idempotency**: `conditional`

## Usage & Flags

```bash
ctxt classify <text|object_id> [--pipeline <name>]
```

| Flag | Default | Purpose |
|---|---|---|
| `--pipeline <name>` | `""` | Pipeline name (reserved) |

## Agent Recipes

```bash
ctxt classify obj_12345678 --format json
ctxt classify "PostgreSQL tenant isolation decision" --format json
```

## Next Steps

- Inspect persisted signals: [show.md](show.md) · Tune classification rules: [profile.md](profile.md)
