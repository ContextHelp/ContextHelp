# US-0059: Compose Recommendation Document

**System Types:** ctxt
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md)

---

## User Goal

As a knowledge worker, I want to compose a recommendation document from a set of knowledge objects so I can present structured, evidence-backed recommendations to a specific audience without manually synthesising each source.

---

## Context

After gathering context through search, enrichment, and analysis, a knowledge worker often needs to produce a formal recommendation — for an executive, a technical review board, or a project team. Automated recommendation generation from source objects ensures each recommendation is grounded in the knowledge store, carries confidence levels, and is framed appropriately for the intended audience.

---

## Acceptance Criteria

- [ ] User can provide source object IDs (`--from <object-ids>`)
- [ ] User can specify a goal statement to focus recommendations (`--goal <text>`)
- [ ] User can target a specific audience (`--audience <role>`, e.g., `executive`, `technical`)
- [ ] User can select an output template (`--template <name>`)
- [ ] User can apply a focus profile (`--profile <profile>`)
- [ ] User can request a specific output format (`--format <format>`)
- [ ] Output includes: executive summary, recommendations list, rationale, risks, and next steps
- [ ] Each recommendation includes a confidence level and supporting source references
- [ ] Each recommendation links to at least one source object in provenance
- [ ] `--goal <text>` scopes recommendations to the stated goal (reflected in framing)
- [ ] `--audience executive` produces abbreviated, impact-focused recommendations
- [ ] `--audience technical` produces detailed, implementation-focused recommendations
- [ ] Open decisions from source objects generate concrete action recommendations
- [ ] Closed/superseded decisions appear as context, not action items
- [ ] Non-existent object IDs return 404 with descriptive error
- [ ] Empty object set returns 400 with descriptive error
- [ ] Invalid `--audience` value returns 400 with list of valid values

---

## Implementation Notes

### CLI Interface

```bash
# Basic recommendation document
ctxt make recommendation --from o-abc123,o-def456

# With goal and audience
ctxt make recommendation --from o-abc123 --goal "Reduce deployment risk" --audience executive

# With template and profile
ctxt make recommendation --from o-abc123 --template decision-review --profile engineering

# Returns JSON with recommendation document
{
  "document_id": "rd-xyz789",
  "executive_summary": "...",
  "recommendations": [
    {
      "id": "rec-1",
      "title": "Adopt blue-green deployment",
      "confidence": "HIGH",
      "rationale": "Three incidents traced to deployment rollbacks",
      "risks": ["Requires infrastructure changes"],
      "source_ids": ["d-abc123"]
    }
  ],
  "next_steps": [...],
  "provenance": {
    "source_objects": ["o-abc123"],
    "goal": "Reduce deployment risk",
    "generated_at": "2025-01-18T10:30:45Z"
  }
}
```

### REST API

```
POST /compositions/recommendation
Content-Type: application/json

{
  "object_ids": ["o-abc123", "o-def456"],
  "goal": "Reduce deployment risk",
  "audience": "executive",
  "template": "decision-review",
  "profile": "engineering",
  "format": "markdown"
}

→ 200 OK  (document persisted; retrievable via GET /compositions/recommendation/{id})
→ 400 Bad Request  (empty object set or invalid audience value)
→ 404 Not Found    (non-existent object IDs)
```

---

## E2E Test Checklist

### CLI → Server payload propagation
- [ ] `--from <object-ids>` sends `object_ids` array in POST body
- [ ] `--goal <text>` sends `goal` in POST body when provided
- [ ] `--audience <role>` sends `audience` in POST body when provided
- [ ] `--template <name>` sends `template` in POST body when provided
- [ ] `--profile <profile>` sends `profile` in POST body when provided
- [ ] `--format <format>` sends `format` in POST body when provided

### Server-side receipt and storage
- [ ] POST /compositions/recommendation persists document; GET /{id} returns same record
- [ ] Stored record contains `object_ids`, `goal`, `audience`, `template` as originally submitted
- [ ] Recommendation sections stored and retrievable from the record
- [ ] Provenance stored: source objects with IDs and role (supporting/opposing each recommendation)

### CLI output validation
- [ ] `ctxt make recommendation --from <ids>` exits 0 and returns valid recommendation JSON
- [ ] Output includes: executive summary, recommendations list, rationale, risks, next steps
- [ ] Each recommendation includes confidence level and supporting source references
- [ ] Document generation completes in acceptable time

### Content correctness
- [ ] Each recommendation links to at least one source object in provenance
- [ ] `--goal <text>` scopes recommendations to the stated goal (reflected in framing)
- [ ] `--audience executive` produces abbreviated, impact-focused recommendations
- [ ] `--audience technical` produces detailed, implementation-focused recommendations
- [ ] Open decisions from source objects generate concrete action recommendations
- [ ] Closed/superseded decisions reflected as context, not action items

### Error handling
- [ ] Non-existent object IDs return 404 with descriptive error
- [ ] Empty object set returns 400 with descriptive error
- [ ] Invalid `--audience` value returns 400 with list of valid values

---

## Related Stories

- [US-0022: generate-brief-from-objects](./US-0022-generate-brief-from-objects.md) — Brief as foundation for recommendation document
- [US-0023: generate-plan-from-decisions](./US-0023-generate-plan-from-decisions.md) — Plan derived from recommendations
- [US-0057: compose-stakeholder-analysis](./US-0057-compose-stakeholder-analysis.md) — Stakeholder context informs audience framing
- [US-0058: compose-impact-assessment](./US-0058-compose-impact-assessment.md) — Impact data grounds recommendations
- [US-0026: share-composition-with-team](./US-0026-share-composition-with-team.md) — Share the recommendation document
- [US-0060: compose-with-custom-template](./US-0060-compose-with-custom-template.md) — Custom template for recommendation output

---

## Personas

- [Knowledge Workers](../../personas/knowledge-workers.md)
- [Researchers / OSINT](../../personas/researchers-osint.md)

---

## E2E Tests

> Not yet implemented.
