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
| `type`, `subtype`, `pipeline`, `source`, `source_key` | `==`, `!=`, `=in=`, `=out=` | Direct object columns (`source_key` matches external identity) |
| `profile` | `==`, `!=`, `=in=`, `=out=` | Alias for `profile_id` |
| `created_at`, `updated_at` | `==`, `!=`, `>`, `>=`, `<`, `<=` | ISO8601 timestamp/date |
| `tag` | `==`, `!=`, `=in=(a,b)` | Matches tag label (`=out=` is **not** supported on `tag`) |
| `class` | `==`, `!=`, `=in=(a,b)`, `=out=(a,b)` | JSON array membership over `objects.classes` |
| `metadata.<key>` | `==`, `!=`, `>`, `>=`, `<`, `<=`, `=in=`, `=out=` | JSON path into `objects.metadata` (nested keys dotted, e.g. `metadata.a.b`) |
| `mention` | `==` only | Entity target (`mention==project.checkout` — **not** `mention:`) |
| `related` | `==` only | 1-hop shared-mention neighborhood (`related==@arch.decision`) |
| `similar` | `==` only | Full-text match (SQLite FTS5 `MATCH`; Postgres `websearch_to_tsquery`) |

## Verified Examples

```bash
# URLs tagged ux OR design
ctxt list --q "type==url;tag=in=(ux,design)" --format json

# Filter by metadata key and profile
ctxt list --q "profile==engineering;metadata.severity==p1" --format json

# Text lessons/decisions mentioning a project, created after a date
ctxt list --q "type==text;tag=in=(lesson,decision);mention==project.checkout;created_at>=2026-01-01" --format json

# Look up objects by external source_key
ctxt list --q "source_key==youtube:abc123" --format json

# 1-hop graph neighbors sharing mention targets
ctxt list --q "related==@arch.decision" --format json
```

## Gotchas

- **Do not use Lucene syntax** (`AND`, `OR`, `tag:foo`, `mention:foo`) — the parser is strict RSQL (`;`, `,`, `==`, `=in=(...)`).
- For scoped/shared viewers, `mention` and `related` edge subqueries enforce viewer visibility on both sides automatically.

## Next Steps

- Full `list` flags: [list.md](list.md) · Semantic/hybrid search: [find.md](find.md) · Agent memory presets: [../workflows/memory-taxonomy.md](../workflows/memory-taxonomy.md)
