---
status: shipped
---

# US-0041: Agent Uses Constrained Enrichment

**System Types:** dpkms (self-hosted), dpkms cloud
**Personas:** [Agents/LLMs](../../personas/agents-llms-tools.md), [Platform Integrators](../../personas/platform-integrators.md)

---

## User Goal

As an autonomous agent, I want to trigger targeted constrained enrichment on a stored object — specifying an extraction type and an explicit constraint vocabulary — so that extracted entities and tags conform to a known schema and cannot produce unexpected values.

---

## Context

Raw ingestion produces objects with minimal enrichment. Agents that rely on deterministic queries (RSQL) need entities (`mentions`) and tags to follow strict schemas: entities must match the `@namespace.slug` pattern; tags must come from a fixed vocabulary. The `POST /enrich` endpoint accepts an `object_id`, an `extraction_type`, a `constraints` object, and a `provider` field. The server runs the extraction asynchronously and updates the stored object only with values that pass constraint validation. This prevents hallucinated or out-of-vocabulary values from polluting the knowledge graph. The design builds directly on US-0014 (constrain-extraction-with-lmql) and is the programmatic API surface for that capability.

---

## Acceptance Criteria

- [ ] Agent submits `POST /enrich` with required fields: `object_id`, `extraction_type`, `constraints`, and `provider`
- [ ] Server responds with HTTP 202 Accepted containing `job_id`, `object_id`, and `extraction_type`
- [ ] `object_id` and `extraction_type` echoed in the 202 response match the submitted values
- [ ] Agent polls `GET /jobs/{job_id}` until status reaches `"completed"` or `"failed"`
- [ ] Job status transitions on server: `"pending"` → `"processing"` → `"completed"`
- [ ] Extracted entities conform to the `@namespace.slug` pattern declared in the `constraints` object (no out-of-vocabulary values stored)
- [ ] Extracted tags are a strict subset of the `allowed_tags` list in the `constraints` object
- [ ] After completion, `GET /objects/{object_id}` returns the object with updated `mentions` and `tags` arrays
- [ ] The `pipeline` field on the stored object reflects the enrichment pipeline used
- [ ] Enrichment result is durable — a second `GET /objects/{object_id}` returns the same updated fields
- [ ] Re-submitting `POST /enrich` for the same `object_id` and `extraction_type` does not duplicate entities or tags (idempotent)
- [ ] `POST /enrich` with a non-existent `object_id` returns HTTP 404 with an error message
- [ ] `POST /enrich` with an unsupported `extraction_type` returns HTTP 400 with an error message
- [ ] `POST /enrich` where constraints produce zero valid outputs results in a `"failed"` job status with a descriptive error
- [ ] Agent surfaces enrichment failure reason from the job response body

---

## Implementation Notes

### Enrich Request

```
POST /enrich
Content-Type: application/json

{
  "object_id": "o-xyz789",
  "extraction_type": "entities",         // "entities" | "decisions" | "tags"
  "constraints": {
    "entity_pattern": "@[a-z0-9]+\\.[a-z0-9-]+",
    "allowed_tags": ["recommended", "important", "draft", "archived"]
  },
  "provider": "openai/gpt-4o-mini"       // AI provider or model identifier
}

→ 202 Accepted
{
  "job_id": "job-enrich001",
  "object_id": "o-xyz789",
  "extraction_type": "entities",
  "status": "pending"
}
```

### Job Status Polling

```
GET /jobs/{job_id}

→ 200 OK
{
  "job_id": "job-enrich001",
  "status": "completed",
  "object_id": "o-xyz789",
  "error": null
}
```

### Object After Enrichment

```
GET /objects/{object_id}

→ 200 OK
{
  "id": "o-xyz789",
  "mentions": ["@person.alice", "@project.backend"],
  "tags": ["recommended", "important"],
  "pipeline": "text.short",
  "summary": "...",
  "created_at": "2025-01-15T10:30:00Z"
}
```

### Constraint Enforcement

- **Entity pattern** (`@namespace.slug`): The extraction model is constrained (via LMQL for local models, or instructor/outlines for API models) to produce only values matching `@[a-z0-9]+\.[a-z0-9-]+`. Values not matching the pattern are discarded before storage.
- **Tag vocabulary** (`allowed_tags`): Only tags present in the `allowed_tags` list pass validation. Out-of-vocabulary tags are dropped, not stored.
- **Zero valid outputs**: If all extracted values fail constraint validation, the job transitions to `"failed"` with a descriptive error — not a silent no-op update.

See [constrain-extraction-with-lmql](../enrichment/US-0014-constrain-extraction-with-lmql.md) for the underlying constraint implementation.

---

