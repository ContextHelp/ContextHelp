# `ctxt list`

Deterministically query, filter, sort, paginate, and incrementally scan knowledge objects across local storage and synced registries.

- **Use `list`** for reproducible agent queries, RSQL expressions (`--q`), tag/mention/date windows, and incremental cursor scans.
- **Use `find`** for natural-language semantic or hybrid search ([find.md](find.md)).

## Execution Profile

- **Side effect**: `read` (note: `--cursor <name> --advance` updates local cursor state)
- **Idempotency**: `yes` (without `--advance`)

## Usage

```bash
ctxt list [flags]
```

## Key Flags

### Object & taxonomy filters

| Flag | Default | Purpose |
|---|---|---|
| `--type <t>` | `""` | Filter by object type (`text`, `url`, `image`, `audio`, `video`, `feed`) |
| `--subtype <st>` | `""` | Filter by object subtype |
| `--tagged <csv>` | `""` | Filter by tags (comma-separated) |
| `--hint <csv>` | `""` | Filter by hints (comma-separated) |
| `--mention <@slug>` | `""` | Filter by entity mention |
| `--pipeline <name>` | `""` | Filter by pipeline name |
| `--orig-lang <code>` | `""` | Filter by original language code |
| `--status <st>` | `""` | `active\|inbox\|discarded\|raw\|all` |
| `--after <iso>` | `""` | Created after ISO date |
| `--before <iso>` | `""` | Created before ISO date |
| `--q <rsql>` | `""` | Structured RSQL query — see [query-language.md](query-language.md) |

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

### Display, pagination & cursors

| Flag | Default | Purpose |
|---|---|---|
| `--limit <n>` | `50` | Maximum results |
| `--start <n>` | `0` | Pagination offset |
| `--sort <key>` | `recent` | `recent\|view\|match\|weight\|score` |
| `--dir <order>` | `desc` | `asc\|desc` |
| `--no-track` | `false` | Skip match tracking |
| `--cursor <name>` | `""` | Return items added after named cursor's `last_seen_at` |
| `--advance` | `false` | Advance cursor after successful list (requires `--cursor`) |

## Agent Recipes

```bash
# Deterministic tag + date window as JSON
ctxt list --type url --tagged ux,onboarding --after 2026-01-01 --limit 25 --format json

# Structured RSQL query
ctxt list --q "type==url;tag=in=(ux,design)" --limit 30 --format json

# Incremental poll using a named cursor
ctxt list --mention @project.alpha --cursor alpha-digest --advance --format json

# Facet breakdown filtered by source and person
ctxt list --source-type slack --person alice-chen --facets
```

## Gotchas & Next Steps

- `--cursor` combined with `--q` does **not** gate by `created_at` (emits a stderr warning).
- Avoid duplicating the same tag filter in both `--tagged` and `--q` — both compound and can narrow unexpectedly.
- Next: RSQL grammar ([query-language.md](query-language.md)), cursor management ([cursor.md](cursor.md)), inspect object ([show.md](show.md)), or compose ([compose.md](compose.md)).
