---
status: paper
---

# US-0049: Classify Content With Taxonomy

**System Types:** dpkms (self-hosted)
**Personas:** [Agents/LLMs](../../personas/agents-llms-tools.md), [Platform Integrators](../../personas/platform-integrators.md)

---

## User Goal

As an AI system or platform integrator, I want content to be automatically classified against a predefined taxonomy so that hierarchical browsing, routing, and filtering are possible.

---

## Context

Tags (US-0011) are flat vocabulary labels. A taxonomy adds hierarchy: content classified under `engineering/backend/database` is also implicitly under `engineering/backend` and `engineering`. This enables drill-down navigation, structured routing to specialized agents, and hierarchical search filters.

---

## Acceptance Criteria

- [ ] Content is classified to one or more taxonomy paths using the operator-configured taxonomy
- [ ] Each classification includes a `path` (e.g., `engineering/backend/database`) and a confidence score
- [ ] Classification paths are constrained to the configured taxonomy (no ad-hoc paths)
- [ ] Results are stored under `enrichment.classifications` on the knowledge object
- [ ] Request payload includes the AI provider and taxonomy identifier
- [ ] Results are persisted before the job is marked complete

---

## Implementation Notes

### CLI Interface

```bash
# Trigger taxonomy classification
ctxt enrich <object_id> --step classify-content --ai-provider lmql --taxonomy engineering-v1

# Returns immediately with job ID
{
  "job_id": "j-abc123",
  "object_id": "o-def456",
  "status": "pending"
}
```

### REST API Endpoint

```
POST /enrich/{object_id}/classify
Content-Type: application/json

{
  "step": "classify-content",
  "ai_provider": "lmql",
  "taxonomy": "engineering-v1"
}

→ 200 OK
{
  "object_id": "o-def456",
  "classifications": [
    {
      "path": "engineering/backend/database",
      "confidence": 0.92
    }
  ],
  "taxonomy_used": "engineering-v1",
  "classifications_assigned": 1
}
```

### Knowledge Object Update

```json
{
  "id": "o-def456",
  "enrichment": {
    "classifications": [
      {
        "path": "engineering/backend/database",
        "confidence": 0.92
      }
    ],
    "classified_at": "2025-01-18T10:30:45Z",
    "classification_method": "lmql",
    "taxonomy_used": "engineering-v1"
  }
}
```

---

## E2E Test Checklist

- [ ] CLI: `ctxt enrich <object_id> --step classify-content --ai-provider lmql --taxonomy engineering-v1` exits 0
- [ ] CLI: `--step classify-content` flag is present in the request payload sent to server as `step` field (verified via request capture or server log)
- [ ] CLI: `--ai-provider lmql` flag is present in the request payload as `ai_provider` field
- [ ] CLI: `--taxonomy <id>` flag is present in the request payload as `taxonomy` field
- [ ] Server: POST `/enrich/{object_id}/classify` receives `step`, `ai_provider`, and `taxonomy` in request body
- [ ] Server: Response contains `classifications` array with `path` and `confidence` per entry
- [ ] Server: `taxonomy_used` in response matches the `taxonomy` value sent in request
- [ ] Constraint: All `path` values exist in the specified taxonomy (no ad-hoc paths returned)
- [ ] Storage: GET `/objects/{object_id}` returns object with `enrichment.classifications` populated
- [ ] Storage: `enrichment.classified_at` timestamp is set on the stored object
- [ ] Storage: `enrichment.taxonomy_used` matches the `taxonomy` value sent in request
- [ ] Error: Unknown taxonomy identifier returns 422 with a clear error message
- [ ] Error: Empty taxonomy raises a configuration error (not a silent no-op)
- [ ] Resilience: Step is retryable on transient failures

---

## Related Stories

- [US-0011](./US-0011-assign-tags-from-vocabulary.md) — Flat tag assignment (complementary to taxonomy)
- [US-0014](./US-0014-constrain-extraction-with-lmql.md) — LMQL constraint enforcement

---

## Personas

- [Agents / LLMs / Tools](../../personas/agents-llms-tools.md)
- Automation Builder
- Platform Engineer

---

## E2E Tests

- planned: `test/integration/us0049_classify_test.go::TestClassify_AssignsTaxonomyLabels`
- planned: `test/integration/us0049_classify_test.go::TestClassify_MultiLabel`
- planned: `test/integration/us0049_classify_test.go::TestClassify_CustomTaxonomyConfig`
- planned: `test/integration/us0049_classify_test.go::TestClassify_ConfidenceThreshold`