## E2E Test Checklist

- [ ] Request: Agent sends POST /enrich with `Content-Type: application/json` header and non-empty JSON body
- [ ] Request: POST /enrich request body includes `object_id` field referencing an existing knowledge object
- [ ] Request: POST /enrich request body includes `extraction_type` field specifying the kind of constrained extraction (e.g., `"entities"`, `"decisions"`, `"tags"`)
- [ ] Request: POST /enrich request body includes `constraints` object specifying allowed values (e.g., `{"allowed_tags": [...]}` or `{"entity_pattern": "@[a-z0-9]+\\.[a-z0-9-]+"}`)
- [ ] Request: POST /enrich request body includes `provider` field indicating the AI provider or model to use for extraction
- [ ] Response: POST /enrich returns HTTP 202 Accepted with `job_id` field in response body
- [ ] Response: 202 response body contains `object_id` matching the value sent in the request (server-side receipt validated)
- [ ] Response: 202 response body contains `extraction_type` matching the value sent in the request
- [ ] Polling: Agent polls GET /jobs/{job_id} using the `job_id` from the enrich response
- [ ] Polling: Server-side job status transitions from `"pending"` → `"processing"` → `"completed"` (validated across sequential GET /jobs/{job_id} calls)
- [ ] Constraints: Extracted entities conform to the `@namespace.slug` pattern specified in the request `constraints` object (no out-of-vocabulary values present in stored result)
- [ ] Constraints: Extracted tags are a strict subset of the `allowed_tags` list sent in the request `constraints` object (server validates and rejects non-conforming values)
- [ ] Storage: After job completes, GET /objects/{object_id} returns the object with `mentions` array updated to include newly extracted entities
- [ ] Storage: After job completes, GET /objects/{object_id} returns the object with `tags` array updated to include assigned tags from the constrained vocabulary
- [ ] Storage: Enrichment result stored on server is durable — a second GET /objects/{object_id} after enrichment returns the same updated fields
- [ ] Storage: `pipeline` field on the stored object reflects the enrichment pipeline used (e.g., `"text.short"`)
- [ ] Idempotency: Re-submitting POST /enrich for the same `object_id` and `extraction_type` does not duplicate entities or tags in stored object
- [ ] Error: POST /enrich with an `object_id` that does not exist returns HTTP 404 with error message
- [ ] Error: POST /enrich with an unsupported `extraction_type` returns HTTP 400 with error message
- [ ] Error: POST /enrich with a `constraints` value that produces zero valid outputs returns a `"failed"` job status with a descriptive error (not a silent empty update)
- [ ] Error: Agent surfaces enrichment failure reason when GET /jobs/{job_id} returns `status: "failed"`

---

## Related Stories

- [agent-ingests-content-and-waits](./US-0039-agent-ingests-content-and-waits.md) — Ingest object before enrichment
- [agent-constructs-rsql-query](./US-0038-agent-constructs-rsql-query.md) — Query enriched `mentions` and `tags` after enrichment completes
- [constrain-extraction-with-lmql](../enrichment/US-0014-constrain-extraction-with-lmql.md) — Underlying constraint enforcement mechanism
- [extract-entities-and-mentions](../enrichment/US-0009-extract-entities-and-mentions.md) — Unconstrained entity extraction counterpart
- [assign-tags-from-vocabulary](../enrichment/US-0011-assign-tags-from-vocabulary.md) — Tag vocabulary assignment counterpart

---

## Personas

- [Agents / LLMs / Tools](../../personas/agents-llms-tools.md)
- Automation Builder

---

## E2E Tests

- `test/integration/us0041_agent_constrained_enrichment_test.go::TestUS0041_ConstrainedEntitiesConformToPattern`
- `test/integration/us0041_agent_constrained_enrichment_test.go::TestUS0041_ConstrainedTagsAreSubsetOfAllowedList`
- `test/integration/us0041_agent_constrained_enrichment_test.go::TestUS0041_EnrichmentResultIsDurable`
- `test/integration/us0041_agent_constrained_enrichment_test.go::TestUS0041_EnrichmentUpdatesObjectViaHTTP`
- `test/integration/us0041_agent_constrained_enrichment_test.go::TestUS0041_PipelineFieldReflectsEnrichmentPipeline`
- `test/integration/us0041_agent_constrained_enrichment_test.go::TestUS0041_HTTPEnrichEndpointExists`
- `test/integration/us0041_agent_constrained_enrichment_test.go::TestUS0041_HTTPEnrichNonexistentObjectReturns404`
- `test/integration/us0041_agent_constrained_enrichment_test.go::TestUS0041_HTTPEnrichUnsupportedExtractionTypeReturns400`
