# US-0062: Graph-Canonical KO with Projected Document Views

**System Types:** ctxt, dpkms
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md),
[Agents/LLMs](../../personas/agents-llms-tools.md)

---

## User Goal

As a knowledge worker or agent, I want notes stored as graph-canonical knowledge objects with
stable sub-object identities, so I can retrieve rendered document views, query specific node
types (decisions, tasks, sections), and reference individual nodes by URI across the corpus.

---

## Context

Under ADR-063, `KnowledgeObject` canonical form shifts from a flat struct to an `ObjectGraph`
of typed nodes and edges. Every section, tag, decision, task, summary, and code block becomes
a `GraphNode` with a stable URI (`ctxt:node/<objectID>/<nodeType>/<ordinal>`). Storage derives
two projections on read:

- **DocumentProjection** — human-readable title/body/sections for display.
- **IndexProjection** — FTS body, tags, mentions, embedding text for search infra.

This enables:
- Stable cross-object node references (agent can cite a specific decision node by URI).
- Node-type-scoped corpus queries ("open questions about authentication").
- Reproducible search indexes: IndexProjection defines exactly what text is indexed.

---

## Acceptance Criteria

### Ingest

- [ ] Note ingested via `ctxt add` is stored with a populated `graph_json` blob in SQLite.
- [ ] Each extracted section, tag, decision, task, summary, and code block becomes a `GraphNode`
      with a unique ID within the object scope.
- [ ] Every `GraphNode` has a stable URI of the form `ctxt:node/<objectID>/<nodeType>/<ordinal>`.
- [ ] Intra-object edges (contains, references, resolves_to, derives_from) are stored inside
      `graph_json` only; not duplicated to the inter-object `edges` table.

### DocumentProjection

- [ ] `GET /objects/{id}` response includes a `document` projection with `title`, `body`,
      `sections[]`, and `metadata`.
- [ ] `sections[]` items are ordered by `GraphNode.Order` ascending.
- [ ] `document` projection is derived on read from `ObjectGraph`; not persisted separately.
- [ ] CLI `ctxt open <id>` renders the DocumentProjection (title + sections).

### Node-Type Query

- [ ] User can filter results to a specific node type:
      `ctxt find --node-type decision "this object"` returns only decision nodes.
- [ ] REST: `GET /objects/{id}/nodes?type=decision` returns all `GraphNode`s of that type.
- [ ] REST: `GET /objects/{id}/nodes?type=task&status=open` returns open task nodes.
- [ ] Each node in the response includes: `id`, `uri` (`ctxt:node/…`), `type`, `label`,
      `content`, `order`.

### Stable Sub-Object References

- [ ] Agent can dereference a node URI: `GET /nodes/ctxt:node/<objectID>/<nodeType>/<ordinal>`
      returns the matching `GraphNode` and its parent object ID.
- [ ] Node URIs survive object `Update` (same objectID + type + ordinal = same URI).
- [ ] Parsing an invalid node URI returns a structured error (not a 500).

### Corpus Node Queries

- [ ] `ctxt find --node-type decision "open questions about authentication"` returns
      `NodeHit` results pointing to decision/task nodes across multiple objects.
- [ ] Each `NodeHit` includes: `object_id`, `node_ref` (full `ctxt:node/…` URI),
      `node_type`, `label`, `snippet`, `rank.score`.
- [ ] REST: `GET /search?q=open+questions+about+authentication&node_type=decision`
      returns a `NodeAwareResult` with `node_hits[]`.
- [ ] Results are ranked; `rank.explain` shows per-strategy scores.
- [ ] Query works across all objects in the corpus (not scoped to a single object).

### IndexProjection

- [ ] `IndexProjection.FTSBody` = concatenated text from `NodeSection` + `NodeSummary` nodes.
- [ ] `IndexProjection.Tags` = labels from `NodeTag` nodes.
- [ ] `IndexProjection.Mentions` = labels from `NodeEntityMention` nodes.
- [ ] FTS index is built from `IndexProjection.FTSBody`; vector embedding from
      `IndexProjection.EmbeddingText`.
