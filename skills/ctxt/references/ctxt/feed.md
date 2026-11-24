# `ctxt feed`

Subscribe to, list, sync, and remove RSS/Atom feeds managed by `dpkms`. Use global `--server <url>` for non-default instances.

## Execution Profile

| Subcommand | Side effect | Confirmation |
|---|---|---|
| `feed list` | `read` | — |
| `feed add <url>` | `write` | — |
| `feed sync` | `write` (spawns sync job) | — |
| `feed delete <url-or-id>` | `destructive` | `--confirm=yes --confirm-token=<sha>` |

## Subcommands & Flags

```bash
ctxt feed add <url>
ctxt feed list [--status <active|paused|error>]
ctxt feed sync
ctxt feed delete <url-or-id> --confirm=yes --confirm-token=<sha>
```

| Flag | Subcommand | Default | Purpose |
|---|---|---|---|
| `--status <st>` | `list` | `""` | Filter by `active\|paused\|error` |

## Agent Recipes

```bash
# Subscribe and immediately sync
ctxt feed add https://hnrss.org/frontpage --format json
ctxt feed sync

# Check errored feeds
ctxt feed list --status error --format json

# Remove a feed subscription
ctxt feed delete https://hnrss.org/frontpage --confirm=yes --confirm-token=<sha>
```

## Next Steps

- Track spawned sync job: [job.md](job.md) · Browse synced articles: [list.md](list.md)
