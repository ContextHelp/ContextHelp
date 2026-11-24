# Query Language (`--q` RSQL)

Exact grammar and field matrix supported by `ctxt list --q "<expr>"` (and `ctxt shell` direct RSQL input).

## Syntax

| Element | Syntax | Meaning |
|---|---|---|
| Logical AND | `;` | `expr1;expr2` |
| Logical OR | `,` | `expr1,expr2` |
| Grouping | `( ... )` | Explicit precedence |
| Equality / inequality | `==`, `!=` | Exact match / negation |
| Ordering | `>`, `>=`, `<`, `<=` | Date/string comparison |
| Set membership | `=in=(a,b)`, `=out=(a,b)` | Match any / exclude all |
| Quoted value | `"phrase with spaces"` | Required when value has spaces or punctuation |

## Supported Fields & Operators

Every unknown field name is rejected with an error.

| Field | Allowed operators | Notes |
|---|---|---|
| `type` | `==`, `!=`, `=in=`, `=out=` | Object type (`text`, `url`, `image`, `audio`, `video`, `feed`) |
| `subtype` | `==`, `!=`, `=in=`, `=out=` | Object subtype |
| `pipeline` | `==`, `!=`, `=in=`, `=out=` | Pipeline name (e.g. `text.long`) |
| `source` | `==`, `!=`, `=in=`, `=out=` | Source identifier |
| `created_at` | `==`, `!=`, `>`, `>=`, `<`, `<=` | ISO8601 timestamp/date |
| `updated_at` | `==`, `!=`, `>`, `>=`, `<`, `<=` | ISO8601 timestamp/date |
| `tag` | `==`, `!=`, `=in=(a,b)` | Matches tag label (`=out=` is **not** supported on `tag`) |
| `mention` | `==` only | Entity target (`mention==project.checkout` — **not** `mention:`) |
| `related` | `==` only | 1-hop shared-mention neighborhood (`related==@arch.decision`) |
| `similar` | `==` only | SQLite FTS5 `MATCH` only (**rejected on Postgres** — use `ctxt find`) |

## Verified Examples

```bash
# URLs tagged ux OR design
ctxt list --q "type==url;tag=in=(ux,design)" --format json

# Text lessons/decisions mentioning a project, created after a date
ctxt list --q "type==text;tag=in=(lesson,decision);mention==project.checkout;created_at>=2026-01-01" --format json

# Exclude raw/draft pipelines, match either of two types
ctxt list --q "(type==url,type==text);pipeline!=raw" --format json

# 1-hop graph neighbors sharing mention targets with an entity
ctxt list --q "related==@arch.decision" --format json

# SQLite-only FTS filter inside RSQL
ctxt list --q 'type==text;similar=="rate limit"' --format json
```

## Gotchas

- **Do not use Lucene syntax** (`AND`, `OR`, `tag:foo`, `mention:foo`) — the parser is strict RSQL (`;`, `,`, `==`, `=in=(...)`).
- For metadata facets (`meta-type`, `topic`, `person`, `dates_mentioned`, `source_type`), use dedicated flags on `ctxt list` or `ctxt find` rather than `--q`.
- Do not combine `--cursor` with `--q` (cursor timestamp gating is skipped).

## Next Steps

- Full `list` flags: [list.md](list.md) · Semantic/hybrid search: [find.md](find.md) · Agent memory presets: [../workflows/memory-taxonomy.md](../workflows/memory-taxonomy.md)
