# US-0040: Agent Composes Brief Programmatically

**System Types:** dpkms (self-hosted), dpkms cloud
**Personas:** [Agents/LLMs](../../personas/agents-llms-tools.md)

---

## User Goal

As an autonomous agent, I want to programmatically compose a brief from knowledge objects by submitting a query and template, then polling for the finished composition, so I can deliver structured outputs without manual intervention.

---

## Context

Agents need to synthesize knowledge from multiple stored objects into coherent outputs — briefs, summaries, reports — for downstream consumption. The `POST /compose` endpoint accepts a query (RSQL or NLQ), a template type, and an output format, then asynchronously assembles the brief. The agent polls `GET /jobs/{job_id}` until completion and retrieves the result from `GET /compositions/{composition_id}`. The response includes a `sources` array for provenance auditing, so callers can verify which objects contributed to the output.

---

## Acceptance Criteria

- [ ] Agent submits `POST /compose` with required fields: `query`, `template`, `output_format`, and optionally `profile`
- [ ] Server responds with HTTP 202 Accepted containing `job_id`, `composition_id`, and `status`
- [ ] Initial `status` is `"pending"` or `"processing"`
- [ ] Agent polls `GET /jobs/{job_id}` until status reaches `"completed"` or `"failed"`
- [ ] After completion, `GET /compositions/{composition_id}` returns `content`, `sources`, `template`, and `query` fields
- [ ] `sources` array lists object IDs that contributed to the composition (provenance)
- [ ] `template` and `query` fields in the retrieved composition match the values sent in the original request
- [ ] When `output_format=markdown`, the `content` field contains Markdown-formatted text
- [ ] Composition is durable — retrievable by `composition_id` in subsequent requests
- [ ] `POST /compose` with a query matching zero objects returns an informative error, not a silent empty brief
- [ ] `POST /compose` with a missing `template` field returns HTTP 400 with an error message
- [ ] Agent surfaces composition failure reason when job status is `"failed"`

---

## Implementation Notes

### Composition Request

```
POST /compose
Content-Type: application/json

{
  "query": "type==decision;mentions=in=@project.backend",
  "template": "brief",            // "brief" | "summary" | "report"
  "output_format": "markdown",    // "markdown" | "json" | "plain"
  "profile": "engineering"        // optional focus profile
}

→ 202 Accepted
{
  "job_id": "job-comp456",
  "composition_id": "c-def789",
  "status": "pending"
}
```

### Job Status Polling

```
GET /jobs/{job_id}

→ 200 OK
{
  "job_id": "job-comp456",
  "status": "completed",
  "composition_id": "c-def789",
  "error": null
}
```

### Composition Retrieval

```
GET /compositions/{composition_id}

→ 200 OK
{
  "composition_id": "c-def789",
  "content": "# Backend Decisions\n\n...",
  "sources": ["o-abc001", "o-abc002", "o-abc003"],
  "template": "brief",
  "query": "type==decision;mentions=in=@project.backend",
  "output_format": "markdown",
  "created_at": "2025-01-15T12:00:00Z"
}
```

### Agent Composition Pattern

```python
import time
import requests

def compose_brief(api_url, query, template, output_format="markdown",
                  profile=None, poll_interval=3, max_wait=300):
    body = {"query": query, "template": template, "output_format": output_format}
    if profile:
        body["profile"] = profile

    resp = requests.post(f"{api_url}/compose", json=body,
                         headers={"Content-Type": "application/json"})
    if resp.status_code != 202:
        raise RuntimeError(f"Compose failed: {resp.status_code} {resp.text}")

    data = resp.json()
    job_id = data["job_id"]
    composition_id = data["composition_id"]

    deadline = time.time() + max_wait
    while time.time() < deadline:
        job = requests.get(f"{api_url}/jobs/{job_id}").json()
        status = job["status"]
        if status == "completed":
            return requests.get(f"{api_url}/compositions/{composition_id}").json()
        if status == "failed":
            raise RuntimeError(f"Composition failed: {job.get('error')}")
        time.sleep(poll_interval)

    raise TimeoutError(f"Composition {composition_id} did not complete within {max_wait}s")
```

---

