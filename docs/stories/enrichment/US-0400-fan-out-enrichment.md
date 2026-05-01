---
status: shipped
---

# US-0400: Fan-Out Enrichment at Ingest

**System Types:** dpkms (self-hosted), dpkms cloud
**Personas:** [Agents/LLMs](../../personas/agents-llms-tools.md),
[Knowledge Workers](../../personas/knowledge-workers.md),
[Maintainers](../../personas/maintainers.md)

---

## User Goal

As a knowledge worker, I want a single ingested source to
automatically produce multiple downstream effects — entity page
updates, concept page updates, cross-references, and changelog
entries — so my knowledge base compounds rather than just
accumulates.

---

## Context

Current ingest is 1:1 (input -> object). Karpathy's wiki pattern
shows 1 source -> 10-15 wiki touches. Fan-out transforms ctxt
from "smart filing cabinet" to "knowledge compiler". Aligns with
ADR-037 (AMA) multi-agent coordination for parallel enrichment.

---

## Acceptance Criteria

- [ ] Single ingest triggers a fan-out pipeline producing N
  downstream effects (entity updates, concept updates,
  cross-refs, log entry)
- [ ] Entity pages are created or updated when new mentions of
  known entities are detected
- [ ] Concept pages are created or updated when content matches
  existing concept clusters
- [ ] Cross-references between the new object and existing
  objects are bidirectionally linked
- [ ] An append-only changelog entry records all mutations from
  the fan-out
- [ ] Fan-out is configurable per profile (which downstream
  effects to trigger)
- [ ] Fan-out runs asynchronously; ingest returns immediately
  with job tracking
- [ ] Idempotent — re-ingesting same content does not duplicate
  downstream effects

---

## Implementation Notes

### Pipeline Design

```
ingest(source)
  -> create knowledge_object
  -> emit fan_out_event(object_id)
  -> parallel:
       extract_entities(object_id)
         -> for each entity:
              upsert entity_page(entity, object_id)
              create cross_ref(object_id, entity_page_id)
       extract_concepts(object_id)
         -> for each concept:
              upsert concept_page(concept, object_id)
              create cross_ref(object_id, concept_page_id)
       append_changelog(object_id, mutations[])
  -> mark fan_out complete
```

### CLI Interface

```bash
# Ingest with fan-out (default when configured)
ctxt "meeting notes about auth redesign" --wait

# Ingest without fan-out
ctxt "quick note" --no-fanout

# Check fan-out status
ctxt jobs <job_id>

# Configure fan-out for profile
ctxt profile update research --fanout entities,concepts,xrefs
```

### REST API

```
POST /objects
Content-Type: application/json

{
  "content": "...",
  "fanout": true,
  "fanout_steps": ["entities", "concepts", "xrefs", "changelog"]
}

-> 202 Accepted
{
  "object_id": "o-abc123",
  "fanout_job_id": "j-def456",
  "status": "pending"
}
```

---

## E2E Test Checklist

- [ ] Ingest text mentioning 3 known entities -> verify 3
  entity pages updated
- [ ] Ingest text with new concept -> verify concept page
  created
- [ ] Verify cross-references are bidirectional (object ->
  entity, entity -> object)
- [ ] Verify changelog entry lists all mutations
- [ ] Re-ingest same content -> no duplicate effects
- [ ] Ingest with `--no-fanout` -> no downstream effects
- [ ] Profile with `fanout: [entities]` only -> no concept
  pages created
- [ ] Fan-out job completes within timeout

---

## Related Stories

- US-0009: Extract entities and mentions (prerequisite)
- US-0046: Extract relationships between entities
- US-0063: Unified graph extraction at ingest
- US-0401: Persistent composed pages
- US-0405: Append-only knowledge changelog

---

## E2E Tests

- `test/integration/us0400_fan_out_test.go::TestUS0400_FanOutCreatesEdges`
- `test/integration/us0400_fan_out_test.go::TestUS0400_FanOutIdempotent`
- `test/integration/us0400_fan_out_test.go::TestUS0400_FanOutAuditLog`
- `test/integration/us0400_fan_out_test.go::TestUS0400_NoFanoutFlag`
- `test/integration/us0400_fan_out_test.go::TestUS0400_FanOutCompletesInTime`
