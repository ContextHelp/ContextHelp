---
status: shipped
---

# US-0009: Extract Entities and Mentions

**System Types:** dpkms (self-hosted), dpkms cloud
**Personas:** [Agents/LLMs](../../personas/agents-llms-tools.md), [Knowledge Workers](../../personas/knowledge-workers.md)

---

## User Goal

As an AI system or enrichment pipeline, I want to automatically extract named entities and mentions from captured content so they become queryable and relatable.

---

## Context

Entities (people, projects, systems, concepts) are the semantic glue that connects knowledge across documents. Mentions (cross-references using `@namespace.slug` format) enable graph traversal and discovery. Automatic extraction eliminates manual tagging while ensuring consistent format.

---

## Acceptance Criteria

- [ ] Entity extraction identifies all canonical types: person, organization, concept, location, product, event, project, system
- [ ] Mentions are formatted as `@namespace.slug` (e.g., `@person.alice-smith`, `@project.mobile-redesign`)
- [ ] Extraction is deterministic (same input always produces same entities)
- [ ] Mentions are automatically linked to canonical entities in knowledge base
- [ ] Entities unknown to system are created as new canonical entities
- [ ] Extracted mentions stored as `entity_mention` nodes in the object's `ObjectGraph`
  (graph-canonical write); `IndexProjection.Mentions` derived on read exposes them as an array
- [ ] Intra-object edges (object → references → entity) stored in `graph_json`; inter-object
  edges written to the `edges` table for cross-object traversal
- [ ] Pipeline supports both local (LMQL) and API-hosted (instructor) extraction

---

## Implementation Notes

### Extraction Types

**Named Entities (canonical `MentionType` set):**
- `@person.[slug]` — individuals mentioned
- `@organization.[slug]` — companies, teams, departments
- `@concept.[slug]` — abstract ideas, patterns, principles
- `@location.[slug]` — places or locations
- `@product.[slug]` — products or services
- `@event.[slug]` — events or occurrences
- `@project.[slug]` — initiatives, products, projects
- `@system.[slug]` — software systems, platforms, services
- `@entity.[slug]` — generic entities not fitting other types

**Format Rules:**
- Slug format: lowercase, hyphens, alphanumeric: `[a-z0-9-]+`
- Example inputs: `"Alice Smith"` → `@person.alice-smith`
- Example inputs: `"mobile app redesign"` → `@project.mobile-app-redesign`

### Pipeline Step Implementation

```go
type EntityExtractionStep struct {
  extractionProvider AIProvider  // LMQL or instructor
  entityRegistry     EntityStore
  graphStore         GraphStore
}

func (s *EntityExtractionStep) Execute(ctx ExecutionContext,
    input interface{}) (interface{}, error) {

  // Extract entities using constrained AI
  prompt := fmt.Sprintf(`
    Extract named entities from this text.
    Return JSON array: [{"type":"person","name":"...","slug":"..."}]

    Text: %s
  `, input.(string))

  entities := s.extractionProvider.Call(ctx, prompt, Constraints{
    OutputFormat: "json",
    Schema: EntityListSchema,
  })

  // Normalize and canonicalize
  mentions := []string{}
  for _, entity := range entities {
    slug := normalizeSlug(entity.Name)

    // Lookup or create canonical entity
    canonical := s.entityRegistry.GetOrCreate(entity.Type, slug)

    mention := fmt.Sprintf("@%s.%s", entity.Type, canonical.Slug)
    mentions = append(mentions, mention)

    // Create graph edge: source object → mention → entity
    s.graphStore.CreateEdge(input.ID, mention, canonical.ID)
  }

  return map[string]interface{}{
    "mentions": mentions,
    "count": len(mentions),
  }, nil
}
```

### Constrained Extraction (LMQL)

```lmql
argmax
    "Extract entities from: " + text + "\n"
    "Output JSON array:\n"
    "[\n"
    for i in range(max_entities):
        '  {"type":"' + in(["person", "organization", "concept", "location", "product", "event", "project", "system", "entity"]) + '", '
        '"name":"' + gen(name_pattern) + '", '
        '"slug":"' + gen(slug_pattern) + '"},\n'
    "]\n"
from
    openai("gpt-4")
where
  len(STOP) < 200  // prevent too-long output
```

### Fallback for API Models (instructor)

