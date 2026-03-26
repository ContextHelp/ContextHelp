# US-0057: Compose Stakeholder Analysis

**System Types:** ctxt
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md)

---

## User Goal

As a knowledge worker, I want to compose a stakeholder analysis from a set of knowledge objects so I can understand who is involved in decisions and how frequently, and communicate that to project leads or executives.

---

## Context

Stakeholder involvement is often implicit — spread across meeting notes, decision records, and email threads. Composing a stakeholder analysis that aggregates entity appearances across source objects makes this visible in one place. Filtering by role or entity, and optionally including indirect stakeholders discovered via graph traversal, gives knowledge workers the depth of analysis they need.

---

## Acceptance Criteria

- [ ] User can provide source object IDs (`--from <object-ids>`)
- [ ] User can filter analysis to a specific entity (`--entity <uri>`)
- [ ] User can filter stakeholders by role (`--role <role>`)
- [ ] User can request inclusion of indirect stakeholders from graph neighbors (`--include-indirect`)
- [ ] User can request a specific output format (`--format <format>`)
- [ ] Output includes stakeholder list with entity URIs, names, roles
- [ ] Each stakeholder entry includes the count of decisions/objects they appear in
- [ ] Each stakeholder traces to at least one source object via provenance
- [ ] `--entity <uri>` limits analysis to a specific person/entity
- [ ] `--role <role>` filters stakeholders by role (e.g., decision-maker, contributor)
- [ ] `--include-indirect` adds entities mentioned in related objects (graph neighbors)
- [ ] Stakeholders are sorted by involvement count (descending) by default
- [ ] Object set with no entity mentions returns empty stakeholder list (not an error)
- [ ] Non-existent object IDs return 404 with descriptive error
- [ ] Invalid entity URI format returns 400 with descriptive error

---

## Implementation Notes

### CLI Interface

```bash
# Stakeholder analysis from a set of objects
ctxt make stakeholder-analysis --from o-abc123,o-def456

# Filter to a specific entity
ctxt make stakeholder-analysis --from o-abc123 --entity @person.alice-smith

# Include indirect stakeholders
ctxt make stakeholder-analysis --from o-abc123 --include-indirect

# Filter by role
ctxt make stakeholder-analysis --from o-abc123 --role decision-maker

# Returns JSON with stakeholder analysis
{
  "analysis_id": "sa-xyz789",
  "stakeholders": [
    {
      "entity_uri": "@person.alice-smith",
      "name": "Alice Smith",
      "role": "decision-maker",
      "involvement_count": 5,
      "decisions": ["d-abc123", "d-def456"]
    }
  ],
  "provenance": {
    "source_objects": ["o-abc123", "o-def456"],
    "generated_at": "2025-01-18T10:30:45Z"
  }
}
```

### REST API

```
POST /compositions/stakeholder-analysis
Content-Type: application/json

{
  "object_ids": ["o-abc123", "o-def456"],
  "entity_filter": "@person.alice-smith",
  "role_filter": "decision-maker",
  "include_indirect": true,
  "format": "markdown"
}

→ 200 OK  (analysis persisted; retrievable via GET /compositions/stakeholder-analysis/{id})
→ 400 Bad Request  (invalid entity URI format)
→ 404 Not Found    (non-existent object IDs)
```

---

## E2E Test Checklist

### CLI → Server payload propagation
- [ ] `--from <object-ids>` sends `object_ids` array in POST body
- [ ] `--entity <uri>` sends `entity_filter` in POST body when provided
- [ ] `--role <role>` sends `role_filter` in POST body when provided
- [ ] `--include-indirect` sends `include_indirect: true` in POST body when flag present
- [ ] `--format <format>` sends `format` in POST body when provided

### Server-side receipt and storage
- [ ] POST /compositions/stakeholder-analysis persists analysis; GET /{id} returns same record
- [ ] Stored record contains `object_ids` as originally submitted
- [ ] Each stakeholder entry stored with: entity URI, name, role, involvement_count, decisions_list
- [ ] Analysis provenance stored: source objects and extraction timestamp

### CLI output validation
- [ ] `ctxt make stakeholder-analysis --from <ids>` exits 0 and returns valid analysis JSON
- [ ] Output includes stakeholder list with entity URIs, names, roles
- [ ] Each stakeholder entry includes count of decisions/objects they appear in
- [ ] Analysis generation completes in acceptable time

### Content correctness
- [ ] Each stakeholder traces to at least one source object via provenance
- [ ] `--entity <uri>` limits analysis to a specific person/entity
- [ ] `--role <role>` filters stakeholders by role (e.g., decision-maker, contributor)
- [ ] `--include-indirect` adds entities mentioned in related objects (graph neighbors)
- [ ] Stakeholders sorted by involvement count (descending) by default

### Error handling
- [ ] Non-existent object IDs return 404 with descriptive error
- [ ] Object set with no entity mentions returns empty stakeholder list (not error)
- [ ] Invalid entity URI format returns 400 with descriptive error

---

## Related Stories

- [US-0009: extract-entities-and-mentions](../enrichment/US-0009-extract-entities-and-mentions.md) — Source of entity mentions
- [US-0022: generate-brief-from-objects](./US-0022-generate-brief-from-objects.md) — Stakeholder list used in brief participants section
- [US-0024: compose-with-graph-traversal](./US-0024-compose-with-graph-traversal.md) — Graph traversal for indirect stakeholders
- [US-0026: share-composition-with-team](./US-0026-share-composition-with-team.md) — Share the stakeholder analysis
- [US-0059: compose-recommendation-document](./US-0059-compose-recommendation-document.md) — Stakeholders inform recommendation audience

---

## Personas

- [Knowledge Workers](../../personas/knowledge-workers.md)
- [Researchers / OSINT](../../personas/researchers-osint.md)

---

## E2E Tests

> Not yet implemented.
