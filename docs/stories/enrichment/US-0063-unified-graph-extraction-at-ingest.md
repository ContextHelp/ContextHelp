---
status: paper
---

# US-0063: Unified Graph Extraction at Ingest

**System Types:** dpkms (self-hosted), dpkms cloud
**Personas:** [Agents/LLMs](../../personas/agents-llms-tools.md),
[Knowledge Workers](../../personas/knowledge-workers.md)

---

## User Goal

As an AI agent or knowledge worker, I want every ingested object to carry a rich, typed graph
of its internal structure — topics, decisions, open questions, artifacts, and their relations —
so I can query across objects with questions like "what decisions were made in the last week?"
without post-hoc enrichment passes.

---

## Context

Under the graph-canonical KO model (ADR-063), a `KnowledgeObject` stores an `ObjectGraph`
(typed nodes + edges) in `graph_json`. Previous enrichment steps wrote to separate flat fields
(decisions array, tasks array, tags, sections). With a unified extractor running at ingest time,
all node types are emitted in a single pass and stored canonically. Projection helpers then
derive `DocumentProjection` (display) and `IndexProjection` (FTS/vector/filter) on the way out,
eliminating the need for per-step projection logic downstream.

This story covers the `graph_extractor` ingest step (T-0176) that supersedes the piecemeal
extraction steps for search and agent workflows.

---

## Acceptance Criteria

- [ ] A `graph_extractor` pipeline step runs on raw content at ingest time
- [ ] Step emits the following typed nodes into `ObjectGraph`:
  - `section` — structural sections detected in the content
  - `tag` — topic/domain tags assigned from vocabulary
  - `entity_mention` — `@namespace.slug` mentions (people, projects, systems, concepts)
  - `decision` — decisions with `impact` (LOW/MEDIUM/HIGH) and `status` (OPEN/RESOLVED/SUPERSEDED)
  - `task` — open questions / action items with optional `assignee`
  - `summary` — generated one-paragraph summary node
  - `code_block` — detected code blocks with language annotation
- [ ] Step emits typed edges between nodes:
  - `contains` — section contains sub-nodes (tags, decisions, tasks)
  - `references` — entity_mention references a canonical entity node
  - `resolves_to` — task/question resolves_to a decision
  - `derives_from` — summary derives_from section nodes
- [ ] Resulting `ObjectGraph` is stored as `graph_json` on the object row (migration 022+)
- [ ] `DocumentProjection` derived from graph: title, body, sections list, structured metadata
- [ ] `IndexProjection` derived from graph: `fts_body`, `tags` array, `mentions` array,
  `embedding_text`
- [ ] `GET /objects/{id}` returns `graph` field populated from `graph_json`
- [ ] Agent query `GET /search?q=decisions+last+week&node_type=decision` returns decision nodes
  across objects, sorted by `created_at` descending
- [ ] Step supports both LMQL (local, hard-constrained) and instructor (API-hosted, retry) modes
- [ ] Step is idempotent: re-running on same content produces identical `ObjectGraph`
- [ ] Step integrates into the standard ingest pipeline and does not break existing FTS/vector
  indexes (projections keep index fields populated)

---

## Implementation Notes

### Pipeline Step Registration

```go
// internal/pipeline/steps/graph_extractor.go
type GraphExtractorStep struct {
    llm        LLMProvider
    edgeStore  EdgeStore
    objectStore ObjectStore
}

func (s *GraphExtractorStep) Name() string { return "graph_extractor" }

func (s *GraphExtractorStep) Execute(ctx ExecutionContext,
    draft *storage.KnowledgeObjectDraft) error {

    graph, err := s.extractGraph(ctx, draft.RawContent)
    if err != nil { return err }

    draft.Graph = graph
    // projections invoked by storage layer on write
    return nil
}
```

### Node/Edge Types (from pluginapi constants)

```go
// Node types
pluginapi.NodeTypeSection       = "section"
pluginapi.NodeTypeTag           = "tag"
pluginapi.NodeTypeEntityMention = "entity_mention"
pluginapi.NodeTypeDecision      = "decision"
pluginapi.NodeTypeTask          = "task"
pluginapi.NodeTypeSummary       = "summary"
pluginapi.NodeTypeCodeBlock     = "code_block"

// Edge types
pluginapi.EdgeTypeContains    = "contains"
pluginapi.EdgeTypeReferences  = "references"
pluginapi.EdgeTypeResolvesTo  = "resolves_to"
pluginapi.EdgeTypeDerivedFrom = "derives_from"
```

