# Story: Generate Brief from Objects

**System Types:** ctxt
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md)

---

## User Goal

As a knowledge worker, I want to generate a summary brief from selected knowledge objects with full provenance and context so I can share organized insights with others.

---

## Context

Knowledge workers often need to compile insights, decisions, and related context into cohesive briefs for stakeholders, teams, or future reference. Manual compilation is tedious and loses traceability. Automated brief generation from structured knowledge objects saves time and improves consistency.

---

## Acceptance Criteria

- [ ] User can generate brief from specific object IDs
- [ ] User can generate brief from query results (e.g., all decisions tagged 'urgent')
- [ ] User can select brief template (default, executive, technical, chronological)
- [ ] Generated brief includes: summary, key points, entities involved, decisions, timeline
- [ ] Each section includes provenance (which objects contributed, when, source)
- [ ] Brief is formatted as human-readable markdown by default
- [ ] User can export brief as PDF, HTML, or plain text
- [ ] Brief generation completes within 5 seconds
- [ ] Brief respects focus profile (role-appropriate emphasis)

---

## Implementation Notes

### CLI Interface

```bash
# Generate from specific objects
ctxt make brief --from o-abc123,o-def456,o-ghi789

# Generate from query results
ctxt make brief --tag urgent --days 7

# With custom template
ctxt make brief --from o-abc123 --template executive

# Export to PDF
ctxt make brief --from o-abc123 --export pdf > brief.pdf

# With custom profile
ctxt make brief --from o-abc123 --profile engineering

# Returns JSON with brief content
{
  "brief_id": "b-xyz789",
  "title": "Recent Urgent Decisions",
  "template": "executive",
  "sections": [...]
}
```

### Brief Template Structure

```yaml
# Brief templates define sections and generation logic

templates:
  default:
    name: "Standard Brief"
    sections:
      - id: context
        title: "Context"
        instructions: "Summarize who, what, when, where"
        max_sentences: 3
      - id: key_points
        title: "Key Points"
        instructions: "List 3-5 main insights"
        format: "bullet_list"
      - id: decisions
        title: "Decisions"
        instructions: "Extract and explain decisions"
        format: "structured"
      - id: next_steps
        title: "Next Steps"
        instructions: "Actions or follow-ups"
        format: "checklist"
      - id: participants
        title: "Participants"
        instructions: "List people involved"
        format: "list"

  executive:
    name: "Executive Summary"
    sections:
      - id: executive_summary
        title: "Executive Summary"
        instructions: "One paragraph, business impact focus"
        max_sentences: 1
      - id: key_metrics
        title: "Key Metrics"
        instructions: "Numbers, outcomes, impact"
        format: "structured"
      - id: recommendations
        title: "Recommendations"
        instructions: "Specific, actionable next steps"
        format: "numbered_list"

  technical:
    name: "Technical Brief"
    sections:
      - id: technical_summary
        title: "Technical Overview"
        instructions: "Architecture, systems, components"
        format: "structured"
      - id: design_decisions
        title: "Design Decisions"
        instructions: "Trade-offs, alternatives considered"
        format: "comparison"
      - id: implementation_notes
        title: "Implementation Notes"
        instructions: "Code examples, configurations"
        format: "code_blocks"
```

### Brief Generation Pipeline

```go
type BriefGenerator struct {
  objectStore ObjectStore
  graphStore  GraphStore
  aiProvider  AIProvider
}

func (g *BriefGenerator) GenerateBrief(ctx context.Context,
    objectIDs []string, template string, profile string) (*Brief, error) {

  // Step 1: Retrieve objects + graph neighbors
  objects := g.objectStore.GetByIDs(objectIDs)
  entities := g.extractEntitiesFromObjects(objects)
  relatedObjects := g.graphStore.GetNeighbors(objectIDs)

  // Step 2: Select template
  tmpl := g.loadTemplate(template)

  // Step 3: Generate brief sections
  brief := &Brief{
    ID:        generateID(),
    Title:     g.generateTitle(objects),
    Template:  template,
    Objects:   objectIDs,
    Sections:  []Section{},
  }

  for _, sectionDef := range tmpl.Sections {
    section := g.generateSection(ctx, objects, entities, sectionDef, profile)
    brief.Sections = append(brief.Sections, section)
  }

  // Step 4: Apply profile lens
  brief = g.applyProfile(brief, profile)

  // Step 5: Add provenance
  brief.Provenance = g.generateProvenance(objects, relatedObjects)

  return brief, nil
}

func (g *BriefGenerator) generateSection(ctx context.Context,
    objects []Object, entities []Entity, sectionDef SectionDef, profile string) Section {

  prompt := fmt.Sprintf(`
    Context: %v
    Entities: %v
    Task: %s
    Profile: %s

    Generate the brief section:
    %s
  `, objects, entities, sectionDef.Title, profile, sectionDef.Instructions)

  content := g.aiProvider.Call(ctx, prompt)
  return Section{
    ID:      sectionDef.ID,
    Title:   sectionDef.Title,
    Content: content,
    Format:  sectionDef.Format,
  }
}

func (g *BriefGenerator) generateProvenance(objects []Object,
    relatedObjects []Object) Provenance {

  provenance := Provenance{
    PrimaryObjects: []ObjectRef{},
    RelatedObjects: []ObjectRef{},
    GeneratedAt:    time.Now(),
  }

  for _, obj := range objects {
    provenance.PrimaryObjects = append(provenance.PrimaryObjects, ObjectRef{
      ID:        obj.ID,
      Type:      obj.Type,
      Summary:   obj.Summary,
      CreatedAt: obj.CreatedAt,
      Source:    obj.Source,
    })
  }

  for _, obj := range relatedObjects {
    provenance.RelatedObjects = append(provenance.RelatedObjects, ObjectRef{
      ID:        obj.ID,
      Type:      obj.Type,
      Relevance: g.calculateRelevance(obj, objects),
    })
  }

  return provenance
}
```

