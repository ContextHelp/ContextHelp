# US-0407: Source-Scoped Metadata Facet Search

**System Types:** dpkms (self-hosted), dpkms cloud, ctxt
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md),
[Agents/LLMs](../../personas/agents-llms-tools.md)

---

## User Goal

As a knowledge worker, I want to filter search results by
structured metadata facets — type, topic, person, date range,
source — as first-class query parameters, not just text
matching.

---

## Context

OB1 uses JSONB metadata filtering alongside semantic search:
filter by type, topic, person, date range. ctxt has `--profile`
and `--tag` but lacks granular faceted search on structured
metadata fields. Prerequisite: US-0403 (structured metadata
extraction) populates the facets.

---

## Acceptance Criteria

- [ ] `ctxt find` accepts `--type`, `--topic`, `--person`,
  `--since`, `--until`, `--source` as filter parameters
- [ ] Filters compose with semantic search (intersection)
- [ ] Filters compose with each other (AND semantics)
- [ ] `ctxt list` accepts same filter parameters for browsing
- [ ] Facet counts returned with results
  (e.g., "12 tasks, 8 observations, 3 decisions")
- [ ] RSQL queries support metadata field predicates
- [ ] Facet values autocomplete from existing metadata

---

## Implementation Notes

### CLI Interface

```bash
# Filter by type
ctxt find "auth" --type decision

# Filter by person
ctxt find "redesign" --person alice-chen

# Filter by date range
ctxt find "deployment" --since 2026-04-01 --until 2026-04-23

# Filter by source
ctxt list --source slack --type task

# Combined
ctxt find "auth" --type decision --person alice-chen \
  --since 2026-04-01

# Facet counts
ctxt find "auth" --facets
# -> tasks: 12, observations: 8, decisions: 3
```

### RSQL Extension

```
# Metadata field predicates
type==decision;topics=in=(auth,security)
people=has=@person.alice-chen
dates_mentioned=gt=2026-04-01
```

---

## E2E Test Checklist

- [ ] `--type task` returns only task-typed objects
- [ ] `--person alice-chen` returns only objects mentioning
  that person
- [ ] `--since` / `--until` filter by dates_mentioned
- [ ] Combined filters intersect correctly
- [ ] Semantic search + filters work together
- [ ] `--facets` returns count breakdown by type
- [ ] RSQL metadata predicates match CLI filters
- [ ] Autocomplete suggests existing facet values

---

## Related Stories

- US-0403: Structured metadata extraction (populates facets)
- US-0016: Natural language search (base search)
- US-0017: Structured RSQL query (RSQL integration)
- US-0020: Apply focus profile to search (profile-level filter)
