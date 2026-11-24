# `ctxt resolve`

One-shot ref resolver for agents, scripts, and editor integrations. Resolves a single knowledge object ID or entity slug/alias to its body (Markdown by default) or a JSON envelope with body + provenance.

- **Use `resolve`** when an agent needs clean pipe-friendly body text or structured provenance (`source`, `pipeline`, `content_hash`, registry URL/version) for `obj_<id>` or `@<slug>`.
- **Use `show`** for general object inspection or `--raw` storage fields ([show.md](show.md)).

## Execution Profile

- **Side effect**: `read`
- **Idempotency**: `yes`

## Usage

```bash
ctxt resolve <ref> [--format json]
```

Supported `<ref>` forms:
- `obj_<id>` — knowledge object by ID
- `@<slug>` or `<slug>` — entity by exact slug, falling back to exact alias

Supported `--format` values: default (`markdown`, body only) or `json` (envelope with `ref`, `kind`, `title`, `body`, `provenance`). No other format is accepted.

## Exit Codes

`0` success · `2` bad invocation/unsupported format · `3` not found · `5` missing/insufficient token (`read:objects`) · `6` `dpkms`-side failure · `70` unreachable endpoint.

## Agent Recipes

```bash
# Pipe-friendly Markdown body
ctxt resolve obj_12345678
ctxt resolve @ui.best-practice

# Structured body + provenance envelope for agents
ctxt resolve obj_12345678 --format json
```

## Gotchas & Next Steps

- Entities with content status `"thin"` or `"pending_pull"` return an empty body and emit a warning on stderr until content is pulled.
- Next: inspect entity backlinks ([entity.md](entity.md)) or browse related items ([link.md](link.md), [list.md](list.md)).
