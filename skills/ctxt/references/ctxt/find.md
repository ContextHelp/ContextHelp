# `ctxt find`

Search knowledge using natural-language queries. Runs hybrid FTS + vector search with Reciprocal Rank Fusion (RRF) by default.

- **Use `find`** for semantic/fuzzy discovery and recall.
- **Use `list`** for deterministic, reproducible filtering (`--q` RSQL, `--tagged`, `--mention`, named cursors) ([list.md](list.md)).

## Execution Profile

- **Side effect**: `read`
- **Idempotency**: `yes`

## Usage

```bash
ctxt find [query] [flags]
```

If `[query]` is omitted, `find` falls back to clipboard text.

## Key Flags

### Search mode & ranking

| Flag | Default | Purpose |
|---|---|---|
| `--limit <n>` | `10` | Maximum results |
| `--hybrid` | `false` | Force hybrid FTS + vector RRF search (default from config) |
| `--semantic` | `false` | Vector-only search via embedding provider |
| `--fts` | `false` | Full-text search only (no embeddings) |
| `--rrf-k <n>` | `0` | RRF `k` constant override (`0` = config default) |
| `--fts-weight <f>` | `0` | RRF FTS leg weight override |
| `--vector-weight <f>` | `0` | RRF vector leg weight override |
| `--fts-pool <n>` | `0` | FTS candidate pool size override |
| `--vector-pool <n>` | `0` | Vector candidate pool size override |
| `--min-score <f>` | `-1` | Minimum RRF score threshold (`-1` = config default) |
| `--explain` | `false` | Show per-signal score breakdown for each result |

### Metadata facet filters

| Flag | Default | Purpose |
|---|---|---|
| `--meta-type <t>` | `""` | Filter by metadata type (e.g. `observation`, `task`) |
| `--topic <x>` | `""` | Filter by metadata topic |
| `--person <p>` | `""` | Filter by person in metadata |
| `--since <iso>` | `""` | Filter by `dates_mentioned >=` ISO date |
| `--until <iso>` | `""` | Filter by `dates_mentioned <=` ISO date |
| `--source-type <st>` | `""` | Filter by `source_type` |
| `--facets` | `false` | Show metadata type count breakdown |

## Agent Recipes

```bash
# Hybrid search scoped to a focus profile as JSON
ctxt find "authentication patterns" --profile engineer --limit 15 --format json

# Vector-only search with score breakdown for debugging weak hits
ctxt find "rate limiting strategies" --semantic --explain --format json

# Fast local keyword search filtered by metadata facet
ctxt find "checkout" --fts --meta-type decision --topic payments --format json

# Inspect facet distribution across matching objects
ctxt find "onboarding" --facets
```

## Gotchas & Next Steps

- `--since`/`--until` filter `dates_mentioned` inside metadata, not object creation time (use `ctxt list --after/--before` for creation timestamps).
- For deterministic, reproducible queries, prefer `ctxt list` first and fall back to `ctxt find`.
- Next: inspect a result ([show.md](show.md)), refine with structured queries ([list.md](list.md), [query-language.md](query-language.md)), or synthesize ([compose.md](compose.md)).
