---
status: shipped
---

# US-0056: Compose Decision Timeline

**System Types:** ctxt
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md)

---

## User Goal

As a knowledge worker, I want to compose a chronological timeline of decisions so I can understand how a project or topic evolved over time and communicate that history to others.

---

## Context

Decision records accumulate over weeks and months across meetings, documents, and threads. A chronological timeline gives stakeholders a clear narrative of what was decided, when, and by whom — especially useful during retrospectives, audits, or onboarding. Filtering by date range, tag, or entity allows a timeline to be scoped to exactly the context that matters.

---

## Acceptance Criteria

- [ ] User can filter the timeline by tag (`--tag <tag>`)
- [ ] User can filter by date range (`--from-date <date>`, `--to-date <date>`)
- [ ] User can filter by entity URI (`--entity <uri>`)
- [ ] User can request a specific output format (`--format <format>`)
- [ ] Timeline entries are sorted by date ascending by default
- [ ] Each timeline entry includes: decision ID, title, date, impact, status
- [ ] `--from-date` excludes decisions before that date
- [ ] `--to-date` excludes decisions after that date
- [ ] `--tag <tag>` limits timeline to decisions with that tag only
- [ ] `--entity <uri>` limits timeline to decisions mentioning that entity
- [ ] Combined filters narrow results with AND semantics
- [ ] Each timeline entry references its source decision ID in provenance
- [ ] No decisions matching filters returns an empty timeline (not an error)
- [ ] Invalid date format returns 400 with descriptive error
- [ ] Non-existent entity URI returns 404 with descriptive error

---

## Implementation Notes

### CLI Interface

```bash
# Full timeline
ctxt make timeline

# Scoped to a tag and date range
ctxt make timeline --tag architecture --from-date 2025-01-01 --to-date 2025-03-31

# Scoped to a specific entity
ctxt make timeline --entity @person.alice-smith

# With output format
ctxt make timeline --format markdown

# Returns JSON with timeline entries
{
  "timeline_id": "tl-xyz789",
  "entries": [
    {
      "decision_id": "d-abc123",
      "title": "Defer infrastructure refactor",
      "date": "2025-01-15",
      "impact": "HIGH",
      "status": "OPEN"
    }
  ],
  "provenance": {
    "filters": {"tag": "architecture", "from_date": "2025-01-01"},
    "generated_at": "2025-01-18T10:30:45Z"
  }
}
```

### REST API

```
POST /compositions/timeline
Content-Type: application/json

{
  "tag": "architecture",
  "from_date": "2025-01-01",
  "to_date": "2025-03-31",
  "entity": "@person.alice-smith",
  "format": "markdown"
}

→ 200 OK  (timeline persisted; retrievable via GET /compositions/timeline/{id})
→ 400 Bad Request  (invalid date format)
→ 404 Not Found    (non-existent entity URI)
```

---

## E2E Test Checklist

### CLI → Server payload propagation
- [ ] `--tag <tag>` sends `tag` filter in POST body when provided
- [ ] `--from-date <date>` sends `from_date` in POST body when provided
- [ ] `--to-date <date>` sends `to_date` in POST body when provided
- [ ] `--entity <uri>` sends `entity` filter in POST body when provided
- [ ] `--format <format>` sends `format` in POST body when provided

### Server-side receipt and storage
- [ ] POST /compositions/timeline persists timeline; GET /compositions/timeline/{id} returns same record
- [ ] Stored timeline record contains filter params (`tag`, `from_date`, `to_date`, `entity`) as submitted
- [ ] Timeline entries stored in chronological order with decision IDs and timestamps
- [ ] Provenance stored: each timeline entry references its source decision ID

### CLI output validation
- [ ] `ctxt make timeline` exits 0 and returns valid timeline JSON
- [ ] Timeline entries are sorted by date ascending by default
- [ ] Each entry includes: decision ID, title, date, impact, status
- [ ] Timeline generation completes in acceptable time

### Content correctness
- [ ] `--from-date` excludes decisions before that date
- [ ] `--to-date` excludes decisions after that date
- [ ] `--tag <tag>` limits timeline to decisions with that tag only
- [ ] `--entity <uri>` limits timeline to decisions mentioning that entity
- [ ] Combined filters narrow results correctly (AND semantics)

### Error handling
- [ ] Invalid date format for `--from-date`/`--to-date` returns 400 with descriptive error
- [ ] No decisions matching filters returns empty timeline (not error)
- [ ] Non-existent entity URI returns 404 with descriptive error

---

## Related Stories

- [US-0010: extract-decisions-and-tasks](../enrichment/US-0010-extract-decisions-and-tasks.md) — Source decisions for timeline
- [US-0022: generate-brief-from-objects](./US-0022-generate-brief-from-objects.md) — Timeline as input to brief generation
- [US-0023: generate-plan-from-decisions](./US-0023-generate-plan-from-decisions.md) — Plan from decisions in the timeline
- [US-0026: share-composition-with-team](./US-0026-share-composition-with-team.md) — Share the composed timeline
- [US-0058: compose-impact-assessment](./US-0058-compose-impact-assessment.md) — Assess impact of decisions in timeline

---

## Personas

- [Knowledge Workers](../../personas/knowledge-workers.md)
- [Researchers / OSINT](../../personas/researchers-osint.md)

---

## E2E Tests

- `test/integration/us0056_decision_timeline_test.go::TestUS0056_DecisionObjectsRetainTimestamp`
- `test/integration/us0056_decision_timeline_test.go::TestUS0056_TimelineCompositionPreservesChronologicalOrder`
- `test/integration/us0056_decision_timeline_test.go::TestUS0056_EmptyDecisionSetProducesEmptyTimeline`
- `test/integration/us0056_decision_timeline_test.go::TestUS0056_DecisionStatusPreservedInTimeline`
