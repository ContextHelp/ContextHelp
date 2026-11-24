# `ctxt export`

Export a single knowledge object as JSON.

- **Use `export`** for direct JSON export of one object.
- **Use `resolve`** for pipe-friendly Markdown body or body + provenance JSON ([resolve.md](resolve.md)).
- **Use `compose`** to synthesize multiple objects into a brief/plan/summary/draft ([compose.md](compose.md)).

## Execution Profile

- **Side effect**: `read`
- **Idempotency**: `yes`

## Usage

```bash
ctxt export <id> [-o|--output <file>]
```

No local flags beyond globals ([../../SKILL.md](../../SKILL.md)).

## Agent Recipes

```bash
# Export an object to stdout or a file
ctxt export obj_12345678
ctxt export obj_12345678 --output ./obj_12345678.json
```

## Next Steps

- Resolve with provenance: [resolve.md](resolve.md) · Inspect raw details: [show.md](show.md)