- [ ] Re-running indexing from the same `graph_json` produces identical FTS/vector content.

---

## Implementation Notes

### Ingest Flow

```
ctxt add "Auth service uses JWT; open question: should we rotate keys weekly?"

1. Pipeline extracts sections, decisions, tasks via enrichment steps
2. Each sub-object written to Graph.Nodes (not flat fields)
3. Storage.Create serialises ObjectGraph → graph_json
4. object_nodes denorm table upserted for fast node-type queries
5. IndexProjection derived → FTS + vector index updated
6. DocumentProjection available immediately on GET
```

### Node URI Format

```
ctxt:node/<objectID>/<nodeType>/<ordinal>

Examples:
  ctxt:node/obj-abc123/decision/0    ← first decision node in obj-abc123
  ctxt:node/obj-abc123/section/2     ← third section
  ctxt:node/obj-abc123/task/0        ← first task
```

Ordinal = 0-indexed count within that `nodeType` for this object. Stable across updates
as long as object ID, type, and relative order within type are preserved.

### REST API

```
# Retrieve document projection
GET /objects/obj-abc123
→ {
    "id": "obj-abc123",
    "document": {
      "title": "Auth service JWT notes",
      "body": "...",
      "sections": [
        {"order": 0, "label": "Overview", "content": "Auth service uses JWT…"},
        {"order": 1, "label": "Open question", "content": "Should we rotate keys weekly?"}
      ]
    },
    "graph": { "node_count": 5, "edge_count": 3 }
  }

# Query nodes by type within one object
GET /objects/obj-abc123/nodes?type=decision
→ {
    "nodes": [
      {
        "id": "node-uuid-1",
        "uri": "ctxt:node/obj-abc123/decision/0",
        "type": "decision",
        "label": "JWT key rotation policy",
        "content": "open question: should we rotate keys weekly?",
        "order": 0
      }
    ]
  }

# Dereference a node URI
GET /nodes/ctxt:node/obj-abc123/decision/0
→ { "object_id": "obj-abc123", "node": { ... } }

# Corpus node search
GET /search?q=open+questions+about+authentication&node_type=decision
→ {
    "query": "open questions about authentication",
    "node_hits": [
      {
        "object_id": "obj-abc123",
        "node_ref": "ctxt:node/obj-abc123/decision/0",
        "node_type": "decision",
        "label": "JWT key rotation policy",
        "snippet": "open question: should we rotate keys weekly?",
        "rank": { "score": 0.91, "explain": { "vector": 0.88, "fts": 0.72 } }
      }
    ],
    "total": 3,
    "took_ms": 210
  }
```

### CLI Interface

```
# Ingest a note (graph stored automatically)
ctxt add "Auth uses JWT; open Q: rotate keys weekly?"

# View rendered document
ctxt open obj-abc123

# Query decision nodes in one object
ctxt open obj-abc123 --node-type decision

# Corpus query returning node hits
ctxt find --node-type decision "open questions about authentication"
ctxt find --node-type task "authentication tasks"

# Dereference a specific node
ctxt node get "ctxt:node/obj-abc123/decision/0"
```

### Go Types (pluginapi, pseudocode)

```go
// NodeHit — a search result pointing to a specific node within an object.
type NodeHit struct {
    ObjectID  string      `json:"object_id"`
    NodeRef   string      `json:"node_ref"`   // ctxt:node/… URI
    NodeType  GraphNodeType `json:"node_type"`
    Label     string      `json:"label"`
    Snippet   string      `json:"snippet"`
    RankScore float64     `json:"rank_score"`
}

// NodeAwareFilter — extends object-level filter with node-type constraints.
type NodeAwareFilter struct {
    NodeTypes      []GraphNodeType `json:"node_types,omitempty"`
    ReturnNodeHits bool            `json:"return_node_hits,omitempty"`
}

// NodeAwareResult — search result carrying object result + per-node hits.
type NodeAwareResult struct {
    Objects  []KnowledgeObject `json:"objects"`
    NodeHits []NodeHit         `json:"node_hits,omitempty"`
}
```

