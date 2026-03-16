# US-0052: Graph-Based Entity Search

**System Types:** dpkms (self-hosted), dpkms cloud
**Personas:** [Agents/LLMs](../../personas/agents-llms-tools.md)

---

## User Goal

As a user, I want to search by entity reference and traverse the knowledge graph so that I find all objects related to a given entity, including indirect connections through shared mentions.

---

## Context

Structured knowledge is stored as a graph: each knowledge object can mention entities (people, systems, projects, concepts), and these mentions form edges between objects and entities. Querying by entity — `ctxt find "@auth-service"` — starts at that entity node and traverses outward, returning all objects that mention it directly (depth 1) or indirectly (depth 2+). Users can constrain traversal by relation type and hop depth. Each result includes a `distance` field indicating how many hops it is from the queried entity. New entity mentions are persisted as graph edges at ingestion time, and removing an object removes its corresponding edges.

---

## Acceptance Criteria

- [ ] User can search by entity reference: `ctxt find "@auth-service"`
- [ ] User can control traversal depth: `--depth 2`; result `distance` values do not exceed the specified depth
- [ ] User can filter by relation type: `--relation mentions`; only objects with that relation type are returned
- [ ] Direct mentions (depth=1) appear first; depth-2 objects appear with lower rank
- [ ] Results include a `distance` field indicating hop count from the queried entity
- [ ] `rank.explain.graph_match` includes distance and relation path
- [ ] Cyclic graph does not cause infinite traversal
- [ ] After ingesting an object with `@auth-service` mention, a graph edge is created (verifiable via DB or `ctxt entity show`)
- [ ] Removing an object removes its corresponding graph edges
- [ ] Entity with no connections returns an empty result set with 200 (not 404)
- [ ] Unknown entity slug returns 404 with an entity-not-found error
- [ ] Unknown relation type returns 400 with a relation-not-found error
- [ ] Same entity query via CLI, REST, and gRPC returns identical traversal results

---

## Implementation Notes

[Detailed implementation guide to be filled in]

---

## E2E Test Checklist

### CLI → Server Payload

- [ ] `ctxt find "@auth-service"` sends `entities=["@auth-service"]` (or query containing
      entity ref) in request payload; server receives entity identifier
- [ ] `ctxt find "@auth-service" --depth 2` sends `graph_depth=2` in payload; server receives
      and traverses up to 2 hops
- [ ] `ctxt find "@auth-service" --relation mentions` sends `relation=mentions` in payload;
      server filters traversal to that relation type
- [ ] `ctxt find "@auth-service" --limit 20` sends `limit=20` in payload; server receives it

### Server-Side Receipt and Storage

- [ ] Server receives entity identifier and resolves it against entity graph
- [ ] Server receives `graph_depth`; traversal stops at specified hop count (verifiable via
      result `distance` field)
- [ ] Server receives `relation` filter; only objects with that relation type to entity returned
- [ ] Server receives `limit`; result count ≤ limit
- [ ] Each result includes `rank.explain.graph_match` with distance and relation path
- [ ] Entity relationships stored in graph table/structure; new entity `ctxt add` creates graph
      node verifiable via DB

### Flags Coverage

- [ ] `--depth <n>` — `graph_depth` present in payload; result `distance` values ≤ n
- [ ] `--relation <type>` — present in payload; results only include specified relation type
- [ ] `--limit <n>` — present in payload; result count ≤ n
- [ ] Unknown entity slug → server returns 404 with entity-not-found error
- [ ] Unknown relation type → server returns 400 with relation-not-found error

### Graph Traversal Correctness

- [ ] Direct mentions of entity (depth=1) appear first in results
- [ ] With `--depth 2`, objects two hops away also appear (with lower rank than depth-1)
- [ ] Results include `distance` field indicating hop count from queried entity
- [ ] Cyclic graph does not cause infinite traversal

### Storage Verification

- [ ] After ingesting an object with `@auth-service` mention, graph edge created (verifiable
      via DB or `ctxt entity show @auth-service`)
- [ ] Removing object removes corresponding graph edges

### Error Handling

- [ ] Entity with no connections → empty result set with 200 (not 404)
- [ ] Depth exceeding max configured limit → capped at max; response notes actual depth used

### Interface Parity

- [ ] Same entity query via CLI, REST, and gRPC → identical traversal results

---

## Related Stories

- [US-0018](./US-0018-multi-strategy-search-execution.md) — Multi-Strategy Search Execution (graph is one strategy in the pipeline)
- [US-0021](./US-0021-search-with-result-explanation.md) — Search with Result Explanation (`rank.explain.graph_match` populated here)
- [US-0009](../enrichment/US-0009-extract-entities-and-mentions.md) — Extract Entities and Mentions (enrichment that creates graph edges)
- [US-0061](./US-0061-visual-similarity-search.md) — Visual Similarity Search (graph strategy used for entities detected in images)