## E2E Test Checklist

- [ ] Request: Agent sends POST /compose with `Content-Type: application/json` header and non-empty JSON body
- [ ] Request: POST /compose request body includes `query` field containing the RSQL or NLQ string used to select source content
- [ ] Request: POST /compose request body includes `template` field specifying the brief format (e.g., `"brief"`, `"summary"`, `"report"`)
- [ ] Request: POST /compose request body includes `profile` field when a focus profile is active
- [ ] Request: POST /compose request body includes `output_format` field (e.g., `"markdown"`, `"json"`, `"plain"`)
- [ ] Response: POST /compose returns HTTP 202 Accepted with `job_id` and `composition_id` fields in response body
- [ ] Response: Response body `status` field is `"pending"` or `"processing"` immediately after submission
- [ ] Polling: Agent sends GET /jobs/{job_id} using the `job_id` from the compose response to track progress
- [ ] Polling: GET /jobs/{job_id} response body includes `status` field that transitions to `"completed"` when done
- [ ] Storage: After job completes, agent sends GET /compositions/{composition_id} to retrieve the result
- [ ] Storage: GET /compositions/{composition_id} response body contains `content` field with the composed brief text
- [ ] Storage: GET /compositions/{composition_id} response body contains `sources` array listing the object IDs used as inputs (server-side provenance validated)
- [ ] Storage: GET /compositions/{composition_id} response body contains `template` field matching the value sent in the original request
- [ ] Storage: GET /compositions/{composition_id} response body contains `query` field matching the value sent in the original request (server-side receipt validated)
- [ ] Storage: Composed brief is retrievable by `composition_id` in subsequent GET requests (persistence validated)
- [ ] Content: Composed brief includes content derived from objects matching the submitted query (at least one source object ID appears in `sources` array)
- [ ] Format: When `output_format=markdown` is requested, response `content` field contains Markdown-formatted text
- [ ] Error: POST /compose with a query that matches zero objects returns an informative error (not a silent empty brief)
- [ ] Error: POST /compose with a missing `template` field returns HTTP 400 with error message
- [ ] Error: Agent surfaces composition failure reason when job transitions to `"failed"` status

---

## Related Stories

- [agent-ingests-content-and-waits](./US-0039-agent-ingests-content-and-waits.md) — Ingest objects that become composition inputs
- [agent-constructs-rsql-query](./US-0038-agent-constructs-rsql-query.md) — Build the RSQL query used in the compose request
- [generate-brief-from-objects](../composition/US-0022-generate-brief-from-objects.md) — Human-facing composition counterpart
- [compose-with-graph-traversal](../composition/US-0024-compose-with-graph-traversal.md) — Advanced composition using entity relationships

---

## Personas

- [Agents / LLMs / Tools](../../personas/agents-llms-tools.md)
- [Automation Builder](/Users/jadb/.w/ideacrafterslabs/.docs/personas/individuals/automation-builder.md)

---

## E2E Tests

- `test/integration/us0040_agent_compose_test.go::TestUS0040_ComposeReturnsBriefWithObjectContent`
- `test/integration/us0040_agent_compose_test.go::TestUS0040_ComposeBriefIsMarkdown`
- `test/integration/us0040_agent_compose_test.go::TestUS0040_ComposeWithCitationsIncludesSourceIDs`
- `test/integration/us0040_agent_compose_test.go::TestUS0040_ComposeWithCitationsStructuredNotMarkdownBlob`
- `test/integration/us0040_agent_compose_test.go::TestUS0040_ComposeQueryMatchTemplate`
- `test/integration/us0040_agent_compose_test.go::TestUS0040_ComposeEmptyObjectsReturnsEmptyBrief`
- `test/integration/us0040_agent_compose_test.go::TestUS0040_ComposeObjectsQueryableAfterCompose`
- `test/integration/us0040_agent_compose_test.go::TestUS0040_HTTPComposeEndpointExists`
- `test/integration/us0040_agent_compose_test.go::TestUS0040_HTTPComposeReturnsJobIDAndCompositionID`
- `test/integration/us0040_agent_compose_test.go::TestUS0040_HTTPComposeMissingTemplateReturns400`
