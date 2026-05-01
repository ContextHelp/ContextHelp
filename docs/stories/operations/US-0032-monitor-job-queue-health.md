---
status: shipped
---

# US-0032: Monitor Job Queue Health

**System Types:** dpkms (self-hosted), dpkms cloud
**Personas:** [Operations](../../personas/operations.md), [Maintainers](../../personas/maintainers.md)

---

## User Goal

As an operator, I want to inspect the job queue health — counts by state, recent
failures, throughput — so I can detect backlogs and act before enrichment degrades.

---

## Context

dPKMS processes all ingestion asynchronously via a job queue. Operators need
visibility into queue depth by state (pending, running, completed, failed), error
patterns in failed jobs, and whether workers are keeping up with demand. CLI uses
`ctxt job list` with `--state` / `--limit` flags; the server exposes
`GET /api/v1/jobs?status=&limit=&offset=` and `GET /api/v1/jobs/{id}`. The
`GET /health` endpoint provides a basic server liveness check.

---

## Acceptance Criteria

- [ ] Operator can list all jobs via `ctxt job list`
- [ ] Operator can filter by state: `pending`, `running`, `completed`, `failed`, `cancelled`
- [ ] `--state` flag is forwarded to server as `status` query param
- [ ] `--limit` flag is forwarded to server and controls page size
- [ ] Response includes `total` count alongside paged `data`
- [ ] Operator can inspect a single job: `ctxt job status <id>`
- [ ] Single-job response includes: `id`, `type`, `status`, `pipeline`, timestamps, retry info
- [ ] `GET /health` returns `{"status":"ok"}` when queue and storage are healthy
- [ ] Failed job shows `error` field with human-readable message
- [ ] JSON output mode works (`--output json`) for scripting / alerting integrations

---

## Implementation Notes

### CLI Commands

```
# List all jobs (default limit 50)
ctxt job list

# Filter by state
ctxt job list --state failed
ctxt job list --state pending --limit 100

# Inspect a specific job
ctxt job status job_12345678

# Stream output for alerting scripts
ctxt job list --state failed --output json | jq '.data[] | .id'
```

### REST API

```
GET /api/v1/jobs?status=failed&limit=50&offset=0
→ 200 OK
{
  "data": [
    {
      "id": "job_12345678",
      "type": "analyze",
      "status": "failed",
      "pipeline": "url.article",
      "created_at": "2026-03-15T10:00:00Z",
      "started_at": "2026-03-15T10:00:02Z",
      "completed_at": null,
      "retry_count": 3,
      "max_retries": 3,
      "error": "http: context deadline exceeded"
    }
  ],
  "total": 12
}

GET /api/v1/jobs/{id}
→ 200 OK
{ ...single job object... }

GET /health
→ 200 OK
{"status": "ok"}
```

### Flag → Request Mapping

| CLI flag       | Server query param | Notes                              |
|----------------|--------------------|------------------------------------|
| `--state`      | `?status=`         | maps to `storage.JobFilter.Status` |
| `--limit`      | `?limit=`          | default 50, max determined by svc  |
| (offset)       | `?offset=`         | not yet a CLI flag; API-only       |

---

## E2E Test Checklist

### CLI → Server payload
- [ ] `ctxt job list --state failed` sends request with `status=failed` query param
- [ ] `ctxt job list --limit 10` sends request with `limit=10` query param
- [ ] `ctxt job list --state pending --limit 5` sends both `status=pending` and `limit=5`
- [ ] `ctxt job status job_12345678` sends `GET /api/v1/jobs/job_12345678`

### Server-side receipt and storage
- [ ] `GET /api/v1/jobs?status=failed` returns only jobs with `status == "failed"`
- [ ] `GET /api/v1/jobs?limit=5` returns at most 5 jobs
- [ ] Response `total` reflects full count matching filter, not just current page
- [ ] `GET /api/v1/jobs/{id}` returns the stored job record with all fields populated
- [ ] Job record includes `retry_count` and `max_retries` persisted from worker

### Health check
- [ ] `GET /health` returns `{"status":"ok"}` with HTTP 200 when storage is reachable
- [ ] `GET /health` returns HTTP 503 when storage layer reports an error

### Output and error handling
- [ ] Failed job in list shows non-empty `error` field in JSON output
- [ ] `ctxt job status` for nonexistent ID returns human-readable error (not panic)
- [ ] `ctxt job list --state invalid` returns 400-equivalent error from server
- [ ] JSON output mode (`--output json`) renders machine-parseable structure

### Related flags fully covered
- [ ] All five states (`pending`, `running`, `completed`, `failed`, `cancelled`) are
      accepted by server and return filtered results

---

## Related Stories

- [US-0033](./US-0033-debug-failed-enrichment-job.md) — Debug a specific failed job
- [US-0036](./US-0036-scale-worker-pool-for-load.md) — Scale workers when queue backs up
- [US-0027](../admin/US-0027-configure-ai-provider.md) — AI provider health affects job success
- [US-0009](../enrichment/US-0009-extract-entities-and-mentions.md) — Enrichment jobs monitored here

---

## Personas

- [Operations](../../personas/operations.md)
- [Platform Engineer](/Users/jadb/.w/ideacrafterslabs/.docs/personas/individuals/platform-engineer.md)

---

## E2E Tests

- `test/integration/us0032_job_queue_health_test.go::TestUS0032_HealthReturnsOK`
- `test/integration/us0032_job_queue_health_test.go::TestUS0032_JobCounts`
