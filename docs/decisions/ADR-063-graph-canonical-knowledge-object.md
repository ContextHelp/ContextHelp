# ADR-063 – Graph-Canonical KnowledgeObject with Document Projection

> **Status:** Proposed
> **Date:** 2026-03-30
> **Author:** $USER
> **Applies to:** dPKMS, ctxt, pluginapi
> **Supersedes:** None
> **Superseded by:** N/A
> **References:** ADR-004, ADR-007, ADR-013, ADR-049, ADR-062

---

## Context

`KnowledgeObject` (`pkg/pluginapi/pluginapi.go`) is a flat struct. Extraction steps bolt on typed
arrays (`Sections`, `Tags`, `Decisions`, `Tasks`, `Mentions`) directly to top-level fields. No
stable sub-object identity: a tag has no node ID; a section cannot be referenced by an edge.
FTS and vector indexes built ad hoc from raw scalar fields (`TextContent`, `Summaries`).

Failure modes:

1. **No intra-object graph** — sub-objects (sections, tags, decisions) are anonymous; cannot
   carry edges, provenance, or confidence scores.
2. **Array-append pattern** — every extraction step mutates a flat slice; ordering, deduplication,
   and merge semantics undefined.
3. **Ad-hoc index inputs** — `FTSIndexed`/`VectorIndexed` booleans track state but don't define
   what text was indexed or how; reproducing indexes requires re-running full pipelines.
4. **No projection layer** — display, search, and export consume the same raw struct; format
   concerns bleed into enrichment logic.
5. **Inter-object edges (ADR-049) orphaned** — `edges` table references object IDs; intra-object
   nodes (a specific decision, a specific mention) have no addressable identity for finer-grained
   edges.

Diagram: [graph diagram](ADR-063-graph-canonical-knowledge-object/ko-graph-structure-v1.mmd)

---

## Decision

### 1. Canonical form

`KnowledgeObject` canonical form = **identity scalars** + **`ObjectGraph`**.

Identity scalars (unchanged): `ID`, `Type`, `Subtype`, `Source`, `Pipeline`, `ProfileID`,
`ContentHash`, `Status`, `CreatedAt`, `UpdatedAt`, and lifecycle fields from ADR-062.

```
// pseudocode
type KnowledgeObject struct {
    // --- identity scalars (unchanged) ---
    ID, Type, Subtype, Source, Pipeline, ProfileID string
    ContentHash, Status string
    CreatedAt, UpdatedAt time.Time
    // ... other scalars ...

    // --- graph (write source of truth) ---
    Graph ObjectGraph

    // --- projections (derived on read; never persisted in graph) ---
    // DocumentProjection and IndexProjection populated by storage layer
}

type ObjectGraph struct {
    Nodes []GraphNode
    Edges []GraphEdge
}

type GraphNode struct {
    ID       string          // stable UUID within object scope
    Type     GraphNodeType   // section | tag | entity_mention | decision | task |
                             //   summary | code_block
    Label    string
    Content  string
    Order    int
    Metadata map[string]any
}

// Inter-object edges MUST go to `edges` table (ADR-049); `GraphEdge` is intra-object only.
type GraphEdge struct {
    ID     string
    Type   GraphEdgeType   // contains | references | resolves_to | derives_from
    FromID string          // GraphNode.ID within this object only
    ToID   string          // GraphNode.ID within this object only
    Weight float64
}

type GraphNodeType string
const (
    NodeSection      GraphNodeType = "section"
    NodeTag          GraphNodeType = "tag"
    NodeEntityMention GraphNodeType = "entity_mention"
    NodeDecision     GraphNodeType = "decision"
    NodeTask         GraphNodeType = "task"
    NodeSummary      GraphNodeType = "summary"
    NodeCodeBlock    GraphNodeType = "code_block"
)

type GraphEdgeType string
const (
    EdgeContains    GraphEdgeType = "contains"
    EdgeReferences  GraphEdgeType = "references"
    EdgeResolvesTo  GraphEdgeType = "resolves_to"
    EdgeDerivedFrom GraphEdgeType = "derives_from" // intra-object only; cross-object → ADR-049 `edges`
)
```

### 2. Storage

`ObjectGraph` persisted as JSON blob in `objects.graph_json` (new column). `ObjectGraph` is the
**write source of truth** for all intra-object structure.

