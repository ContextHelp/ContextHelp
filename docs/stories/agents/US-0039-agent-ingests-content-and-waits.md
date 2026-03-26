# US-0039: Agent Ingests Content And Waits

**System Types:** dpkms (self-hosted), dpkms cloud
**Personas:** [Agents/LLMs](../../personas/agents-llms-tools.md)

---

## User Goal

As an autonomous agent, I want to submit content for ingestion and asynchronously poll for completion so I can proceed with downstream queries only after the object is fully enriched and stored.

---

## Context

Agents cannot rely on synchronous ingestion because enrichment pipelines are long-running. The `POST /analyze` endpoint returns immediately with a job ID and an object ID. The agent must poll `GET /jobs/{job_id}` until the job reaches a terminal state (`"completed"` or `"failed"`), then retrieve the enriched object via `GET /objects/{object_id}`. This async model keeps the API non-blocking and allows agents to handle multiple in-flight ingestions concurrently.

---

## Acceptance Criteria

- [ ] Agent submits content via `POST /analyze` with required fields: `content`, `source_type`, and optionally `profile`
- [ ] Server responds with HTTP 202 Accepted containing `job_id`, `object_id`, and `status`
- [ ] Initial `status` in the 202 response is `"pending"` or `"pending_enrichment"`
- [ ] Agent polls `GET /jobs/{job_id}` at a configured interval until status is terminal
- [ ] Job status transitions on server: `"pending"` → `"processing"` → `"completed"` (or `"failed"`)
- [ ] After `"completed"`, object is retrievable via `GET /objects/{object_id}` with non-empty `summary` and `pipeline` fields
- [ ] Stored object `id` matches the `object_id` from the original ingest response
- [ ] Agent times out gracefully if job does not complete within a configured maximum wait
- [ ] Agent does not poll if the initial `POST /analyze` returns a non-202 status
- [ ] Agent surfaces failure reason from response body when job status is `"failed"`
- [ ] Agent reports job not found without crashing if `GET /jobs/{job_id}` returns 404

---

## Implementation Notes

### Ingest Endpoint

```
POST /analyze
Content-Type: application/json

{
  "content": "Article text or URL...",
  "source_type": "text",          // "text" | "url" | "document" | ...
  "profile": "engineering"        // optional focus profile
}

→ 202 Accepted
{
  "job_id": "job-abc123",
  "object_id": "o-xyz789",
  "status": "pending"
}
```

### Job Status Polling

```
GET /jobs/{job_id}

→ 200 OK
{
  "job_id": "job-abc123",
  "status": "processing",         // "pending" | "processing" | "completed" | "failed"
  "object_id": "o-xyz789",
  "error": null                   // populated when status == "failed"
}
```

### Object Retrieval After Completion

```
GET /objects/{object_id}

→ 200 OK
{
  "id": "o-xyz789",
  "summary": "...",
  "pipeline": "text.short",
  "tags": [...],
  "mentions": [...],
  "created_at": "2025-01-15T10:30:00Z"
}
```

### Agent Polling Pattern

```python
import time
import requests

def ingest_and_wait(api_url, content, source_type, profile=None,
                    poll_interval=2, max_wait=120):
    body = {"content": content, "source_type": source_type}
    if profile:
        body["profile"] = profile

    resp = requests.post(f"{api_url}/analyze", json=body,
                         headers={"Content-Type": "application/json"})
    if resp.status_code != 202:
        raise RuntimeError(f"Ingest failed: {resp.status_code} {resp.text}")

    data = resp.json()
    job_id = data["job_id"]
    object_id = data["object_id"]

    deadline = time.time() + max_wait
    while time.time() < deadline:
        job = requests.get(f"{api_url}/jobs/{job_id}").json()
        status = job["status"]
        if status == "completed":
            return requests.get(f"{api_url}/objects/{object_id}").json()
        if status == "failed":
            raise RuntimeError(f"Job failed: {job.get('error')}")
        time.sleep(poll_interval)

    raise TimeoutError(f"Job {job_id} did not complete within {max_wait}s")
```

---

## E2E Test Checklist

- [ ] Request: Agent sends POST /analyze with `Content-Type: application/json` header and JSON body containing `content` field (not empty)
- [ ] Request: POST /analyze request body includes `source_type` field set to the correct value (e.g., `"text"`, `"url"`)
- [ ] Request: POST /analyze request body includes `profile` field when a focus profile is specified by the agent
- [ ] Response: POST /analyze returns HTTP 202 Accepted (not 200)
- [ ] Response: 202 response body contains `job_id` field with a non-empty string value
- [ ] Response: 202 response body contains `object_id` field with a non-empty string value
- [ ] Response: 202 response body contains `status` field set to `"pending_enrichment"` or `"pending"`
- [ ] Polling: Agent sends GET /jobs/{job_id} using the `job_id` received from the ingest response
- [ ] Polling: GET /jobs/{job_id} request URL contains the exact `job_id` returned by POST /analyze
- [ ] Polling: Agent polls at configured interval until `status` transitions to `"completed"` or `"failed"`
- [ ] Polling: Server returns `status` field in GET /jobs/{job_id} response body
- [ ] Polling: Server-side job record exists and is retrievable immediately after POST /analyze completes
- [ ] Polling: Job `status` on server transitions from `"pending"` → `"processing"` → `"completed"` (verified across sequential GET /jobs/{job_id} calls)
- [ ] Storage: After job `status` is `"completed"`, GET /objects/{object_id} returns the stored object with non-empty `summary` and `pipeline` fields (server-side storage validated)
- [ ] Storage: Stored object's `id` matches `object_id` returned in the original POST /analyze response
- [ ] Timeout: Agent handles job that remains in `"pending"` longer than expected (does not poll indefinitely; times out gracefully after configured maximum wait)
- [ ] Error: If POST /analyze returns non-202 status, agent surfaces the error and does not poll
- [ ] Error: If GET /jobs/{job_id} returns `status: "failed"`, agent surfaces the failure reason from the response body
- [ ] Error: If GET /jobs/{job_id} returns 404, agent reports job not found without crashing

---

## Related Stories

- [agent-constructs-rsql-query](./US-0038-agent-constructs-rsql-query.md) — Query newly ingested objects after enrichment
- [agent-uses-constrained-enrichment](./US-0041-agent-uses-constrained-enrichment.md) — Trigger targeted enrichment on an ingested object
- [url-capture-and-extraction](../ingestion/US-0002-url-capture-and-extraction.md) — Human-facing ingestion counterpart
- [batch-enrichment-with-progress](../enrichment/US-0015-batch-enrichment-with-progress.md) — Batch ingestion with job monitoring

---

## Personas

- [Agents / LLMs / Tools](../../personas/agents-llms-tools.md)
- [Automation Builder](/Users/jadb/.w/ideacrafterslabs/.docs/personas/individuals/automation-builder.md)

---

## E2E Tests

> Not yet implemented.
