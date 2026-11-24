# `ctxt classify`

Classify raw text or an existing knowledge object using `c12n` classification signals.

- **Use `classify`** to run/inspect `c12n` classification signals on text or an object ID.
- **Use `reprocess`** to re-run LLM enrichment steps (`structured_metadata`, `entity_extractor`, `tagger`) ([reprocess.md](reprocess.md)).

## Execution Profile

- **Side effect**: `write` (persists signals when given an existing object ID)
- **Idempotency**: `conditional`

## Usage

```bash
ctxt classify <text|object_id> [flags]
```

## Key Flags

| Flag | Default | Purpose |
|---|---|---|
| `--pipeline <name>` | `""` | Pipeline name (reserved) |

## Agent Recipes

```bash
# Classify an existing object and inspect persisted signals
ctxt classify obj_12345678 --format json
ctxt show obj_12345678 --format json

# Classify ad-hoc text
ctxt classify "PostgreSQL migration decision for tenant isolation" --format json
```

## Next Steps

- Inspect persisted signals: [show.md](show.md) · Tune classification rules: [profile.md](profile.md)
