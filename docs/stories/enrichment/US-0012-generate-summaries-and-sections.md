# US-0012: Generate Summaries And Sections

**System Types:** dpkms (self-hosted), dpkms cloud
**Personas:** [Agents/LLMs](../../personas/agents-llms-tools.md), [Knowledge Workers](../../personas/knowledge-workers.md)

---

## User Goal

As an AI system or knowledge worker, I want long-form content to be automatically summarized and decomposed into logical sections so that retrieval and composition are more precise.

---

## Context

Long documents are difficult to query and compose from. Automatic summarization produces a concise representation for display and ranking. Section decomposition breaks content into addressable chunks, each with its own embedding, enabling fine-grained retrieval.

---

## Acceptance Criteria

- [ ] A brief summary (1–3 sentences) is generated and stored on the object under `summary`
- [ ] Long-form content is decomposed into sections, each with `title`, `body`, and `position`
- [ ] Sections are stored as child objects linked to the parent object
- [ ] Request payload includes the AI provider and max section count
- [ ] Both summary and sections are persisted to the knowledge base before the job is marked complete

---

## Implementation Notes

### CLI Interface

```bash
# Trigger summarization and section decomposition
ctxt enrich <object_id> --step generate-summaries-and-sections --ai-provider instructor --max-sections 10

# Returns immediately with job ID
{
  "job_id": "j-abc123",
  "object_id": "o-def456",
  "status": "pending"
}
```

### REST API Endpoint

```
POST /enrich/{object_id}/summarize
Content-Type: application/json

{
  "step": "generate-summaries-and-sections",
  "ai_provider": "instructor",
  "max_sections": 10
}

→ 200 OK
{
  "object_id": "o-def456",
  "summary": "Alice Smith presented the mobile redesign architecture, deciding to adopt a micro-frontend approach.",
  "sections": [
    {
      "position": 0,
      "title": "Background",
      "body": "The current mobile app was built in 2019..."
    }
  ],
  "sections_created": 3
}
```

### Knowledge Object Update

```json
{
  "id": "o-def456",
  "summary": "Alice Smith presented the mobile redesign architecture...",
  "enrichment": {
    "sections": [
      { "position": 0, "title": "Background", "body": "..." }
    ],
    "summarized_at": "2025-01-18T10:30:45Z",
    "summarization_method": "instructor",
    "sections_created": 3
  }
}
```

---

## E2E Test Checklist

- [ ] CLI: `ctxt enrich <object_id> --step generate-summaries-and-sections --ai-provider instructor` exits 0
- [ ] CLI: `--step generate-summaries-and-sections` flag is present in the request payload sent to server (verified via request capture or server log)
- [ ] CLI: `--ai-provider instructor` flag is present in the request payload as `ai_provider` field
- [ ] CLI: `--max-sections <n>` flag is present in the request payload as `max_sections` field when provided
- [ ] Server: POST `/enrich/{object_id}/summarize` receives `step`, `ai_provider`, and `max_sections` in request body
- [ ] Server: Response contains `summary` string field (non-empty)
- [ ] Server: Response contains `sections` array with `position`, `title`, and `body` fields per entry
- [ ] Server: `sections_created` in response matches the actual count in `sections` array
- [ ] Storage: GET `/objects/{object_id}` returns object with `summary` field populated
- [ ] Storage: GET `/objects/{object_id}` returns object with `enrichment.sections` populated as a non-empty array
- [ ] Storage: `enrichment.summarized_at` timestamp is set on the stored object
- [ ] Storage: `enrichment.summarization_method` matches the `ai_provider` value sent in request
- [ ] Sections: Each section is retrievable as a child object linked to the parent
- [ ] Sections: Section count does not exceed `max_sections` when the flag is provided
- [ ] Error: Empty content returns an error rather than an empty summary
- [ ] Resilience: Step is retryable on transient failures

---

## Related Stories

- [US-0009](./US-0009-extract-entities-and-mentions.md) — Entity extraction step
- [US-0001](../ingestion/US-0001-text-capture-minimal-friction.md) — Trigger for summarization
- [US-0022](../composition/US-0022-generate-brief-from-objects.md) — Uses summaries for composition
