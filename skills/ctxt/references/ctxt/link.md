# `ctxt link`

Create, inspect, traverse, and delete explicit typed links (edges) between knowledge objects.

## Execution Profile

| Subcommand | Side effect | Confirmation |
|---|---|---|
| `link list <object_id>` | `read` | — |
| `link create <source_id> <target_id>` | `write-shared` | — |
| `link delete <source_id> <target_id>` | `destructive-shared` | `--confirm=yes --confirm-token=<sha>` |

## Subcommands & Flags

```bash
ctxt link create <source_id> <target_id> -t <link-type> [--context <reason>]
ctxt link list <object_id> [-t <link-type>] [--follow <depth>]
ctxt link delete <source_id> <target_id> --confirm=yes --confirm-token=<sha>
```

| Flag | Subcommand(s) | Default | Purpose |
|---|---|---|---|
| `-t`, `--type <kind>` | `create`, `list` | `""` | Link type (required on `create`; filter on `list`) |
| `--context <text>` | `create` | `""` | Optional rationale/context for the link |
| `--follow <depth>` | `list` | `0` | Depth-limited graph traversal (max `3`) |

## Agent Recipes

```bash
# Link a decision record to its supporting benchmark object
ctxt link create obj_dec1 obj_bench9 --type supports --context "p99 latency benchmark"

# Traverse linked neighborhood up to 2 hops as JSON
ctxt link list obj_dec1 --follow 2 --format json

# Remove links between two objects
ctxt link delete obj_dec1 obj_bench9 --confirm=yes --confirm-token=<sha>
```

## Gotchas & Next Steps

- `link create` writes forward and reverse edges; verify with `ctxt link list <source_id>`.
- For entity-based co-mention graphs rather than explicit object links, use `ctxt entity backlink` ([entity.md](entity.md)) or `ctxt list --q "related==@..."` ([query-language.md](query-language.md)).
