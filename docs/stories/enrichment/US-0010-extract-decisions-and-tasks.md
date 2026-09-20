---
status: shipped
---

# US-0010: Extract Decisions And Tasks

**System Types:** dpkms (self-hosted), dpkms cloud
**Personas:** [Agents/LLMs](../../personas/agents-llms-tools.md), [Knowledge Workers](../../personas/knowledge-workers.md)

---

## User Goal

As an AI system or knowledge worker, I want to automatically extract decisions and action items from captured content so they are tracked and queryable.

---

## Context

Meetings, documents, and notes often contain implicit or explicit decisions and action items. Automatic extraction ensures these are surfaced, assigned, and tracked without manual review. Decisions carry impact and status; tasks carry assignee and due date.

---

## Acceptance Criteria

- [ ] Decision extraction identifies decision text, impact (LOW/MEDIUM/HIGH), and status (OPEN/RESOLVED/SUPERSEDED)
- [ ] Task extraction identifies task description and optional assignee
- [ ] Extracted decisions and tasks are stored on the knowledge object under `enrichment.decisions` and `enrichment.tasks`
- [ ] Extraction is constrained to valid enum values (impact, status)
- [ ] Pipeline supports both LMQL (local) and instructor (API-hosted) extraction
- [ ] Decisions and tasks appear in server response after enrichment completes

---

## Implementation Notes

### CLI Interface

```bash
# Trigger decision and task extraction on a captured object
ctxt enrich <object_id> --step extract-decisions-and-tasks --ai-provider lmql

# Returns immediately with job ID
{
  "job_id": "j-abc123",
  "object_id": "o-def456",
  "status": "pending"
}
```

### REST API Endpoint

```
POST /enrich/{object_id}/extract-decisions
Content-Type: application/json

{
  "step": "extract-decisions-and-tasks",
  "ai_provider": "lmql"
}

→ 200 OK
{
  "object_id": "o-def456",
  "decisions": [
    {
      "decision": "Use PostgreSQL as the primary datastore",
      "impact": "HIGH",
      "status": "OPEN"
    }
  ],
  "tasks": [
    {
      "task": "Update database migration scripts",
      "assignee": "@person.alice-smith"
    }
  ],
  "decisions_extracted": 1,
  "tasks_extracted": 1
}
```

### Knowledge Object Update

```json
{
  "id": "o-def456",
  "enrichment": {
    "decisions": [
      {
        "decision": "Use PostgreSQL as the primary datastore",
        "impact": "HIGH",
        "status": "OPEN"
      }
    ],
    "tasks": [
      {
        "task": "Update database migration scripts",
        "assignee": "@person.alice-smith"
      }
    ],
    "decisions_extracted_at": "2025-01-18T10:30:45Z",
    "extraction_method": "lmql"
  }
}
```

---

## E2E Test Checklist

- [ ] CLI: `ctxt enrich <object_id> --step extract-decisions-and-tasks --ai-provider lmql` exits 0
- [ ] CLI: `--step extract-decisions-and-tasks` flag is present in the request payload sent to server (verified via request capture or server log)
- [ ] CLI: `--ai-provider lmql` flag is present in the request payload as `ai_provider` field
- [ ] Server: POST `/enrich/{object_id}/extract-decisions` receives `step` and `ai_provider` in request body
- [ ] Server: Response contains `decisions` array with `decision`, `impact`, and `status` fields
- [ ] Server: Response contains `tasks` array with `task` field
- [ ] Server: `impact` values are constrained to `LOW`, `MEDIUM`, or `HIGH` (no other values accepted)
- [ ] Server: `status` values are constrained to `OPEN`, `RESOLVED`, or `SUPERSEDED`
- [ ] Storage: GET `/objects/{object_id}` returns object with `enrichment.decisions` populated as a non-empty JSON array
- [ ] Storage: GET `/objects/{object_id}` returns object with `enrichment.tasks` populated
- [ ] Storage: `enrichment.decisions_extracted_at` timestamp is set on the stored object
- [ ] Storage: `enrichment.extraction_method` matches the `ai_provider` value sent in request
- [ ] LMQL: Decision impact and status fields are enforced as enum constraints (no invalid values produced)
- [ ] Instructor: API model extraction succeeds with retry on schema validation failure
- [ ] Error: Content with no decisions returns empty `decisions` array (not an error)
- [ ] Resilience: Step is retryable on transient failures

---

## Related Stories

- [US-0009](./US-0009-extract-entities-and-mentions.md) — Entity extraction pipeline step
- [US-0011](./US-0011-assign-tags-from-vocabulary.md) — Tag assignment enrichment
- [US-0014](./US-0014-constrain-extraction-with-lmql.md) — LMQL constraint enforcement

---

## Personas

- [Agents / LLMs / Tools](../../personas/agents-llms-tools.md)
- Automation Builder
- Platform Engineer

---

## E2E Tests

- `test/integration/us0010_decision_extraction_test.go::TestUS0010_DecisionObjectsExtractedWithCorrectFields`
- `test/integration/us0010_decision_extraction_test.go::TestUS0010_DecisionImpactConstrainedToValidEnums`
- `test/integration/us0010_decision_extraction_test.go::TestUS0010_DecisionStatusConstrainedToValidEnums`
- `test/integration/us0010_decision_extraction_test.go::TestUS0010_NoDecisionsReturnsEmptySlice`
- `test/integration/us0010_decision_extraction_test.go::TestUS0010_DecisionCountStoredInMetadata`
