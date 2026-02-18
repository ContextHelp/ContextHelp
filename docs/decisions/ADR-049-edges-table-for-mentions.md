# ADR-049 – Edges Table as Source of Truth for Mentions

> **Status:** Accepted
> **Date:** 2026-02-18
> **Author:** @jadb
> **Applies to:** dPKMS
> **Supersedes:** None
> **Superseded by:** N/A

---

## Context

Three documentation sources describe mention storage differently, creating an inconsistency that must be resolved before implementing the storage layer (Phase 6):

1. **`dpkms/storage.md`** describes a `bookmark_mentions(entity_id, bookmark_id)` join table for efficient mention-based queries and backlink lookups.

2. **`design.md`** and **`architecture.md`** store mentions as a JSON array inside the `objects` row: `mentions: ["@entity.slug", ...]`.

3. **`architecture.md`** defines an `edges` table modeling typed relationships between any two entities in the system:
   ```sql
   edges:
     - id (UUID)
     - from_type, from_id
     - to_type, to_id
     - edge_type (mentions, related, derives_from)
     - weight
     - metadata (JSON)
     - created_at
   ```

The `edges` table already captures object↔entity relationships as a first-class concept with typed edges, weights, and metadata. A separate `bookmark_mentions` join table would duplicate this capability for a single edge type. Meanwhile, the JSON array alone cannot support efficient filtered queries or graph traversal.

**The question:** Which of these three representations is the canonical source of truth for mention relationships?

---

## Decision

**The `edges` table is the single source of truth for all mention relationships.** The `mentions` JSON array in the `objects` row is a denormalized read cache for display convenience only. The `bookmark_mentions` join table described in `storage.md` will not be implemented.

### How It Works

**Write path (ingestion):**
1. Pipeline extracts mentions from content.
2. Entity resolver validates/creates entities.
3. For each mention, an edge is created:
   ```sql
   INSERT INTO edges (id, from_type, from_id, to_type, to_id, edge_type, weight, created_at)
   VALUES (uuid, 'object', object_id, 'entity', entity_slug, 'mentions', 1.0, now());
   ```
4. The `mentions` JSON array in the objects row is updated as a denormalized cache.

**Read path (queries):**
- **Mention filtering** (`mention:@ui.best-practice`): query the `edges` table.
- **Backlinks** (which objects mention entity X): query edges `WHERE to_id = X AND edge_type = 'mentions'`.
- **Display** (show mentions on an object): read the JSON array from the objects row (fast, no join).
- **Graph traversal**: walk the edges table using adjacency indexes.

**Consistency rule:** If the edges table and JSON array ever disagree, the edges table wins. The `dpkms housekeeping reindex` command will rebuild JSON arrays from edges.

---

## Rationale

### Alternatives Considered

#### 1. Separate `bookmark_mentions` Join Table (Rejected)

A dedicated join table (`entity_id`, `bookmark_id`) as described in `storage.md`.

**Rejected because:**
- Duplicates the `edges` table's capability for a single edge type
- Cannot represent weighted mentions, metadata, or multiple edge types
- Two tables to maintain for the same data
- Graph traversal queries still need the edges table, making the join table redundant
- Increases schema complexity without benefit

#### 2. JSON Array Only (Rejected)

Store mentions only as `mentions: ["@slug1", "@slug2"]` in the objects JSON.

**Rejected because:**
- Requires full-scan or JSON indexing for mention-based filtering
- SQLite JSON index support is limited (no `json_extract` on arrays without generated columns)
- No efficient backlink queries (which objects mention entity X?)
- No graph traversal without parsing JSON from every row
- Unacceptable performance at scale (1000+ objects)

#### 3. Edges Table Only — No JSON Cache (Considered)

Use edges table exclusively, no JSON array in objects.

**Rejected because:**
- Every object display requires a JOIN to edges table
- Increases read latency for the most common operation (view an object)
- The JSON cache is cheap to maintain and eliminates JOINs for display

### Benefits of Chosen Approach

- **Single source of truth** for graph relationships — no sync issues between tables
- **Unified graph model** — mentions, related, derives_from all in one table
- **Efficient queries** — edges table has adjacency indexes for fast lookups
- **Fast display** — JSON array avoids JOINs for the common case
- **Extensible** — new edge types (e.g., `cites`, `contradicts`) added without schema changes
- **Consistent with architecture.md** — the edges table is already designed for this

---

## Consequences

### Positive

- Eliminates the `bookmark_mentions` table from the schema — simpler migration
- All relationship queries go through one table with one set of indexes
- Graph traversal works naturally across mention, related, and derives_from edges
- Plugin-defined edge types fit the same model

### Negative

- Denormalized JSON array must be kept in sync with edges table
- Slightly more complex write path (write edge + update JSON cache)
- If JSON cache drifts, `housekeeping reindex` is needed

### Neutral

- `storage.md` should be updated to reflect this decision
- `design.md` JSON array description remains accurate (it's the cache)

---

## Implementation Notes

**Indexes on edges table:**
```sql
CREATE INDEX idx_edges_from ON edges(from_type, from_id);
CREATE INDEX idx_edges_to ON edges(to_type, to_id);
CREATE INDEX idx_edges_type ON edges(edge_type);
CREATE INDEX idx_edges_from_type ON edges(from_type, from_id, edge_type);
```

**Backlink query:**
```sql
SELECT from_id FROM edges
WHERE to_type = 'entity' AND to_id = ? AND edge_type = 'mentions';
```

**Mention filter query:**
```sql
SELECT o.* FROM objects o
JOIN edges e ON e.from_type = 'object' AND e.from_id = o.id
WHERE e.to_type = 'entity' AND e.to_id = ? AND e.edge_type = 'mentions';
```

---

## References

- **dpkms/storage.md** — `bookmark_mentions` table (superseded by this ADR)
- **architecture.md:856-906** — Core tables including edges
- **design.md:336-409** — Knowledge Object schema with mentions JSON array
- **cross-package-contracts.md:93-123** — EntityStore interface
- ADR-013 – Knowledge Graph and Mentions
- ADR-021 – Multi-Backend Storage Strategy

---
