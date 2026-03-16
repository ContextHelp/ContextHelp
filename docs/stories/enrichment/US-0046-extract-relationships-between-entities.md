# US-0046: Extract Relationships Between Entities

**System Types:** dpkms (self-hosted)
**Personas:** [Agents/LLMs](../../personas/agents-llms-tools.md), [Knowledge Workers](../../personas/knowledge-workers.md)

---

## User Goal

As an AI system or knowledge worker, I want relationships between entities to be automatically extracted from captured content so the knowledge graph reflects how entities are connected.

---

## Context

Entity mentions alone do not capture how entities relate. Extracting typed relationships (e.g., "Alice manages the mobile-redesign project", "backend-api depends-on postgres") enriches the graph with traversable edges that enable discovery, impact analysis, and composition.

---

## Acceptance Criteria

- [ ] Relationship extraction identifies subject entity, predicate (relationship type), and object entity
- [ ] Predicate values are constrained to a canonical set (e.g., `manages`, `depends-on`, `created-by`, `part-of`, `related-to`)
- [ ] Relationships are stored as graph edges with `subject`, `predicate`, and `object` fields
- [ ] Relationships are stored under `enrichment.relationships` on the knowledge object
- [ ] Request payload includes the AI provider and the relationship type vocabulary
- [ ] Extracted relationships are persisted before the job is marked complete

---

## Implementation Notes

### CLI Interface

```bash
# Trigger relationship extraction
ctxt enrich <object_id> --step extract-relationships --ai-provider lmql

# Returns immediately with job ID
{
  "job_id": "j-abc123",
  "object_id": "o-def456",
  "status": "pending"
}
```

### REST API Endpoint

```
POST /enrich/{object_id}/extract-relationships
Content-Type: application/json

{
  "step": "extract-relationships",
  "ai_provider": "lmql"
}

→ 200 OK
{
  "object_id": "o-def456",
  "relationships": [
    {
      "subject": "@person.alice-smith",
      "predicate": "manages",
      "object": "@project.mobile-redesign"
    }
  ],
  "relationships_extracted": 1,
  "graph_edges_added": 1
}
```

### Knowledge Object Update

```json
{
  "id": "o-def456",
  "enrichment": {
    "relationships": [
      {
        "subject": "@person.alice-smith",
        "predicate": "manages",
        "object": "@project.mobile-redesign"
      }
    ],
    "relationships_extracted_at": "2025-01-18T10:30:45Z",
    "extraction_method": "lmql"
  }
}
```

---

## E2E Test Checklist

- [ ] CLI: `ctxt enrich <object_id> --step extract-relationships --ai-provider lmql` exits 0
- [ ] CLI: `--step extract-relationships` flag is present in the request payload sent to server as `step` field (verified via request capture or server log)
- [ ] CLI: `--ai-provider lmql` flag is present in the request payload as `ai_provider` field
- [ ] Server: POST `/enrich/{object_id}/extract-relationships` receives `step` and `ai_provider` in request body
- [ ] Server: Response contains `relationships` array with `subject`, `predicate`, and `object` fields per entry
- [ ] Server: `predicate` values are constrained to the canonical vocabulary (no free-form predicates)
- [ ] Server: `graph_edges_added` in response equals the count of entries in `relationships`
- [ ] Storage: GET `/objects/{object_id}` returns object with `enrichment.relationships` populated
- [ ] Storage: `enrichment.relationships_extracted_at` timestamp is set on the stored object
- [ ] Storage: `enrichment.extraction_method` matches the `ai_provider` value sent in request
- [ ] Graph: Each relationship is traversable via graph query (subject → predicate → object)
- [ ] Error: Content with no relationships returns empty `relationships` array (not an error)
- [ ] Resilience: Step is retryable on transient failures

---

## Related Stories

- [US-0009](./US-0009-extract-entities-and-mentions.md) — Entities that relationships connect
- [US-0014](./US-0014-constrain-extraction-with-lmql.md) — LMQL constraint enforcement
