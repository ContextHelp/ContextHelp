# US-0011: Assign Tags From Vocabulary

**System Types:** dpkms (self-hosted), dpkms cloud
**Personas:** [Agents/LLMs](../../personas/agents-llms-tools.md), [Knowledge Workers](../../personas/knowledge-workers.md)

---

## User Goal

As an AI system or knowledge worker, I want content to be automatically tagged using a controlled vocabulary so that browsing and filtering are consistent and reliable.

---

## Context

Uncontrolled free-form tagging produces inconsistent taxonomies. A controlled vocabulary (predefined allowed tags) ensures every object is tagged with canonical terms. LMQL hard-constraints prevent the AI from assigning tags outside the allowed set.

---

## Acceptance Criteria

- [ ] Tag assignment is constrained to the operator-defined vocabulary set
- [ ] No tags outside the allowed vocabulary are ever produced
- [ ] Tags are stored on the knowledge object as a JSON array under `tags`
- [ ] Pipeline supports both LMQL (local) and instructor (API-hosted) assignment
- [ ] Operator can specify a custom vocabulary file or use the default vocabulary
- [ ] Request payload includes the vocabulary source used for the assignment

---

## Implementation Notes

### CLI Interface

```bash
# Trigger tag assignment on a captured object
ctxt enrich <object_id> --step assign-tags --ai-provider lmql --vocabulary ./config/tags.yaml

# Returns immediately with job ID
{
  "job_id": "j-abc123",
  "object_id": "o-def456",
  "status": "pending"
}
```

### REST API Endpoint

```
POST /enrich/{object_id}/assign-tags
Content-Type: application/json

{
  "step": "assign-tags",
  "ai_provider": "lmql",
  "vocabulary": "default"
}

→ 200 OK
{
  "object_id": "o-def456",
  "tags": ["architecture", "database", "decision"],
  "tags_assigned": 3,
  "vocabulary_used": "default"
}
```

### Knowledge Object Update

```json
{
  "id": "o-def456",
  "tags": ["architecture", "database", "decision"],
  "enrichment": {
    "tags_assigned_at": "2025-01-18T10:30:45Z",
    "tagging_method": "lmql",
    "vocabulary_used": "default"
  }
}
```

---

## E2E Test Checklist

- [ ] CLI: `ctxt enrich <object_id> --step assign-tags --ai-provider lmql` exits 0
- [ ] CLI: `--step assign-tags` flag is present in the request payload sent to server (verified via request capture or server log)
- [ ] CLI: `--ai-provider lmql` flag is present in the request payload as `ai_provider` field
- [ ] CLI: `--vocabulary <path>` flag is present in the request payload as `vocabulary` field when provided
- [ ] Server: POST `/enrich/{object_id}/assign-tags` receives `step`, `ai_provider`, and `vocabulary` in request body
- [ ] Server: Response contains `tags` array with only vocabulary-valid tag values
- [ ] Server: Response contains `vocabulary_used` field confirming which vocabulary was applied
- [ ] Constraint: All returned tags exist in the configured vocabulary (no out-of-vocabulary tags)
- [ ] Storage: GET `/objects/{object_id}` returns object with `tags` field populated as a non-empty JSON array
- [ ] Storage: `enrichment.tags_assigned_at` timestamp is set on the stored object
- [ ] Storage: `enrichment.vocabulary_used` matches the vocabulary value sent in request
- [ ] LMQL: Tag assignment uses hard `in(allowed_tags)` constraint — invalid tags are never emitted
- [ ] Instructor: API model tag assignment succeeds; Pydantic `Literal` or `enum` enforces vocabulary
- [ ] Error: Empty vocabulary raises a configuration error (not a silent no-op)
- [ ] Resilience: Step is retryable on transient failures

---

## Related Stories

- [US-0009](./US-0009-extract-entities-and-mentions.md) — Entity extraction step
- [US-0014](./US-0014-constrain-extraction-with-lmql.md) — LMQL constraint enforcement
- [US-0001](../ingestion/US-0001-text-capture-minimal-friction.md) — Trigger for tag assignment

---

## Personas

- [Agents / LLMs / Tools](../../personas/agents-llms-tools.md)
- [Automation Builder](/Users/jadb/.w/ideacrafterslabs/.docs/personas/individuals/automation-builder.md)
- [Platform Engineer](/Users/jadb/.w/ideacrafterslabs/.docs/personas/individuals/platform-engineer.md)

---

## E2E Tests

> Not yet implemented.
