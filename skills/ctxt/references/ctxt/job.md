# `ctxt job`

Follow, retry, and cancel async ingestion jobs on the active `dpkms` instance.

- **Use `ctxt job`** for client-side job status, listing, retry, and cancellation.
- **Use `dpkms job log <job-id>`** when you need detailed server-side execution logs ([../dpkms/job.md](../dpkms/job.md)).

## Execution Profile

| Subcommand | Side effect | Idempotency | Confirmation |
|---|---|---|---|
| `job list` / `status` | `read` | `yes` | — |
| `job retry <job-id>` | `write` | `conditional` | — |
| `job cancel <job-id>` | `destructive` | `yes` | `--confirm=yes --confirm-token=<sha>` |

## Subcommands & Flags

```bash
ctxt job list [--state <st>] [--limit <n>] [--offset <n>]
ctxt job status <job-id>
ctxt job retry <job-id>
ctxt job cancel <job-id> --confirm=yes --confirm-token=<sha>
```

| Flag | Subcommand | Default | Purpose |
|---|---|---|---|
| `--state <st>` | `list` | `""` | Filter by state (`pending\|running\|completed\|failed\|cancelled`) |
| `--limit <n>` | `list` | `50` | Maximum jobs to list (newest first) |
| `--offset <n>` | `list` | `0` | Jobs to skip |

## Agent Recipes

```bash
# Confirm no jobs are still running before composing
ctxt job list --state running --format json

# Check one job's state, timing, retries, and error
ctxt job status <job-id> --format json

# Retry a failed job
ctxt job retry <job-id>
```

## Next Steps

- View detailed job logs: [../dpkms/job.md](../dpkms/job.md) · Compose once complete: [compose.md](compose.md)
