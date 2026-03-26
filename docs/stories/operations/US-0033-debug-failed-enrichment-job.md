# US-0033: Debug Failed Enrichment Job

**System Types:** dpkms (self-hosted), dpkms cloud
**Personas:** [Operations](../../personas/operations.md), [Maintainers](../../personas/maintainers.md)

---

## User Goal

As an operator, I want to inspect a failed enrichment job — see its error message,
logs, and retry history — and re-queue it once I've resolved the root cause.

---

## Context

When a job enters `failed` state the worker has exhausted retries. Operators need
to: (1) read the stored error message, (2) view execution logs, (3) retry the job
after fixing the underlying issue (e.g., bad AI provider key, network timeout,
malformed content). CLI uses `ctxt job log <id>` and `ctxt job retry <id>`;
server exposes `GET /api/v1/jobs/{id}` and `POST /api/v1/jobs/{id}/retry`.

---

## Acceptance Criteria

- [ ] `ctxt job log <id>` fetches and displays the job's stored error message
- [ ] `ctxt job status <id>` shows `retry_count` / `max_retries` alongside error
- [ ] `ctxt job retry <id>` sends retry request to server by job ID
- [ ] Server resets job state to `pending` and re-enqueues it on retry
- [ ] Retried job re-appears in `ctxt job list --state pending`
- [ ] Retry of a non-failed job (e.g., `completed`) returns an error
- [ ] Retry of a nonexistent job returns 404 with clear error message
- [ ] Job `error` field is persisted in storage and survives server restart

---

## Implementation Notes

### CLI Commands

```
# View error details for a failed job
ctxt job status job_12345678

# View execution log (error text) for a failed job
ctxt job log job_12345678

# Retry after fixing root cause
ctxt job retry job_12345678
```

### REST API

```
GET /api/v1/jobs/{id}
→ 200 OK
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
  "error": "openai: rate limit exceeded"
}

POST /api/v1/jobs/{id}/retry
→ 200 OK
{
  "id": "job_12345678",
  "status": "pending",
  ...
}
```

### Server-Side Retry Logic (pseudocode)

```
RetryJob(ctx, id):
  job = store.GetJob(ctx, id)
  if job.status != "failed":
    return ErrNotFailed
  job.status = "pending"
  job.retry_count = 0   // reset for new attempt
  store.UpdateJob(ctx, job)
  queue.Enqueue(job)
```

---

## E2E Test Checklist

### CLI → Server payload
- [ ] `ctxt job log job_12345678` sends `GET /api/v1/jobs/job_12345678`
- [ ] `ctxt job retry job_12345678` sends `POST /api/v1/jobs/job_12345678/retry`
  with job ID in the URL path (no body required)
- [ ] No extra flags needed; job ID is the sole identifier forwarded to the server

### Server-side receipt and storage
- [ ] `GET /api/v1/jobs/{id}` response includes `error` field with stored message
- [ ] `error` field is non-empty for jobs in `failed` state
- [ ] `retry_count` and `max_retries` are returned in the job record
- [ ] `POST /api/v1/jobs/{id}/retry` resets job `status` to `"pending"` in storage
- [ ] Retried job appears in `GET /api/v1/jobs?status=pending` response
- [ ] `error` field is preserved in storage after retry (the field is not cleared; it records the most recent failure for audit)

### Error paths
- [ ] `POST /api/v1/jobs/{id}/retry` on a `completed` job returns 400 with
  `INVALID_REQUEST` error code
- [ ] `POST /api/v1/jobs/{nonexistent}/retry` returns 404 with `NOT_FOUND`
- [ ] `GET /api/v1/jobs/{nonexistent}` returns 404 with `NOT_FOUND`

### Observability
- [ ] `ctxt job log` output displays the raw `error` field text, not just exit code
- [ ] `ctxt job status` output shows `Retries: N/M` line with actual counts from server
- [ ] JSON output mode renders full job record including `error`, `retry_count`,
  `max_retries` fields

---

## Related Stories

- [US-0032](./US-0032-monitor-job-queue-health.md) — Discover failed jobs via queue view
- [US-0027](../admin/US-0027-configure-ai-provider.md) — Fix AI provider config before retry
- [US-0009](../enrichment/US-0009-extract-entities-and-mentions.md) — Enrichment job details

---

## Personas

- [Operations](../../personas/operations.md)
- [Platform Engineer](/Users/jadb/.w/ideacrafterslabs/.docs/personas/individuals/platform-engineer.md)

---

## E2E Tests

> Not yet implemented.
