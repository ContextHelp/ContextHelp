# `ctxt cursor`

Manage named client-side cursors for incremental `ctxt list --cursor <name> --advance` retrieval.

## Execution Profile

| Subcommand | Side effect | Idempotency | Confirmation |
|---|---|---|---|
| `cursor list` / `show <name>` | `read` | `yes` | — |
| `cursor reset <name>` / `set <name>` | `write-local` | `yes` | — |
| `cursor delete <name>` | `destructive-local` | `yes` | `--confirm=yes --confirm-token=<sha>` |

## Subcommands & Flags

```bash
ctxt cursor list
ctxt cursor show <name>
ctxt cursor reset <name>                                        # rewind to epoch 0
ctxt cursor set <name> --to <rfc3339-or-signed-duration>
ctxt cursor delete <name> --confirm=yes --confirm-token=<sha>
```

| Flag | Subcommand | Default | Purpose |
|---|---|---|---|
| `--to <ts>` | `set` | `""` | Target timestamp: RFC3339 (`2026-04-28T14:22:11Z`) or signed duration (`-7d`, `+1h`) |

## Agent Recipes

```bash
# Poll only newly arrived mentions since last run, then advance cursor
ctxt list --mention @project.alpha --cursor daily-mentions --advance --format json

# Rewind cursor by 24 hours and inspect
ctxt cursor set daily-mentions --to -24h && ctxt cursor show daily-mentions --format json
```

## Next Steps

- Incremental listing & change sequences (`--changed-after`): [list.md](list.md)