### REST API

```
POST /compositions/brief
Content-Type: application/json

{
  "object_ids": ["o-abc123", "o-def456"],
  "template": "executive",
  "profile": "engineering"
}

→ 200 OK
{
  "brief_id": "b-xyz789",
  "title": "Recent Architecture Decisions",
  "template": "executive",
  "sections": [
    {
      "id": "executive_summary",
      "title": "Executive Summary",
      "content": "The team decided to migrate from monolith...",
      "format": "markdown"
    },
    {
      "id": "recommendations",
      "title": "Recommendations",
      "content": "1. Begin phase 1 migration in Q2\n2. Allocate 3 engineers for 4 weeks\n3. Risk: timing with other projects",
      "format": "markdown"
    }
  ],
  "provenance": {
    "primary_objects": [...],
    "related_objects": [...],
    "generated_at": "2025-01-18T10:30:45Z"
  }
}
```

### Markdown Output Example

```markdown
# Recent Architecture Decisions

**Generated:** 2025-01-18 10:30 AM
**Template:** Executive
**Objects:** 3

## Executive Summary

The engineering team made critical decisions regarding the migration from a monolithic architecture to microservices. The decision prioritizes customer-facing features over infrastructure optimization in Q1, with infrastructure work deferred to Q2.

## Key Decisions

1. **Defer Infrastructure Refactor** (Decision D-5)
   - Impact: HIGH
   - Status: OPEN
   - Date: 2025-01-15
   - Stakeholders: Alice (Tech Lead), Bob (PM)

2. **Prioritize Feature Velocity** (Decision D-6)
   - Impact: HIGH
   - Status: OPEN
   - Date: 2025-01-16
   - Rationale: Market deadline for new features

## Next Steps

- [ ] Brief product team on timeline impact
- [ ] Schedule infrastructure planning session for Q2
- [ ] Update roadmap with new priorities
- [ ] Review resource allocation with finance

## Participants

- **Alice Smith** (@person.alice-smith) — Tech Lead
- **Bob Johnson** (@person.bob-johnson) — Product Manager
- **Carol Davis** (@person.carol-davis) — Engineering Manager

## See Also

- Related Decision: Performance Optimization Strategy (D-3)
- Mentioned System: Backend API (D-5)
- Mentioned Concept: Microservices Architecture

---

**Sources:**

| Object | Type | Created | Source |
|--------|------|---------|--------|
| D-5 | Decision | 2025-01-15 | engineering-meeting-notes.pdf |
| D-6 | Decision | 2025-01-16 | slack-#engineering |
| T-12 | Task | 2025-01-16 | email-from-alice |
```

---

## E2E Test Checklist

- [ ] CLI: `ctxt make brief --from o-abc123` returns valid brief JSON
- [ ] CLI: Brief includes all template sections
- [ ] CLI: Brief includes provenance (source objects, timestamps)
- [ ] CLI: Brief generation completes within 5 seconds
- [ ] Template: Multiple templates produce correctly formatted output
- [ ] Template: Custom template overrides default
- [ ] Profile: Brief with `--profile engineering` emphasizes technical content
- [ ] Export: Markdown export produces valid markdown
- [ ] Export: PDF export creates readable PDF file
- [ ] Entities: Brief includes entities/mentions from source objects
- [ ] Graph: Brief includes related objects discovered via graph traversal
- [ ] Query: `--tag urgent --days 7` generates brief from query results
- [ ] REST API: POST /compositions/brief returns 200 + brief JSON
- [ ] REST API: Brief JSON includes all required fields (sections, provenance)

---

## Related Stories

- [text-capture-minimal-friction](../ingestion/text-capture-minimal-friction.md) — Source content for brief
- [extract-entities-and-mentions](../enrichment/extract-entities-and-mentions.md) — Extracted entities in brief
- [extract-decisions-and-tasks](../enrichment/extract-decisions-and-tasks.md) — Decisions in brief
- [natural-language-search](../search/natural-language-search.md) — Query source objects for brief
- [compose-with-graph-traversal](./compose-with-graph-traversal.md) — Use graph neighbors
