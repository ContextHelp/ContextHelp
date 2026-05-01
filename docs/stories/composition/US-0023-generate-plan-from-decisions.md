---
status: shipped
---

# US-0023: Generate Plan From Decisions

**System Types:** ctxt
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md)

---

## User Goal

As a knowledge worker, I want to generate an actionable plan from a set of decisions so I can turn resolved or open decisions into concrete tasks, milestones, and owners without manually re-reading each decision.

---

## Context

After a decision-heavy meeting or sprint review, a knowledge worker has a collection of decisions in the knowledge store — some open, some closed. Manually extracting tasks and sequencing them into a coherent plan is error-prone and time-consuming. Automated plan generation from decisions ensures no action item is lost and provides full traceability back to the originating decision.

---

## Acceptance Criteria

- [ ] User can generate a plan from specific decision IDs (`--from d-abc123,d-def456`)
- [ ] User can generate a plan by filtering decisions by tag (`--tag <tag>`)
- [ ] User can specify a planning horizon (`--horizon <value>`, e.g., `sprint`, `quarter`)
- [ ] User can select an output format (`--format <format>`)
- [ ] User can apply a focus profile (`--profile <profile>`)
- [ ] Generated plan includes: tasks, milestones, owners, and timeline
- [ ] Each plan task traces back to at least one source decision in provenance
- [ ] Impact level from source decisions (HIGH/MEDIUM/LOW) maps to plan task priority
- [ ] Open decisions produce actionable tasks; closed/superseded decisions appear as context
- [ ] Duplicate or conflicting decisions are surfaced as conflicts or merged tasks
- [ ] Plan generation completes in acceptable time
- [ ] Non-existent decision IDs return 404; empty decision set returns 400
- [ ] Mixed valid/invalid IDs: valid decisions processed, invalid IDs listed in error field

---

## Implementation Notes

### CLI Interface

```bash
# Generate plan from specific decisions
ctxt make plan --from d-abc123,d-def456

# Generate plan from tagged decisions
ctxt make plan --tag sprint-23

# With planning horizon and profile
ctxt make plan --from d-abc123 --horizon quarter --profile engineering

# With output format
ctxt make plan --from d-abc123 --format markdown

# Returns JSON with plan content
{
  "plan_id": "p-xyz789",
  "tasks": [...],
  "milestones": [...],
  "owners": [...],
  "timeline": {...},
  "provenance": {
    "source_decisions": [...],
    "generated_at": "2025-01-18T10:30:45Z"
  }
}
```

### REST API

```
POST /compositions/plan
Content-Type: application/json

{
  "decision_ids": ["d-abc123", "d-def456"],
  "tag": "sprint-23",
  "horizon": "quarter",
  "profile": "engineering",
  "format": "markdown"
}

→ 200 OK
{
  "plan_id": "p-xyz789",
  "tasks": [
    {
      "id": "task-1",
      "title": "...",
      "priority": "HIGH",
      "owner": "@person.alice-smith",
      "source_decision": "d-abc123"
    }
  ],
  "milestones": [...],
  "provenance": {
    "source_decisions": [
      {"id": "d-abc123", "title": "...", "created_at": "2025-01-15T09:00:00Z"}
    ],
    "generated_at": "2025-01-18T10:30:45Z"
  }
}

→ 400 Bad Request  (empty decision set)
→ 404 Not Found    (non-existent decision ID)
```

---

## E2E Test Checklist

### CLI → Server payload propagation
- [ ] `--from d-abc123,d-def456` sends `decision_ids: ["d-abc123","d-def456"]` in POST body
- [ ] `--tag <tag>` sends `tag` filter in POST body when provided
- [ ] `--horizon <value>` sends `horizon` in POST body when provided
- [ ] `--profile <profile>` sends `profile` in POST body when provided
- [ ] `--format <format>` sends `format` in POST body when provided

### Server-side receipt and storage
- [ ] POST /compositions/plan persists plan; GET /compositions/plan/{plan_id} returns same record
- [ ] Stored plan record contains `decision_ids` as originally submitted
- [ ] Generated plan tasks/milestones are stored and retrievable from the plan record
- [ ] Plan provenance stored: source decisions listed with IDs and timestamps

### CLI output validation
- [ ] `ctxt make plan --from d-abc123` exits 0 and returns valid plan JSON
- [ ] Plan includes sections: tasks, milestones, owners, timeline (or equivalent)
- [ ] Plan includes provenance (source decisions, generated_at timestamp)
- [ ] Plan generation completes in acceptable time

### Content correctness
- [ ] Each plan task traces back to at least one source decision in provenance
- [ ] Impact level from decisions (HIGH/MEDIUM/LOW) reflected in plan priority
- [ ] Open decisions contribute actionable tasks; closed decisions reflected as context only
- [ ] Duplicate/conflicting decisions surfaced as conflicts or merged tasks

### Error handling
- [ ] Non-existent decision IDs return 404 with descriptive error
- [ ] Empty decision set returns 400 with descriptive error
- [ ] Mixed valid/invalid IDs: valid decisions processed, invalid IDs listed in error field

---

## Related Stories

- [US-0010: extract-decisions-and-tasks](../enrichment/US-0010-extract-decisions-and-tasks.md) — Source decisions for plan
- [US-0022: generate-brief-from-objects](./US-0022-generate-brief-from-objects.md) — Brief generation from objects
- [US-0025: export-brief-to-markdown-pdf](./US-0025-export-brief-to-markdown-pdf.md) — Export generated plan
- [US-0026: share-composition-with-team](./US-0026-share-composition-with-team.md) — Share generated plan
- [US-0058: compose-impact-assessment](./US-0058-compose-impact-assessment.md) — Assess impact of decisions in plan

---

## Personas

- [Knowledge Workers](../../personas/knowledge-workers.md)
- [Researchers / OSINT](../../personas/researchers-osint.md)

---

## E2E Tests

- `test/integration/us0023_generate_plan_test.go::TestUS0023_PlanPreservesDecisionTaskStructure`
- `test/integration/us0023_generate_plan_test.go::TestUS0023_PlanCompositionContainsDecisionAndTaskContent`
- `test/integration/us0023_generate_plan_test.go::TestUS0023_HighImpactDecisionAppearsInPlan`