Existing flat columns (`sections`, `tags`, `decisions`, `tasks`) remain for compatibility; not
removed yet. Treated as projection cache (may lag `graph_json` by one migration cycle).

Inter-object edges stay in `edges` table (ADR-049). Intra-object edges live inside `graph_json`
only; not duplicated to `edges`.

### 3. Projections (derived on read)

**DocumentProjection** — for display / rendering:

```
// pseudocode
type DocumentProjection struct {
    Title    string
    Body     string
    Sections []SectionView   // derived from NodeSection nodes, ordered by Order
    Metadata map[string]any
}
```

**IndexProjection** — for search infra:

```
// pseudocode
type IndexProjection struct {
    FTSBody       string     // concatenated text from NodeSection + NodeSummary nodes
    Tags          []string   // labels from NodeTag nodes
    Mentions      []string   // labels from NodeEntityMention nodes
    EmbeddingText string     // canonical text fed to embedding model
}
```

Projections invoked by storage `Get`/`List`; not persisted in `graph_json`.

### 4. Flat-field compatibility

Existing flat fields (`Sections []Section`, `Tags []Tag`, `Decisions []Decision`,
`Tasks []Task`, `Summaries []string`) become **projection outputs** populated from `ObjectGraph`
by the storage layer. No column removal in this ADR. Removal deferred to post-migration ADR.

### 5. pluginapi surface additions

Public `pluginapi` gains: `ObjectGraph`, `GraphNode`, `GraphEdge`, `GraphNodeType`,
`GraphEdgeType`, and typed constants listed above.

Existing flat slice fields on `KnowledgeObject` retained (aliased from projections).
Pipeline steps may write to `Graph` directly; storage layer backfills flat fields on write.

> Note: ingest and enrichment steps MUST write to `Graph.Nodes`; flat-field writes treated as
> legacy until backfill completes.

---

## Consequences

### Positive

- **Single write target for enrichment** — no merge ambiguity between flat-field and graph writes.
- **Stable intra-object identity** — every section/tag/decision/task has a UUID; can be
  referenced by intra- and inter-object edges.
- **Defined write semantics** — steps append typed nodes to `Graph.Nodes`; no array-append
  ambiguity, no merge conflicts.
- **Reproducible indexes** — `IndexProjection` defines exactly what text reaches FTS/vector;
  deterministic, re-derivable without re-running pipelines.
- **Display/search decoupled** — `DocumentProjection` for render, `IndexProjection` for search;
  enrichment steps never touch display logic.
- **Inter-object edge precision** — ADR-049 edges can target intra-object node IDs once graph
  is addressable (future: `edges.to_node_id`).

### Negative / risks

- **Storage Create/Update** must serialise `ObjectGraph` to `graph_json`; additional marshal
  cost per write.
- **Storage Get/List** must invoke projections; adds deserialization + projection pass per read.
- **Migration required** — existing rows have no `graph_json`; backfill must reconstruct
  `ObjectGraph` from flat columns for every existing object.
- **pluginapi surface widens** — new exported types increase API surface; semver bump required.
- **Plugin breakage risk** — steps that write directly to flat fields bypass the graph;
  must be audited and migrated.

### Neutral

- Flat columns retained; no schema removal in this change. Migration cost deferred.
- `Draft = KnowledgeObject` alias unaffected.

---

## Migration

1. Add `graph_json TEXT` column to `objects` table (nullable; empty = legacy row).
2. Backfill job: for each row with `graph_json IS NULL`, reconstruct `ObjectGraph` from existing
   flat columns (`sections`, `tags`, `decisions`, `tasks`, `summaries`), generate stable node
   UUIDs (hash of `object_id + type + ordinal-within-type`, 0-indexed per type). Collisions
   impossible by construction. Write JSON to `graph_json`.
3. After backfill: storage `Create`/`Update` writes `graph_json`; `Get`/`List` project from it.
4. Flat column removal deferred to follow-on ADR once all writers confirmed graph-aware.

---

## References

- ADR-004 — ingestion pipeline architecture
- ADR-007 — pipeline step contracts
- ADR-013 — storage driver interface
- ADR-049 — inter-object edge schema
- ADR-062 — information/knowledge lifecycle model
- `pkg/pluginapi/pluginapi.go` — current flat struct (exact code; see repo)
