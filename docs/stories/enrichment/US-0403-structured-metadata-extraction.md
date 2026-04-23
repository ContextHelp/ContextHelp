# US-0403: Structured Metadata Extraction at Ingest

**System Types:** dpkms (self-hosted), dpkms cloud
**Personas:** [Agents/LLMs](../../personas/agents-llms-tools.md),
[Knowledge Workers](../../personas/knowledge-workers.md)

---

## User Goal

As an agent or knowledge worker, I want every ingested object to
be automatically classified with structured metadata — type,
topics, people, action items, dates, source — so downstream
search, filtering, and fan-out have rich facets to work with.

---

## Context

OB1 extracts structured metadata at capture time: type
(observation|task|idea|reference|person_note), topics, people,
action_items, dates_mentioned, source. ctxt has entity extraction
(US-0009) but lacks a unified structured metadata schema applied
at ingest time. This is the prerequisite for source filtering
(US-0407) and fan-out (US-0400).

Aligns with existing LMQL constrained extraction work (US-0014).

---

## Acceptance Criteria

- [x] Every ingested object receives structured metadata
  extracted by LLM
- [x] Metadata schema includes: `type`, `topics[]`,
  `people[]`, `action_items[]`, `dates_mentioned[]`,
  `source_type`
- [x] `type` constrained to vocabulary: observation, task,
  idea, reference, person_note, decision, question
- [ ] `topics` constrained to existing tag vocabulary +
  new-topic proposal
- [ ] Extraction uses LMQL constraints for local models,
  instructor for API models
- [x] Metadata stored under `enrichment.structured_metadata`
  on the knowledge object
- [x] Extraction is idempotent (re-running produces same
  result for same content)
- [x] Fallback: if extraction fails, object is still stored
  with `metadata_status: pending`

---

## Implementation Notes

### Metadata Schema

```
structured_metadata {
  type: "observation" | "task" | "idea" | "reference"
        | "person_note" | "decision" | "question"
  topics: ["auth", "performance", "ux"]
  people: ["@person.alice-chen", "@person.bob-smith"]
  action_items: ["review PR #42", "update docs"]
  dates_mentioned: ["2026-04-23", "2026-05-01"]
  source_type: "text" | "url" | "file" | "api" | "webhook"
  confidence: 0.92
}
```

### CLI Interface

```bash
# Ingest with metadata extraction (default)
ctxt "Alice said we should redesign auth by May 1st"

# View extracted metadata
ctxt open <id> --show metadata

# Re-extract metadata for existing object
ctxt enrich <id> --step structured-metadata
```

---

## E2E Test Checklist

- [x] Ingest text mentioning person + date + action ->
  all three extracted correctly
- [x] Type classification matches content intent
  (task vs observation vs decision)
- [ ] Topics map to existing vocabulary where possible
- [x] Re-extraction of same content -> same metadata
- [x] Failed extraction -> object stored with
  `metadata_status: pending`
- [x] Metadata searchable via type filter

---

## Related Stories

- US-0009: Extract entities and mentions (subset of this)
- US-0014: Constrain extraction with LMQL (mechanism)
- US-0400: Fan-out enrichment (consumes this metadata)
- US-0407: Source-scoped metadata facet search
