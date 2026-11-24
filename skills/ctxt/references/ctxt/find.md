# `ctxt find`

Search knowledge using natural-language queries. Runs hybrid FTS + vector search with Reciprocal Rank Fusion (RRF) by default, and optionally opens/exports a search graph (`--graph`).

- **Use `find`** for semantic/fuzzy discovery, recall, and graph exploration.
- **Use `list`** for deterministic, reproducible filtering (`--q` RSQL, `--changed-after`, named cursors) ([list.md](list.md)).

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
| `--hybrid` | `false` | Hybrid FTS + vector RRF search (default from config) |
| `--semantic` | `false` | Vector-only search; `dpkms` embeds query with default model |
| `--fts` | `false` | Full-text search only |
| `--rrf-k <n>` | `0` | RRF `k` constant override |
| `--fts-weight <f>` | `0` | RRF FTS leg weight override |
| `--vector-weight <f>` | `0` | RRF vector leg weight override |
| `--fts-pool <n>` | `0` | FTS candidate pool size override |
| `--vector-pool <n>` | `0` | Vector candidate pool size override |
| `--min-score <f>` | `-1` | Minimum RRF score threshold (`-1` = config default) |
| `--explain` | `false` | Show per-signal score breakdown for each result |

### Taxonomy, time & metadata filters

| Flag | Default | Purpose |
|---|---|---|
| `--tagged <expr>` | `[]` | Tag filter (`+required,boost,-excluded`; repeatable) |
| `--since <expr>` | `""` | Objects created on/after date or duration (e.g. `7d`, `2026-01-01`) |
| `--until <expr>` | `""` | Objects created on/before date or duration |
| `--mentioned-since <expr>` | `""` | Filter by `dates_mentioned >=` |
| `--mentioned-until <expr>` | `""` | Filter by `dates_mentioned <=` |
| `--meta-type <t>` | `""` | Filter by metadata type (e.g. `observation`, `task`) |
| `--topic <x>` | `""` | Filter by metadata topic |
| `--person <p>` | `""` | Filter by person in metadata |
| `--source-type <st>` | `""` | Filter by `source_type` |
| `--facets` | `false` | Show metadata type count breakdown |

### Graph output & viewer (`--graph`)

| Flag | Purpose |
|---|---|
| `--graph` | Build/open search relationship graph (combine with `--format json` or `--no-browser`) |
| `--graph-max-nodes <n>` / `--graph-max-edges <n>` | Bound graph size |
| `--graph-similar` / `--graph-similar-threshold <f>` | Include similarity edges above threshold |
| `--no-browser` / `--graph-idle-timeout <dur>` | Viewer server controls when opening graph |

## Agent Recipes

```bash
# Hybrid search scoped to a focus profile and tag expression
ctxt find "authentication patterns" --profile engineer --tagged +auth,security --limit 15 --format json

# Vector-only search with score breakdown
ctxt find "rate limiting strategies" --semantic --explain --format json

# Keyword search filtered by creation window and metadata facet
ctxt find "checkout" --fts --since 30d --meta-type decision --topic payments --format json
```

## Gotchas & Next Steps

- `--since`/`--until` filter creation time; use `--mentioned-since`/`--mentioned-until` for dates mentioned inside content.
- For deterministic agent loops, prefer `ctxt list` first and fall back to `ctxt find` ([../workflows/agent-memory.md](../workflows/agent-memory.md)).
- Next: resolve body + provenance ([resolve.md](resolve.md)), inspect ([show.md](show.md)), or synthesize ([compose.md](compose.md)).
