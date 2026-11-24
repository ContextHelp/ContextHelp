# `ctxt cursor`

Manage named per-viewer cursors for incremental `ctxt list --cursor <name> --advance` retrieval.

## Execution Profile

| Subcommand | Side effect | Idempotency | Confirmation |
|---|---|---|---|
| `cursor list` | `read` | `yes` | — |
| `cursor show <name>` | `read` | `yes` | — |
| `cursor reset <name>` | `write-local` | `yes` | — |
| `cursor set <name> --to <ts>` | `write-local` | `yes` | — |
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
| `--to <ts>` | `set` | `""` | Target timestamp: RFC3339 (`2026-04-28T14:22:11Z`) or signed duration (e.g. `-24h`) |

## Agent Recipes

```bash
# Poll only newly arrived mentions since last run, then advance cursor
ctxt list --mention @project.alpha --cursor daily-mentions --advance --format json

# Inspect cursor timestamp and saved query snapshot
ctxt cursor show daily-mentions --format json

# Rewind cursor by 24 hours to reprocess yesterday's window
ctxt cursor set daily-mentions --to -24h

# Full replay from epoch 0
ctxt cursor reset daily-mentions && ctxt list --cursor daily-mentions --advance --format json
```

## Gotchas & Next Steps

- Do not pair `--cursor` with `--q` on `ctxt list`; timestamp gating does not compose with arbitrary RSQL predicates.
- Next: incremental querying ([list.md](list.md)).
