# `ctxt reprocess`

Re-run a specific AI enrichment step on an existing knowledge object without re-ingesting its source.

- **Use `reprocess`** after updating profile schemas, prompts, or models for one object.
- **Use `ctxt upgrade run`** for bulk corpus re-ingestion across many objects ([upgrade.md](upgrade.md)).

## Execution Profile

- **Side effect**: `write`
- **Idempotency**: `conditional` (LLM output may vary across runs)

## Usage

```bash
ctxt reprocess <id> [flags]
```

## Key Flags

| Flag | Default | Purpose |
|---|---|---|
| `--step <name>` | `structured_metadata` | Enrichment step: `structured_metadata\|entity_extractor\|tagger` |

## Agent Recipes

```bash
# Re-run structured metadata extraction
ctxt reprocess obj_12345678 --step structured_metadata

# Re-extract entities and confirm on the object
ctxt reprocess obj_12345678 --step entity_extractor && ctxt show obj_12345678 --format json
```

## Next Steps

- Confirm updated enrichment fields: [show.md](show.md) · Edit profile vocabulary: [profile.md](profile.md)