### Extraction Prompt (pseudocode)

```
system: "Extract structured knowledge graph from content."
user: content

return ObjectGraph{
  nodes: [
    { id: nodeURI(obj, "section", 0), type: "section", text: "..." },
    { id: nodeURI(obj, "tag", 0),     type: "tag",     label: "go" },
    { id: nodeURI(obj, "decision", 0),type: "decision",
      text: "...", impact: in(["LOW","MEDIUM","HIGH"]),
      status: in(["OPEN","RESOLVED","SUPERSEDED"]) },
    { id: nodeURI(obj, "task", 0),    type: "task",    text: "...", assignee: "@person.x" },
    { id: nodeURI(obj, "summary", 0), type: "summary", text: "..." },
    ...
  ],
  edges: [
    { from: sectionNodeID, to: decisionNodeID, type: "contains" },
    { from: summaryNodeID, to: sectionNodeID,  type: "derives_from" },
    ...
  ],
}
```

### LMQL Constrained Extraction (local models)

```lmql
argmax
    "Classify node type: "
    node_type in [
      "section","tag","entity_mention","decision","task","summary","code_block"
    ]
    "\nDecision impact (if decision): "
    impact in ["LOW","MEDIUM","HIGH","N/A"]
    "\nDecision status (if decision): "
    status in ["OPEN","RESOLVED","SUPERSEDED","N/A"]
from openai("gpt-4")
```

### Projection Derivation

```go
// On storage Get/List — invoked after reading graph_json
func DeriveDocumentProjection(g ObjectGraph) DocumentProjection {
    return DocumentProjection{
        Title:    firstSectionText(g),
        Body:     joinSectionTexts(g),
        Sections: collectNodes(g, NodeTypeSection),
        Metadata: buildMetadataMap(g),
    }
}

func DeriveIndexProjection(g ObjectGraph) IndexProjection {
    return IndexProjection{
        FTSBody:       joinTexts(g, NodeTypeSection, NodeTypeSummary, NodeTypeDecision),
        Tags:          collectLabels(g, NodeTypeTag),
        Mentions:      collectLabels(g, NodeTypeEntityMention),
        EmbeddingText: joinTexts(g, NodeTypeSummary, NodeTypeEntityMention),
    }
}
```

### REST API — Object with Graph

```
GET /objects/{object_id}

→ 200 OK
{
  "id": "o-abc123",
  "type": "text",
  "graph": {
    "nodes": [
      { "id": "ctxt:node/o-abc123/decision/0", "type": "decision",
        "text": "Use SQLite as default storage backend",
        "impact": "HIGH", "status": "OPEN" },
      { "id": "ctxt:node/o-abc123/tag/0",      "type": "tag", "label": "architecture" },
      { "id": "ctxt:node/o-abc123/summary/0",  "type": "summary",
        "text": "Discussion about storage backend selection..." }
    ],
    "edges": [
      { "from": "ctxt:node/o-abc123/section/0",
        "to":   "ctxt:node/o-abc123/decision/0", "type": "contains" }
    ]
  },
  "document": {
    "title": "Storage Backend Decision",
    "body": "..."
  },
  "index": {
    "fts_body": "...",
    "tags": ["architecture"],
    "mentions": ["@system.sqlite"],
    "embedding_text": "..."
  }
}
```

### REST API — Cross-Object Graph Node Query

```
GET /search?node_type=decision&since=2026-03-23T00:00:00Z

→ 200 OK
{
  "node_type": "decision",
  "since": "2026-03-23T00:00:00Z",
  "results": [
    {
      "object_id": "o-abc123",
      "node_id": "ctxt:node/o-abc123/decision/0",
      "text": "Use SQLite as default storage backend",
      "impact": "HIGH",
      "status": "OPEN",
      "created_at": "2026-03-25T14:00:00Z"
    }
  ],
  "total": 7,
  "took_ms": 34
}
```

### Pipeline Integration

