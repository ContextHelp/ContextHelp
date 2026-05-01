---
status: shipped
---

# US-0058: Compose Impact Assessment

**System Types:** ctxt
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md)

---

## User Goal

As a knowledge worker, I want to compose an impact assessment from a set of decisions or objects so I can understand which entities, teams, and systems are affected and at what severity level.

---

## Context

Decisions often have downstream effects that are not immediately obvious from the decision record alone. An impact assessment aggregates the impact levels declared in decisions, resolves which entities are affected, and optionally traverses the graph to surface second-order effects. This helps knowledge workers anticipate consequences before communicating or executing a decision.

---

## Acceptance Criteria

- [ ] User can provide source object IDs (`--from <object-ids>`) or a single decision ID (`--decision <id>`)
- [ ] User can scope the assessment by area (`--scope <scope>`, e.g., `team`, `org`, `product`)
- [ ] User can request second-order effects via graph traversal depth (`--depth <n>`)
- [ ] User can request a specific output format (`--format <format>`)
- [ ] Output includes an impact summary: HIGH/MEDIUM/LOW counts
- [ ] Each impact entry includes: affected entity, level, rationale, and source reference
- [ ] Impact levels (HIGH/MEDIUM/LOW) map to the `impact` field values from source decision objects
- [ ] `--scope team` limits affected entities to team-level entities only
- [ ] `--depth 2` includes second-order effects via graph traversal
- [ ] Entities with no affected status are excluded from the impact list
- [ ] Contradictory impacts (same entity, different levels) are surfaced as conflicts
- [ ] Assessment provenance stores source objects, traversal depth, and `generated_at`
- [ ] Non-existent object IDs return 404 with descriptive error
- [ ] No decisions in source objects returns empty impact list (not an error)
- [ ] Invalid `--scope` value returns 400 with list of valid scopes

---

## Implementation Notes

### CLI Interface

```bash
# Impact assessment from a set of objects
ctxt make impact-assessment --from o-abc123,o-def456

# From a single decision, with scope
ctxt make impact-assessment --decision d-abc123 --scope team

# Include second-order effects
ctxt make impact-assessment --from o-abc123 --depth 2

# Returns JSON with assessment
{
  "assessment_id": "ia-xyz789",
  "summary": {"HIGH": 2, "MEDIUM": 1, "LOW": 0},
  "impacts": [
    {
      "affected_entity": "@team.backend",
      "level": "HIGH",
      "rationale": "Decision defers infrastructure work owned by backend team",
      "source_ids": ["d-abc123"]
    }
  ],
  "conflicts": [],
  "provenance": {
    "source_objects": ["o-abc123"],
    "traversal_depth": 2,
    "generated_at": "2025-01-18T10:30:45Z"
  }
}
```

### REST API

```
POST /compositions/impact-assessment
Content-Type: application/json

{
  "object_ids": ["o-abc123", "o-def456"],
  "decision_id": "d-abc123",
  "scope": "team",
  "depth": 2,
  "format": "markdown"
}

→ 200 OK  (assessment persisted; retrievable via GET /compositions/impact-assessment/{id})
→ 400 Bad Request  (invalid scope value)
→ 404 Not Found    (non-existent object IDs)
```

---

## E2E Test Checklist

### CLI → Server payload propagation
- [ ] `--from <object-ids>` sends `object_ids` array in POST body
- [ ] `--decision <id>` sends `decision_id` in POST body when provided
- [ ] `--scope <scope>` sends `scope` in POST body when provided (e.g., team, org, product)
- [ ] `--depth <n>` sends `depth` (second-order effect traversal depth) in POST body when provided
- [ ] `--format <format>` sends `format` in POST body when provided

### Server-side receipt and storage
- [ ] POST /compositions/impact-assessment persists assessment; GET /{id} returns same record
- [ ] Stored record contains `object_ids`/`decision_id` and `scope` as originally submitted
- [ ] Each impact entry stored with: affected_entity, impact_level, rationale, source_ids
- [ ] Assessment provenance stored: source objects, traversal depth, generated_at

### CLI output validation
- [ ] `ctxt make impact-assessment --from <ids>` exits 0 and returns valid assessment JSON
- [ ] Output includes impact summary: HIGH/MEDIUM/LOW counts
- [ ] Each impact entry includes affected entity, level, rationale, and source reference
- [ ] Assessment generation completes in acceptable time

### Content correctness
- [ ] Impact levels (HIGH/MEDIUM/LOW) map to decision impact field values from source objects
- [ ] `--scope team` limits affected entities to team-level entities only
- [ ] `--depth 2` includes second-order effects via graph traversal
- [ ] Entities with no affected status excluded from impact list
- [ ] Contradictory impacts (same entity, different levels) surfaced as conflicts

### Error handling
- [ ] Non-existent object IDs return 404 with descriptive error
- [ ] No decisions in source objects returns empty impact list (not error)
- [ ] Invalid `--scope` value returns 400 with list of valid scopes

---

## Related Stories

- [US-0010: extract-decisions-and-tasks](../enrichment/US-0010-extract-decisions-and-tasks.md) — Source decisions for impact assessment
- [US-0024: compose-with-graph-traversal](./US-0024-compose-with-graph-traversal.md) — Graph traversal for second-order effects
- [US-0023: generate-plan-from-decisions](./US-0023-generate-plan-from-decisions.md) — Plan from decisions assessed for impact
- [US-0056: compose-decision-timeline](./US-0056-compose-decision-timeline.md) — Timeline of decisions to assess
- [US-0059: compose-recommendation-document](./US-0059-compose-recommendation-document.md) — Impact informs recommendations

---

## Personas

- [Knowledge Workers](../../personas/knowledge-workers.md)
- [Researchers / OSINT](../../personas/researchers-osint.md)

---

## E2E Tests

- `test/integration/us0058_impact_assessment_test.go::TestUS0058_HighImpactDecisionPreserved`
- `test/integration/us0058_impact_assessment_test.go::TestUS0058_MultipleImpactLevelsPreserved`
- `test/integration/us0058_impact_assessment_test.go::TestUS0058_ImpactAssessmentCompositionIncludesImpactLevels`
- `test/integration/us0058_impact_assessment_test.go::TestUS0058_NoDecisionsInObjectProducesEmptyImpactList`
