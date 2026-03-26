# US-0024: Compose With Graph Traversal

**System Types:** dpkms (self-hosted), dpkms cloud
**Personas:** [Agents/LLMs](../../personas/agents-llms-tools.md)

---

## User Goal

As an agent or system integrator, I want to compose a view of knowledge by traversing the entity graph from a root object so I can automatically surface related objects, context, and connections without knowing the full object set in advance.

---

## Context

Knowledge objects in dPKMS are connected via typed edges in a graph (mentions, references, related-to, etc.). Agents building context windows or composing briefs need to explore the neighborhood of a root object to gather relevant related content. Parameterised graph traversal with depth and direction controls lets agents pull exactly the context they need without over-fetching.

---

## Acceptance Criteria

- [ ] Agent can initiate graph traversal from one or more root object IDs (`--from <id>`)
- [ ] Agent can control traversal depth (`--depth <n>`; depth 0 = root only, depth 1 = direct neighbors)
- [ ] Agent can filter by relation type (`--relation <type>`, e.g., `mentions`)
- [ ] Agent can control edge direction (`--direction in|out|both`)
- [ ] Agent can request a specific output template (`--template <name>`)
- [ ] Output includes root object plus all discovered neighbors up to requested depth
- [ ] Output includes edge metadata (relation type, direction) for each connection
- [ ] Traversal depth in stored composition matches value submitted in request
- [ ] Cycles in the graph do not cause infinite traversal (each node visited once)
- [ ] `--depth 1` returns only direct neighbors; no second-order nodes included
- [ ] `--depth 2` returns neighbors-of-neighbors; root not duplicated in list
- [ ] `--direction in` returns only inbound edges; `--direction out` returns only outbound edges
- [ ] `--relation <type>` filters traversal to only that edge type
- [ ] Non-existent root ID returns 404; `--depth 0` returns root object only (not an error)
- [ ] Invalid `--direction` value returns 400 with descriptive error

---

## Implementation Notes

### CLI Interface

```bash
# Traverse from a single root object, depth 2
ctxt make graph --from o-abc123 --depth 2

# Filter by relation type and direction
ctxt make graph --from o-abc123 --relation mentions --direction out

# With output template
ctxt make graph --from o-abc123 --template technical

# Returns JSON with traversal result
{
  "composition_id": "gc-xyz789",
  "root_ids": ["o-abc123"],
  "depth": 2,
  "nodes": [...],
  "edges": [
    {"source": "o-abc123", "target": "o-def456", "relation": "mentions", "direction": "out"}
  ],
  "provenance": {
    "traversal_depth": 2,
    "edge_types": ["mentions"],
    "generated_at": "2025-01-18T10:30:45Z"
  }
}
```

### REST API

```
POST /compositions/graph
Content-Type: application/json

{
  "root_ids": ["o-abc123"],
  "depth": 2,
  "relation_filter": "mentions",
  "direction": "out",
  "template": "technical"
}

→ 200 OK  (composition persisted; retrievable via GET /compositions/graph/{id})
→ 400 Bad Request  (invalid direction value)
→ 404 Not Found    (non-existent root ID)
```

---

## E2E Test Checklist

### CLI → Server payload propagation
- [ ] `--from <id>` sends `root_ids` in POST body
- [ ] `--depth <n>` sends `depth: n` in POST body when provided
- [ ] `--relation <type>` sends `relation_filter` in POST body when provided
- [ ] `--direction <in|out|both>` sends `direction` in POST body when provided
- [ ] `--template <name>` sends `template` in POST body when provided

### Server-side receipt and storage
- [ ] POST /compositions/graph persists composition; GET /compositions/graph/{id} returns same record
- [ ] Stored composition lists all traversed node IDs and edge types
- [ ] Traversal depth stored matches `depth` value submitted in request
- [ ] Composition provenance includes graph edges used (source, target, relation)

### CLI output validation
- [ ] `ctxt make graph --from <id>` exits 0 and returns valid composition JSON
- [ ] Output includes root object plus all discovered neighbors up to requested depth
- [ ] Output includes edge metadata (relation type, direction) for each connection
- [ ] Traversal completes in acceptable time

### Graph traversal correctness
- [ ] `--depth 1` returns only direct neighbors; no second-order nodes included
- [ ] `--depth 2` returns neighbors of neighbors; root not duplicated in list
- [ ] `--direction in` returns only inbound edges; outbound neighbors excluded
- [ ] `--direction out` returns only outbound edges; inbound neighbors excluded
- [ ] `--relation mentions` filters traversal to only `mentions` edge type
- [ ] Cycles in graph do not cause infinite traversal (each node visited once)

### Error handling
- [ ] Non-existent root ID returns 404 with descriptive error
- [ ] `--depth 0` returns root object only (no traversal), not an error
- [ ] Invalid `--direction` value returns 400 with descriptive error

---

## Related Stories

- [US-0022: generate-brief-from-objects](./US-0022-generate-brief-from-objects.md) — Graph neighbors used to enrich briefs
- [US-0046: extract-relationships-between-entities](../enrichment/US-0046-extract-relationships-between-entities.md) — Source of graph edges
- [US-0052: graph-based-entity-search](../search/US-0052-graph-based-entity-search.md) — Entity-oriented graph search
- [US-0058: compose-impact-assessment](./US-0058-compose-impact-assessment.md) — Second-order effects via graph traversal

---

## Personas

- [Knowledge Workers](../../personas/knowledge-workers.md)
- [Researchers / OSINT](../../personas/researchers-osint.md)

---

## E2E Tests

> Not yet implemented.