### Storage Layer

```
SQLite schema (migration 022):
  objects.graph_json  TEXT   -- ObjectGraph JSON blob (write source of truth)
  object_nodes table         -- denorm: object_id, node_id, node_type, label, content, ordinal

Projections invoked by storage Get/List (never persisted in graph_json):
  DocumentProjection  ← derived from NodeSection nodes ordered by Order
  IndexProjection     ← FTSBody from NodeSection+NodeSummary; Tags from NodeTag; etc.
```

---

## E2E Test Checklist

### Ingest

- [ ] `ctxt add "note with decisions and tasks"` exits 0; server persists `graph_json` (non-null)
      for the new object (verified via `GET /objects/{id}` showing `graph.node_count > 0`)
- [ ] `object_nodes` table has rows for the new object after ingest
- [ ] Each row in `object_nodes` has a valid `node_uri` in `ctxt:node/…` format

### DocumentProjection

- [ ] `GET /objects/{id}` includes `document.sections[]` ordered by `order` ascending
- [ ] `document.body` is non-empty for a text object
- [ ] `ctxt open <id>` outputs title + sections without error

### Node-Type Query (single object)

- [ ] `GET /objects/{id}/nodes?type=decision` returns only `decision` nodes
- [ ] `GET /objects/{id}/nodes?type=task` returns only `task` nodes
- [ ] Object with no task nodes returns empty `nodes[]`, not 404
- [ ] Each node response includes `uri` matching `ctxt:node/<id>/decision/<n>` format

### Stable Node URIs

- [ ] Node URI parsed by `ParseNodeURI` returns correct objectID, nodeType, ordinal
- [ ] `GET /nodes/ctxt:node/<id>/decision/0` returns the matching node + its parent object ID
- [ ] `GET /nodes/ctxt:node/<id>/decision/999` (non-existent ordinal) returns 404, not 500
- [ ] Malformed URI `GET /nodes/bad-uri` returns structured error (not panic/500)
- [ ] Update object content → same `ctxt:node/<id>/section/0` URI still resolves to section 0

### Corpus Node Query

- [ ] `ctxt find --node-type decision "authentication"` returns `NodeHit` results
- [ ] Each `NodeHit` has `object_id`, `node_ref`, `node_type`, `label`, `snippet`, `rank.score`
- [ ] `GET /search?q=open+questions+about+auth&node_type=decision` returns `node_hits[]`
- [ ] Results span multiple objects when >1 object matches
- [ ] `rank.explain` is populated for each hit
- [ ] `--node-type task "authentication"` returns only task nodes (no decision nodes mixed in)

### IndexProjection

- [ ] FTS search on text from a `section` node finds the object
- [ ] Re-index run from same `graph_json` produces same FTS/vector content (deterministic)
- [ ] `tags` in search index match `NodeTag` labels in `graph_json`

### Interface Parity

- [ ] Same corpus query via CLI, `GET /search?…`, gRPC → identical `node_hits` sets

---

## Related Stories

- [US-0001](../ingestion/US-0001-text-capture-minimal-friction.md) — text capture triggers graph
  construction
- [US-0009](../enrichment/US-0009-extract-entities-and-mentions.md) — entity extraction writes
  entity_mention nodes to Graph
- [US-0016](./US-0016-natural-language-search.md) — NLQ search extended with node-type filters
- [US-0017](./US-0017-structured-rsql-query.md) — RSQL structured query can filter by node type
- [US-0052](./US-0052-graph-based-entity-search.md) — graph entity search complemented by
  intra-object node traversal
- [US-0037](../agents/US-0037-agent-ingests-structured-content.md) — agent ingest path that
  writes graph-canonical nodes
- [US-0038](../agents/US-0038-agent-queries-knowledge-base.md) — agent queries using stable
  node URIs as citations

---

## Personas

- [Knowledge Workers](../../personas/knowledge-workers.md)
- [Agents / LLMs / Tools](../../personas/agents-llms-tools.md)
- [Platform Integrators](../../personas/platform-integrators.md)

---

## E2E Tests

> Not yet implemented.
