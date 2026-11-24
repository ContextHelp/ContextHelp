# `ctxt show`

Display detailed information for a single knowledge object (or raw JSON payload).

- **Use `show`** for human inspection or `--raw` full object dump (by ID or clipboard).
- **Use `resolve`** for one-shot agent/script resolution of either `obj_<id>` or `@<slug>` with pipe-friendly Markdown body or JSON provenance envelope ([resolve.md](resolve.md)).

## Execution Profile

- **Side effect**: `read`
- **Idempotency**: `yes`

## Usage & Flags

```bash
ctxt show [id] [--raw]
```

If `[id]` is omitted, `show` checks the clipboard for an ID.

| Flag | Default | Purpose |
|---|---|---|
| `--raw` | `false` | Show raw object data as JSON |

## Agent Recipes

```bash
# Inspect full structured object
ctxt show obj_12345678 --format json

# Dump raw object payload
ctxt show obj_12345678 --raw
```

## Next Steps

- Resolve with provenance: [resolve.md](resolve.md) · Edit metadata: [edit.md](edit.md) · Schedule expiry: [expire.md](expire.md) · Place hold: [hold.md](hold.md)
