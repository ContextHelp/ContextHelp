# `ctxt inbox`

Inspect, promote (`triage`), or discard deferred captures (`ctxt capture --inbox`) and pending/failed/raw items.

## Execution Profile

| Subcommand | Side effect | Idempotency | Confirmation |
|---|---|---|---|
| `inbox list` | `read` | `yes` | — |
| `inbox triage <id>` | `write` | `no` | — |
| `inbox discard <id>` | `destructive` | `yes` | `--confirm=yes --confirm-token=<sha>` |
| `inbox clear` | `destructive` | `yes` | `--confirm=yes --confirm-token=<sha>` |

## Subcommands & Flags

```bash
ctxt inbox list [--limit <n>] [--offset <n>] [--before <rfc3339>] [--after <rfc3339>] [--pending] [--failed] [--raw]
ctxt inbox triage <id> [--pipeline <name>]
ctxt inbox discard <id> --confirm=yes --confirm-token=<sha>
ctxt inbox clear --confirm=yes --confirm-token=<sha>
```

| Flag | Subcommand | Default | Purpose |
|---|---|---|---|
| `--limit <n>` | `list` | `50` | Maximum results |
| `--offset <n>` | `list` | `0` | Pagination offset |
| `--before <rfc3339>` | `list` | `""` | Created before timestamp |
| `--after <rfc3339>` | `list` | `""` | Created after timestamp |
| `--pending` | `list` | `false` | Show only pending/running jobs |
| `--failed` | `list` | `false` | Show only failed jobs |
| `--raw` | `list` | `false` | Show only unenriched raw objects |
| `--pipeline <name>` | `triage` | `""` | Pipeline to run (default: auto-detect) |

## Agent Recipes

```bash
# Inspect failed or raw inbox items
ctxt inbox list --failed --format json
ctxt inbox list --raw --limit 20 --format json

# Promote an inbox item to active and enqueue enrichment
ctxt inbox triage obj_12345 --pipeline text.long

# Discard a single item
ctxt inbox discard obj_99999 --confirm=yes --confirm-token=<sha>
```

## Next Steps

- Track triaged job: [job.md](job.md) · Inspect promoted object: [show.md](show.md)
