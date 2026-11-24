# `ctxt reprocess`

Re-run a specific AI enrichment step on an existing knowledge object without re-ingesting its source.

- **Use `reprocess`** for single-object re-enrichment after profile/schema/prompt updates.
- **Use `ctxt upgrade run`** for bulk corpus re-ingestion ([upgrade.md](upgrade.md)).

## Execution Profile

- **Side effect**: `write`
- **Idempotency**: `conditional`

## Usage & Flags

```bash
ctxt reprocess <id> [--step <structured_metadata|entity_extractor|tagger>]
```

| Flag | Default | Purpose |
|---|---|---|
| `--step <name>` | `structured_metadata` | Step: `structured_metadata\|entity_extractor\|tagger` |

## Agent Recipes

```bash
ctxt reprocess obj_12345678 --step entity_extractor && ctxt show obj_12345678 --format json
```

## Next Steps

- Verify updated fields: [show.md](show.md) · Evolve profile schema: [profile.md](profile.md)
