# `dpkms job`

Inspect, read server logs, retry, and cancel async ingestion jobs in the `dpkms` queue.

- **Use `dpkms job`** when you need `job log <job-id>` or direct substrate inspection.
- **Use `ctxt job`** for client-side listing, status, retry, and cancel ([../ctxt/job.md](../ctxt/job.md)).

## Execution Profile

| Subcommand | Side effect | Idempotency |
|---|---|---|
| `job list` / `status` / `log` | `read` | `yes` |
| `job retry <job-id>` | `write` | `conditional` |
| `job cancel <job-id>` | `write` | `yes` |

## Subcommands & Flags

```bash
dpkms job list [--state <pending|running|completed|failed|cancelled>] [--limit <n>]
dpkms job status <job-id>
dpkms job log <job-id>
dpkms job retry <job-id>
dpkms job cancel <job-id>
```

| Flag | Subcommand | Default | Purpose |
|---|---|---|---|
| `--state <st>` | `list` | `""` | `pending\|running\|completed\|failed\|cancelled` |
| `--limit <n>` | `list` | `50` | Maximum jobs to return |

## Agent Recipes

```bash
dpkms job list --state failed --limit 20 --format json
dpkms job status <job-id> --format json
dpkms job log <job-id>
dpkms job retry <job-id>
```

## Next Steps

- Reap zombie jobs: [housekeeping.md](housekeeping.md)