```yaml
# In pipeline definition (text.long, text.short, url, document)
steps:
  - name: graph_extractor
    config:
      ai_provider: lmql        # or "instructor"
      max_nodes: 50
      node_types:              # subset if needed; default: all
        - section
        - tag
        - entity_mention
        - decision
        - task
        - summary
        - code_block
```

---

## E2E Test Checklist

### Ingest + Graph Storage

- [ ] `ctxt add "content with a decision..."` completes; `GET /objects/{id}` returns `graph` field
  with at least one `decision` node
- [ ] `graph.nodes` contains typed nodes for all detected types present in content
- [ ] `graph.edges` contains at least one `contains` edge linking a section to a child node
- [ ] `graph_json` column in SQLite row is non-null after ingest
- [ ] Re-ingesting identical content produces identical `graph_json` (idempotency)

### Projection Correctness

- [ ] `document.title` matches first section node text
- [ ] `index.tags` array equals labels from all `tag` nodes
- [ ] `index.mentions` array equals labels from all `entity_mention` nodes
- [ ] `index.fts_body` contains text from section, summary, and decision nodes
- [ ] `index.embedding_text` is non-empty and contains summary text
- [ ] FTS search on `index.fts_body` content returns the ingested object

### Decision/Task Nodes

- [ ] Decision node: `impact` is one of `LOW`, `MEDIUM`, `HIGH`
- [ ] Decision node: `status` is one of `OPEN`, `RESOLVED`, `SUPERSEDED`
- [ ] Task node: `text` field is non-empty; `assignee` is optional `@type.slug` or absent
- [ ] Content with no decisions returns `graph.nodes` without any `decision` type (not an error)

### Cross-Object Node Query

- [ ] `GET /search?node_type=decision` returns decision nodes from multiple objects
- [ ] `GET /search?node_type=decision&since=<iso8601>` returns only decisions from objects
  created after the given timestamp
- [ ] Response includes `object_id`, `node_id`, `text`, `impact`, `status`, `created_at`
- [ ] Results sorted by `created_at` descending

### Extraction Modes

- [ ] `ai_provider=lmql`: decision impact/status constrained to enum values (no invalid output)
- [ ] `ai_provider=instructor`: extraction succeeds with retry on schema validation failure
- [ ] Step registered in all standard pipeline definitions (`text.short`, `text.long`,
  `url`, `document`)

### Compatibility

- [ ] Objects without `graph_json` (pre-migration rows) return empty `graph` field; FTS/vector
  indexes still populated via legacy flat columns
- [ ] Existing search queries (`ctxt find "..."`) return correct results after step is enabled
- [ ] Enrichment step is retryable on transient LLM failures

---

## Related Stories

- [US-0001](../ingestion/US-0001-text-capture-minimal-friction.md) — ingest trigger
- [US-0009](./US-0009-extract-entities-and-mentions.md) — entity extraction (unified into this step)
- [US-0010](./US-0010-extract-decisions-and-tasks.md) — decision/task extraction (unified)
- [US-0011](./US-0011-assign-tags-from-vocabulary.md) — tag assignment (unified)
- [US-0012](./US-0012-generate-summaries-and-sections.md) — summary/section generation (unified)
- [US-0013](./US-0013-detect-and-extract-code-snippets.md) — code block extraction (unified)
- [US-0014](./US-0014-constrain-extraction-with-lmql.md) — LMQL constraint enforcement
- [US-0017](../search/US-0017-structured-rsql-query.md) — graph nodes queryable via RSQL
- [US-0024](../composition/US-0024-compose-with-graph-traversal.md) — graph traversal in
  composition
- [US-0052](../search/US-0052-graph-based-entity-search.md) — graph-based entity search

---

## Personas

- [Agents / LLMs / Tools](../../personas/agents-llms-tools.md)
- [Knowledge Workers](../../personas/knowledge-workers.md)

---

## E2E Tests

- planned: `test/integration/us0063_graph_at_ingest_test.go::TestGraphAtIngest_NodesEmitted`
- planned: `test/integration/us0063_graph_at_ingest_test.go::TestGraphAtIngest_EdgesEmitted`
- planned: `test/integration/us0063_graph_at_ingest_test.go::TestGraphAtIngest_DedupeAcrossPipelines`
- planned: `test/integration/us0063_graph_at_ingest_test.go::TestGraphAtIngest_PartialFailureRetains`