```python
from pydantic import BaseModel, Field
from typing import List

class Entity(BaseModel):
    type: str = Field(..., description="person, organization, concept, location, product, event, project, system, or entity")
    name: str = Field(..., description="full name as written in text")
    slug: str = Field(..., description="lowercase, hyphenated, alphanumeric slug")

class EntityList(BaseModel):
    entities: List[Entity]

# Use instructor to validate output
from instructor import Instructor
client = Instructor(openai.Client())

response = client.messages.create(
    model="gpt-4",
    messages=[{"role": "user", "content": prompt}],
    response_model=EntityList,
)

# instructor automatically retries on schema failure
```

### REST API Endpoint

```
POST /enrich/{object_id}/extract-mentions
Content-Type: application/json

{
  "step": "extract-entities-and-mentions",
  "ai_provider": "lmql",  // or "instructor" for API
  "max_entities": 10
}

→ 200 OK
{
  "object_id": "o-abc123",
  "mentions": [
    "@person.alice-smith",
    "@project.mobile-redesign",
    "@system.backend-api"
  ],
  "entities_created": 2,
  "entities_linked": 1,
  "graph_edges_added": 3
}
```

### Knowledge Object Update

```json
{
  "id": "o-abc123",
  "type": "text",
  "raw_content": "Alice Smith discussed the mobile redesign project with the backend team...",
  "mentions": [
    "@person.alice-smith",
    "@project.mobile-redesign",
    "@system.backend-api"
  ],
  "enrichment": {
    "entities_extracted_at": "2025-01-18T10:30:45Z",
    "extraction_method": "lmql",
    "confidence": 0.95
  }
}
```

### Graph Representation

```
object(o-abc123)
  ├── mentions(@person.alice-smith)
  │   └── entity(person:alice-smith)
  ├── mentions(@project.mobile-redesign)
  │   └── entity(project:mobile-redesign)
  └── mentions(@system.backend-api)
      └── entity(system:backend-api)
```

---

## E2E Test Checklist

- [ ] CLI: `ctxt enrich <object_id> --step extract-entities-and-mentions` exits 0
- [ ] CLI: `--ai-provider lmql` flag is present in the request payload sent to server (verified via request capture or server log)
- [ ] CLI: `--max-entities <n>` flag is present in the request payload as `max_entities` field
- [ ] CLI: `--step extract-entities-and-mentions` flag is present in the request payload as `step` field
- [ ] Server: POST `/enrich/{object_id}/extract-mentions` receives `step`, `ai_provider`, and `max_entities` in request body
- [ ] Server: Response contains `object_id`, `mentions` array, `entities_created`, `entities_linked`, and `graph_edges_added`
- [ ] Storage: GET `/objects/{object_id}` returns `index.mentions` array populated with
  `@type.slug` values (derived from `entity_mention` nodes via IndexProjection)
- [ ] Storage: `enrichment.extraction_method` field on stored object matches the `ai_provider`
  value sent in request
- [ ] Storage: `enrichment.entities_extracted_at` timestamp is set on the stored object after enrichment completes
- [ ] Extract: Input with clear entity names → all entities identified
- [ ] Extract: Unknown entities → created as new canonical entities
- [ ] Extract: Duplicate mentions → linked to same canonical entity
- [ ] Format: All mentions follow `@type.slug` format
- [ ] Format: Slugs are lowercase, hyphenated
- [ ] Graph: Edges created between object and mentioned entities
- [ ] Graph: Entity lookup by mention works (traversal test)
- [ ] Determinism: Same input → same entities across runs
- [ ] LMQL: Local model extraction succeeds without post-processing
- [ ] Instructor: API model extraction succeeds with retry on schema failure
- [ ] Async: Extraction works as pipeline step in job queue
- [ ] Resilience: Step is retryable on transient failures

---

## Related Stories

- [US-0001](../ingestion/US-0001-text-capture-minimal-friction.md) — Trigger for extraction
- [US-0011](./US-0011-assign-tags-from-vocabulary.md) — Tag assignment step
- [US-0022](../composition/US-0022-generate-brief-from-objects.md) — Use entities in composition
- [US-0024](../composition/US-0024-compose-with-graph-traversal.md) — Traverse entity relationships

---

## Personas

- [Agents / LLMs / Tools](../../personas/agents-llms-tools.md)
- Automation Builder
- Platform Engineer

---

## E2E Tests

- `test/integration/us0009_entity_extraction_test.go::TestUS0009_EntityMentionsExtracted`
- `test/integration/us0009_entity_extraction_test.go::TestUS0009_MentionEdgesCreated`
- `test/integration/us0009_entity_extraction_test.go::TestUS0009_MentionFormatIsNamespaceSlug`
- `test/integration/us0009_entity_extraction_test.go::TestUS0009_EntityObjectsCreatedForMentions`
