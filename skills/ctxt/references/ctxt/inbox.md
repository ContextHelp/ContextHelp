# `ctxt inbox`

Inspect, promote, or discard deferred captures (`ctxt capture --inbox`) and failed/raw items.

## Execution Profile

| Subcommand | Side effect | Idempotency | Confirmation |
|---|---|---|---|
| `inbox list` | `read` | `yes` | — |
| `inbox triage <id>` | `write` | `no` (enqueues a job) | — |
| `inbox discard <id>` | `destructive` | `yes` | `--confirm=yes --confirm-token=<sha>` |
| `inbox clear` | `destructive` | `yes` | `--confirm=yes --confirm-token=<sha>` |

## Subcommands & Flags

```bash
ctxt inbox list [--limit <n>] [--offset <n>] [--before <rfc3339>] [--after <rfc3339>] [--pending] [--failed] [--raw]
ctxt inbox triage <id> [--pipeline <name>]
ctxt inbox discard <id> --confirm=yes --confirm-token=<sha>
ctxt inbox clear --confirm=yes --confirm-token=<sha>
```

### `inbox list` flags

| Flag | Default | Purpose |
|---|---|---|
| `--limit <n>` | `50` | Maximum results |
| `--offset <n>` | `0` | Pagination offset |
| `--before <rfc3339>` | `""` | Created before timestamp |
| `--after <rfc3339>` | `""` | Created after timestamp |
| `--pending` | `false` | Show only pending/running jobs |
| `--failed` | `false` | Show only failed jobs |
| `--raw` | `false` | Show only unenriched raw objects |

### `inbox triage` flags

| Flag | Default | Purpose |
|---|---|---|
| `--pipeline <name>` | `""` | Pipeline to run (default: auto-detect) |

## Agent Recipes

```bash
# Inspect pending and failed items as JSON
ctxt inbox list --failed --format json
ctxt inbox list --raw --limit 20 --format json

# Promote an inbox item to active and enqueue enrichment
ctxt inbox triage obj_12345 --pipeline text.long

# Discard noise (run once to obtain token, then confirm)
ctxt inbox discard obj_99999 --confirm=yes --confirm-token=<sha>
```

## Gotchas & Next Steps

- `inbox clear` discards every inbox item; inspect with `inbox list` before clearing.
- Next: track the triaged job ([../dpkms/job.md](../dpkms/job.md)) or inspect the promoted object ([show.md](show.md)).
