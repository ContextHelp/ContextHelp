# `ctxt list`

Deterministically query, filter, sort, paginate, change-poll, and cursor-scan knowledge objects across local storage and synced registries.

- **Use `list`** for reproducible agent queries, RSQL expressions (`--q`), `--tagged` filter expressions, change-order sync (`--changed-after`), and named cursors.
- **Use `find`** for natural-language semantic or hybrid search ([find.md](find.md)).

## Execution Profile

- **Side effect**: `read` (note: `--cursor <name> --advance` updates local cursor state)
- **Idempotency**: `yes` (without `--advance`)

## Usage

```bash
ctxt list [flags]
```

## Key Flags

### Object, taxonomy & time filters

| Flag | Default | Purpose |
|---|---|---|
| `--type <t>` | `""` | Filter by object type (`text`, `url`, `image`, `audio`, `video`, `feed`) |
| `--subtype <st>` | `""` | Filter by subtype |
| `--tagged <expr>` | `[]` | Tag filter (`+required,boost,-excluded`; repeatable, e.g. `--tagged +a,b,+c`) |
| `--mention <ref>` | `""` | Filter by mention (`@ns.slug` or `ctxt://` URI) |
| `--pipeline <name>` | `""` | Filter by pipeline name |
| `--status <st>` | `""` | `active\|inbox\|discarded\|raw\|all` |
| `--since <expr>` | `""` | Created on/after date or duration (e.g. `1w`, `2026-01-01`) |
| `--until <expr>` | `""` | Created on/before date or duration |
| `--q <rsql>` | `""` | Structured RSQL query — see [query-language.md](query-language.md) |

### Metadata facet filters

| Flag | Default | Purpose |
|---|---|---|
| `--meta-type <t>` | `""` | Filter by metadata type (`observation`, `task`, etc.) |
| `--topic <x>` | `""` | Filter by metadata topic |
| `--person <p>` | `""` | Filter by person in metadata |
| `--mentioned-since <expr>` | `""` | Filter by `dates_mentioned >=` |
| `--mentioned-until <expr>` | `""` | Filter by `dates_mentioned <=` |
| `--source-type <st>` | `""` | Filter by `source_type` |
| `--facets` | `false` | Show metadata type count breakdown |

### Pagination, change feed & cursors

| Flag | Default | Purpose |
|---|---|---|
| `--limit <n>` | `50` | Maximum results (`0` = every match) |
| `--start <n>` | `0` | Pagination offset |
| `--sort <key>` | `created` | `created` (synonym: `recent`) or `updated` |
| `--dir <order>` | `desc` | `asc\|desc` |
| `--changed-after <seq>` | `0` | Change-order feed: objects created/updated after sequence (`0` = start), oldest first; emits `next_changed_after` |
| `--cursor <name>` | `""` | Return items added after named cursor's `last_seen_at` |
| `--advance` | `false` | Advance cursor after successful list (requires `--cursor`) |

## Agent Recipes

```bash
# Deterministic tag + date window as JSON
ctxt list --type url --tagged +ux,onboarding --since 2026-01-01 --limit 25 --format json

# Structured RSQL query
ctxt list --q "type==url;tag=in=(ux,design)" --limit 30 --format json

# Poll incremental changes by change sequence
ctxt list --changed-after 0 --limit 100 --format json

# Poll using a named client cursor
ctxt list --mention @project.alpha --cursor alpha-digest --advance --format json
```

## Gotchas & Next Steps

- `--since`/`--until` replaced legacy `--after`/`--before`; use `--mentioned-since`/`--mentioned-until` for dates mentioned inside content.
- Avoid duplicating the same tag constraint in both `--tagged` and `--q` — both compound.
- Next: RSQL grammar ([query-language.md](query-language.md)), cursors ([cursor.md](cursor.md)), resolve ([resolve.md](resolve.md)), or compose ([compose.md](compose.md)).
