---
status: paper
---

# US-0047: Extract Temporal Information

**System Types:** dpkms (self-hosted)
**Personas:** [Agents/LLMs](../../personas/agents-llms-tools.md), [Knowledge Workers](../../personas/knowledge-workers.md)

---

## User Goal

As an AI system or knowledge worker, I want dates, timelines, and sequences extracted from captured content so that temporal queries and ordering are possible.

---

## Context

Content often contains implicit or explicit time references ("last quarter", "by end of March", "the incident on 2024-11-01"). Normalizing these to ISO-8601 timestamps and storing them as structured fields enables time-range queries, timeline construction, and temporal ordering across objects.

---

## Acceptance Criteria

- [ ] Temporal expressions are detected and normalized to ISO-8601 format where possible
- [ ] Each extracted date includes an `expression` (original text), `normalized` (ISO-8601), and `type` (e.g., `date`, `deadline`, `event`, `range`)
- [ ] Extracted temporal data is stored under `enrichment.temporal` on the knowledge object
- [ ] Request payload includes the AI provider and reference date for relative expression resolution
- [ ] Extracted temporal data is persisted before the job is marked complete

---

## Implementation Notes

### CLI Interface

```bash
# Trigger temporal extraction
ctxt enrich <object_id> --step extract-temporal --ai-provider lmql --reference-date 2025-01-18

# Returns immediately with job ID
{
  "job_id": "j-abc123",
  "object_id": "o-def456",
  "status": "pending"
}
```

### REST API Endpoint

```
POST /enrich/{object_id}/extract-temporal
Content-Type: application/json

{
  "step": "extract-temporal",
  "ai_provider": "lmql",
  "reference_date": "2025-01-18"
}

→ 200 OK
{
  "object_id": "o-def456",
  "temporal": [
    {
      "expression": "end of March",
      "normalized": "2025-03-31",
      "type": "deadline"
    }
  ],
  "temporal_expressions_found": 1
}
```

### Knowledge Object Update

```json
{
  "id": "o-def456",
  "enrichment": {
    "temporal": [
      {
        "expression": "end of March",
        "normalized": "2025-03-31",
        "type": "deadline"
      }
    ],
    "temporal_extracted_at": "2025-01-18T10:30:45Z",
    "extraction_method": "lmql"
  }
}
```

---

## E2E Test Checklist

- [ ] CLI: `ctxt enrich <object_id> --step extract-temporal --ai-provider lmql` exits 0
- [ ] CLI: `--step extract-temporal` flag is present in the request payload sent to server as `step` field (verified via request capture or server log)
- [ ] CLI: `--ai-provider lmql` flag is present in the request payload as `ai_provider` field
- [ ] CLI: `--reference-date <date>` flag is present in the request payload as `reference_date` field when provided
- [ ] Server: POST `/enrich/{object_id}/extract-temporal` receives `step`, `ai_provider`, and `reference_date` in request body
- [ ] Server: Response contains `temporal` array with `expression`, `normalized`, and `type` fields per entry
- [ ] Server: `normalized` values are valid ISO-8601 date strings
- [ ] Server: `type` values are constrained to the canonical set (`date`, `deadline`, `event`, `range`)
- [ ] Storage: GET `/objects/{object_id}` returns object with `enrichment.temporal` populated
- [ ] Storage: `enrichment.temporal_extracted_at` timestamp is set on the stored object
- [ ] Storage: `enrichment.extraction_method` matches the `ai_provider` value sent in request
- [ ] Relative dates: "end of March" resolved correctly relative to `reference_date`
- [ ] Content without dates: Returns empty `temporal` array (not an error)
- [ ] Resilience: Step is retryable on transient failures

---

## Related Stories

- [US-0009](./US-0009-extract-entities-and-mentions.md) — Entity extraction step
- [US-0014](./US-0014-constrain-extraction-with-lmql.md) — LMQL constraint enforcement

---

## Personas

- [Agents / LLMs / Tools](../../personas/agents-llms-tools.md)
- [Automation Builder](/Users/jadb/.w/ideacrafterslabs/.docs/personas/individuals/automation-builder.md)
- [Platform Engineer](/Users/jadb/.w/ideacrafterslabs/.docs/personas/individuals/platform-engineer.md)

---

## E2E Tests

- planned: `test/integration/us0047_temporal_test.go::TestTemporal_ExtractAbsoluteDates`
- planned: `test/integration/us0047_temporal_test.go::TestTemporal_ExtractRelativeDates`
- planned: `test/integration/us0047_temporal_test.go::TestTemporal_TimezoneNormalization`
- planned: `test/integration/us0047_temporal_test.go::TestTemporal_DurationExtraction`
