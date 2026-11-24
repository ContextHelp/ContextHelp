# `dpkms job`

Inspect, trace logs, retry, and cancel async ingestion jobs in the `dpkms` queue. Replaces deprecated `ctxt job`.

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
| `--state <st>` | `list` | `""` | Filter by `pending\|running\|completed\|failed\|cancelled` |
| `--limit <n>` | `list` | `50` | Maximum jobs to return |

## Agent Recipes

```bash
# Check for any running or failed jobs before composing
dpkms job list --state running --format json
dpkms job list --state failed --limit 20 --format json

# Inspect a specific job's status and logs
dpkms job status <job-id> --format json
dpkms job log <job-id>

# Retry a failed job after fixing provider credentials
dpkms job retry <job-id>

# Cancel a stuck pending/running job
dpkms job cancel <job-id>
```

## Gotchas & Next Steps

- Always verify ingestion jobs reached `completed` before running `ctxt compose` ([../ctxt/compose.md](../ctxt/compose.md)).
- For zombie/stuck jobs across the queue, see `dpkms housekeeping run --zombie-threshold` ([housekeeping.md](housekeeping.md)).
