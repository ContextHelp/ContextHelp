---
status: shipped
---

# US-0401: Persistent Composed Pages

**System Types:** dpkms (self-hosted), dpkms cloud, ctxt
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md),
[Agents/LLMs](../../personas/agents-llms-tools.md)

---

## User Goal

As a knowledge worker, I want entity and concept summaries to
persist and update incrementally — not re-derive from scratch on
every query — so my knowledge compounds over time and subtle
multi-document synthesis is pre-computed.

---

## Context

`ctxt make brief` today is ephemeral — generates on demand,
discards result. Karpathy's wiki pattern shows that persistent
pages (entity pages, concept pages) eliminate redundant
re-derivation and enable synthesis that spans dozens of sources.
Classic RAG rediscovers knowledge every query; persistent pages
solve this.

---

## Acceptance Criteria

- [ ] Entity pages are first-class objects with type
  `entity_page` stored in the knowledge graph
- [ ] Concept pages are first-class objects with type
  `concept_page` stored in the knowledge graph
- [ ] Pages update incrementally when new evidence arrives
  (append/revise, not regenerate)
- [ ] Pages track their source objects (provenance chain)
- [ ] Pages are searchable via `ctxt find` like any other
  object
- [ ] Pages include a revision history (what changed and why)
- [ ] Stale pages are flagged when source objects are deleted
  or updated
- [ ] Manual edits to pages are preserved across incremental
  updates

---

## Implementation Notes

### Page Structure

```
entity_page {
  id: "ep-alice-chen"
  type: "entity_page"
  entity: "@person.alice-chen"
  content: "compiled summary..."
  sources: ["o-abc", "o-def", "o-ghi"]
  revision_count: 7
  last_revised: "2026-04-20T..."
  revision_log: [
    { date, source_id, delta_summary }
  ]
}
```

### CLI Interface

```bash
# View entity page
ctxt page show @person.alice-chen

# List all entity pages
ctxt page list --type entity

# Force refresh from sources
ctxt page refresh @person.alice-chen

# Create concept page manually
ctxt page create "auth-patterns" --type concept \
  --sources o-abc,o-def
```

### Incremental Update Flow

```
new_object ingested
  -> fan-out detects entity @person.alice-chen
  -> load existing entity_page ep-alice-chen
  -> diff: what does new_object add?
  -> append delta to page content
  -> update sources list
  -> append revision_log entry
  -> re-embed page (new vector)
```

---

## E2E Test Checklist

- [ ] Ingest 3 objects mentioning same entity -> entity page
  created with synthesized content from all 3
- [ ] Ingest 4th object mentioning entity -> page updated
  incrementally, not regenerated
- [ ] `ctxt find "alice chen"` returns entity page in results
- [ ] Delete source object -> entity page flagged as
  potentially stale
- [ ] `ctxt page refresh` regenerates from current sources
- [ ] Revision log shows all incremental updates
- [ ] Manual edit to page content preserved after next
  incremental update

---

## Related Stories

- US-0400: Fan-out enrichment at ingest (triggers page updates)
- US-0022: Generate brief from objects (ephemeral predecessor)
- US-0406: Knowledge lint and health check
- US-0402: Index-first retrieval

---

## E2E Tests

- `test/integration/us0401_persistent_pages_test.go::TestUS0401_IngestCreatesEntityPage`
- `test/integration/us0401_persistent_pages_test.go::TestUS0401_IncrementalUpdate`
- `test/integration/us0401_persistent_pages_test.go::TestUS0401_PageSearchable`
- `test/integration/us0401_persistent_pages_test.go::TestUS0401_RevisionLogShowsAllUpdates`
