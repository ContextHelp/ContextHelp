# ADR-060 – Contextual Provenance Cascade in Search Results

> **Status:** Accepted
> **Date:** 2026-03-13
> **Author:** @jadb
> **Applies to:** dPKMS, ctxt
> **Supersedes:** None
> **Superseded by:** None

---

## Context

The current search result schema returns matched knowledge objects with their own metadata (type, tags, entities, summary, score). However, knowledge objects exist within a contextual hierarchy:

- A note belongs to a **collection** (e.g., `meetings`, `research`, `engineering`)
- A collection may belong to a **project** or **namespace**
- A path-level annotation may further qualify a subtree (e.g., "these are Q1 planning notes")

When a search returns results, the consumer (human or agent) must currently resolve contextual meaning themselves — they receive the leaf object but not the structural context that explains *why* this object is relevant or *what it belongs to*.

This gap was observed while evaluating [qmd](https://github.com/tobi/qmd), which uses a hierarchical "context cascade" model: contextual metadata defined at the collection or path level automatically propagates into search results for all matching documents. This makes results self-explanatory without requiring the consumer to re-traverse the graph.

---

## Decision

**Search results MUST include cascaded contextual provenance from the object's ancestry**, not just the object's own metadata.

Provenance is assembled from the following hierarchy, innermost wins on conflict:

```
Namespace metadata
  └─ Collection metadata
       └─ Path-level annotations
            └─ Object metadata
```

Each level may contribute:
- `context_label` — human-readable descriptor (e.g., "Q1 2026 Planning", "Engineering Notes")
- `namespace` — the `@namespace` slug
- `collection` — named group (e.g., `meetings`, `research`)
- `path_prefix` — filesystem or virtual path prefix matched

The assembled provenance is included in the `SearchResult` response as a `context` field.

### Result Schema Addition

```go
type SearchResult struct {
    ID          string          `json:"id"`
    Score       float64         `json:"score"`
    Object      KnowledgeObject `json:"object"`
    Snippet     string          `json:"snippet"`
    Context     ResultContext   `json:"context"`       // NEW
    MatchSource string          `json:"match_source"`  // "lexical" | "semantic" | "rerank"
}

type ResultContext struct {
    Namespace   string            `json:"namespace,omitempty"`
    Collection  string            `json:"collection,omitempty"`
    PathPrefix  string            `json:"path_prefix,omitempty"`
    Label       string            `json:"label,omitempty"`
    Annotations map[string]string `json:"annotations,omitempty"`
}
```

### Cascade Assembly

At query time, after candidate objects are retrieved and ranked, the query engine enriches each result by joining against the context registry:

```
1. Resolve object's namespace → collect namespace-level context
2. Resolve object's collection membership → collect collection-level context
3. Match object's path against path-prefix annotations → collect path-level context
4. Merge (innermost wins): namespace < collection < path < object
5. Attach ResultContext to SearchResult
```

This join is cheap: context entries are few, cached in memory, and indexed by namespace/collection/path prefix.

---

## Rationale

### Why cascade rather than flat metadata

Flat metadata stamped on the object itself would require re-indexing all objects when collection-level context changes (e.g., renaming a project). Cascade resolves at read time — collection metadata changes propagate instantly without re-indexing.

### Why include it in the result rather than forcing callers to resolve

Agents and humans consuming search results should not need a second round-trip to understand where a result came from. Embedding context in the result makes it self-describing — a single search call gives a complete picture.

### Why innermost wins

Objects that carry their own explicit context (e.g., a note tagged `@project.alpha`) should not be overridden by a collection-level label. The innermost (most specific) annotation is most accurate.

---

## Consequences

### Positive

- Search results are self-describing: consumers understand provenance without a second call
- Agents can use `context.collection` and `context.namespace` to filter or group results in post-processing
- Collection renames and re-annotations propagate without re-indexing
- Aligns with the graph-centric model (objects know their place in the graph)

### Negative

- Adds a join step to every search result assembly (mitigated: context entries are few and cached)
- Response payload is slightly larger
- Context registry must be populated at ingest time (new requirement on the write path)

### Neutral

- `ResultContext` is optional in the response schema — callers that don't need it can ignore it
- The context registry is a small in-memory structure; persistence in SQLite is a single table

---

## Implementation Notes

### Context Registry

```go
// ContextRegistry maps collection/path/namespace identifiers to annotations.
// Populated at ingest time, cached in memory, persisted in SQLite.
type ContextRegistry interface {
    Register(entry ContextEntry) error
    Resolve(namespace, collection, path string) ResultContext
}

type ContextEntry struct {
    Namespace   string
    Collection  string            // optional
    PathPrefix  string            // optional
    Label       string
    Annotations map[string]string
}
```

### Query Engine Integration

```go
// After ranking, enrich each result with cascaded context.
func (e *QueryEngine) enrich(results []SearchResult) []SearchResult {
    for i, r := range results {
        results[i].Context = e.contextRegistry.Resolve(
            r.Object.Namespace,
            r.Object.Collection,
            r.Object.SourcePath,
        )
    }
    return results
}
```

---

## References

- ADR-013 – Knowledge Graph and Mentions
- ADR-022 – Vector Semantic Search
- ADR-010 – Use Extended RSQL
- ADR-011 – Use Multi-Source Reranker
- design.md – Query Plan Compilation & Execution
- [qmd](https://github.com/tobi/qmd) — source of the cascade pattern

---
