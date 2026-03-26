# US-0036: Scale Worker Pool for Load

**System Types:** dpkms (self-hosted), dpkms cloud
**Personas:** [Operations](../../personas/operations.md)

---

## User Goal

As an operator, I want to increase the number of worker threads processing the
job queue so that enrichment throughput keeps up with ingestion spikes.

---

## Context

`dpkms serve --workers N` controls the worker pool size. The pool is initialised
at startup via `jobs.NewWorkerPool(..., workers, ...)` and the count is logged as
"Worker pool started (N workers)". During a load spike, operators observe queue
depth growing (via `ctxt job list --state pending`) and respond by restarting the
server with a higher `--workers` value or updating `server.workers` in the config
file. There is no hot-resize; scaling requires a restart. The `GET /health`
endpoint confirms the server is ready after restart. Worker count can also be set
via the `DPKMS_WORKERS` environment variable.

---

## Acceptance Criteria

- [ ] `dpkms serve --workers N` starts a worker pool of exactly N workers
- [ ] Startup log line confirms worker count: "Worker pool started (N workers)"
- [ ] `GET /health` returns `{"status":"ok"}` after pool starts
- [ ] Increasing `--workers` reduces pending job backlog over time
- [ ] `server.workers` config key is honoured when CLI flag is absent
- [ ] `DPKMS_WORKERS` environment variable overrides config file value
- [ ] Worker count of 0 or negative is rejected at startup with a clear error
- [ ] Each worker processes jobs independently; no race conditions on job state

---

## Implementation Notes

### CLI / Config / Env

```bash
# Start with 8 workers via flag
dpkms serve --workers 8

# Start with 8 workers via config
# server:
#   workers: 8
dpkms serve

# Start with 8 workers via environment variable
DPKMS_WORKERS=8 dpkms serve
```

### Worker Count → Request Mapping

| Source            | Key                | Viper binding      |
|-------------------|--------------------|--------------------|
| CLI flag          | `--workers`        | `server.workers`   |
| Config file       | `server.workers`   | `server.workers`   |
| Environment var   | `DPKMS_WORKERS`    | `server.workers`   |

Precedence: CLI flag > environment variable > config file > default (4).

### Scale-Up Procedure (pseudocode)

```
1. Observe backlog: ctxt job list --state pending | count
2. Stop dpkms server gracefully (SIGTERM)
3. Update config or use flag: dpkms serve --workers 8
4. Confirm startup: GET /health → {"status":"ok"}
5. Monitor drain: ctxt job list --state pending (count should decrease)
6. Confirm throughput: ctxt job list --state completed --limit 50 (recent completions)
```

### Graceful Shutdown

On SIGTERM the server:
1. Stops accepting new jobs from the queue
2. Waits for in-flight jobs to complete (or times out)
3. Closes storage driver

This means in-flight jobs are not lost; they complete before shutdown.

---

## E2E Test Checklist

### CLI flag → server behaviour
- [ ] `dpkms serve --workers 8` logs exactly "Worker pool started (8 workers)"
  on stdout during startup
- [ ] `dpkms serve --workers 1` logs "Worker pool started (1 workers)"
- [ ] `dpkms serve` with `server.workers: 6` in config (no flag) logs
  "Worker pool started (6 workers)"
- [ ] `DPKMS_WORKERS=3 dpkms serve` (no flag, no config workers key) logs
  "Worker pool started (3 workers)"
- [ ] CLI flag overrides env var: `DPKMS_WORKERS=3 dpkms serve --workers 8`
  logs "Worker pool started (8 workers)"

### Server-side receipt validation
- [ ] `GET /health` returns HTTP 200 after worker pool starts with any valid N
- [ ] Worker pool processes `N` concurrent jobs; verify by enqueuing `N+2` jobs
  and observing `N` in `running` state simultaneously (via `ctxt job list --state running`)
- [ ] After enqueuing jobs, `GET /api/v1/jobs?status=running` count never exceeds N

### Throughput validation
- [ ] Enqueue 20 jobs; with `--workers 4`, all 20 complete faster than with
  `--workers 1` (wall-clock test)
- [ ] `GET /api/v1/jobs?status=completed` `total` increases over time as workers
  drain the queue

### Graceful shutdown
- [ ] SIGTERM to `dpkms serve` causes in-flight jobs to complete before exit
- [ ] After restart, previously `running` jobs that completed are in `completed`
  state in storage (not stuck in `running`)

### Error paths
- [ ] `dpkms serve --workers 0` exits non-zero with a clear error message
- [ ] `dpkms serve --workers -1` exits non-zero with a clear error message

---

## Related Stories

- [US-0032](./US-0032-monitor-job-queue-health.md) — Observe queue depth driving scale decision
- [US-0033](./US-0033-debug-failed-enrichment-job.md) — Failures under load may need retry
- [US-0035](./US-0035-migrate-storage-backend.md) — Post-migration worker tuning

---

## Personas

- [Operations](../../personas/operations.md)
- [Platform Engineer](/Users/jadb/.w/ideacrafterslabs/.docs/personas/individuals/platform-engineer.md)

---

## E2E Tests

> Not yet implemented.
