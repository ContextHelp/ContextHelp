# Design: SQLite Driver Switch + Pluggable Vector Storage

**Date:** 2026-02-18
**Status:** Approved
**Skeleton:** 6 (Hybrid Retrieval)
**Related:** progressive-retrieval-sufficiency, llm-provider-support

---

## Problem

The codebase uses `modernc.org/sqlite` (pure Go, CGo-free). This driver
cannot load C extensions. Vector search requires sqlite-vec, which is a
native C extension. No vector storage exists in the system today.

---

## Decisions

1. Switch SQLite driver from `modernc.org/sqlite` to `mattn/go-sqlite3`
2. Load sqlite-vec via `sqlite_vec.Auto()` for vector search
3. Define a `VectorStore` interface for backend-agnostic vector ops
4. Select backend from config (sqlite-vec now; pgvector, qdrant later)
5. Merge all migrations into a single `001_initial.sql`
6. Template the vector table DDL for configurable embedding dimension
7. No stubs — embedding pipeline must be real before this lands

---

## Prerequisites (Must Land First)

The LLM provider support plan must deliver:

1. `EmbeddingProvider` interface
2. At least one real backend (OpenAI, Ollama, or local model)
3. Real embedding pipeline step (`Embed()` → `VectorStore.Upsert()`)

This design does not land without a working end-to-end path:
ingest → embed → vector store → vector search.

---

## 1. Driver Switch

### What changes

**`internal/storage/sqlite/driver.go`** (1 file):

```
// before
import _ "modernc.org/sqlite"
db, err := sql.Open("sqlite", path)

// after
import (
    _ "github.com/mattn/go-sqlite3"
    sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
)
sqlite_vec.Auto()
db, err := sql.Open("sqlite3", path)
```

**`go.mod`:** Drop `modernc.org/sqlite`, add `mattn/go-sqlite3` and
`sqlite-vec-go-bindings/cgo`.

**Everything else untouched.** All 12 store files use `database/sql`
with zero driver-specific calls.

### Migrations

Merge migrations 001, 002, 003 into a single `001_initial.sql`.
Include the `vec_objects` virtual table. Drop `schema_version`
versioning — single migration, software is pre-release.

The vector table dimension is templated at runtime from config:

```sql
CREATE VIRTUAL TABLE IF NOT EXISTS vec_objects USING vec0(
    id TEXT PRIMARY KEY,
    embedding float[{dimension}]
);
```

### CI impact

Build environments need `gcc`. Cross-compilation uses standard
tooling (`mingw-w64` for Windows, `musl-cross` for Linux from
macOS). `goreleaser` handles this.

---

## 2. VectorStore Interface

### Types

```
// internal/storage/types.go

VectorHit {
    ID       string
    Score    float64
    Metadata map[string]any
}
```

### Interface

```
// internal/storage/storage.go

VectorStore {
    Upsert(ctx, id string, vector []float32,
           metadata map[string]any) error
    Search(ctx, vector []float32, topK int,
           filter ObjectFilter) ([]VectorHit, error)
    Delete(ctx, id string) error
}
```

### StorageDriver

Add `Vectors() VectorStore` to the `StorageDriver` interface.
The sqlite driver returns its bundled sqlite-vec implementation.

---

## 3. Config-Driven Backend Selection

### Config

```yaml
vector:
  backend: sqlite-vec    # sqlite-vec | pgvector | qdrant
  dimension: 384
  endpoint: ""           # for external backends
```

Added to `Config` struct as `Vector VectorConfig`.

### Factory

```
// internal/storageutil/

NewVectorStore(cfg VectorConfig, driver StorageDriver) -> VectorStore
```

- `"sqlite-vec"` → `driver.Vectors()` (bundled)
- `"pgvector"` → pgvector impl (future)
- `"qdrant"` → qdrant impl (future)
- unknown → error

`Service` receives `VectorStore` as a constructor dependency.
The factory resolves it from config before `Service` is created.

---

## 4. sqlite-vec Implementation

**New file:** `internal/storage/sqlite/vectors.go`

Uses sqlite-vec's `vec0` virtual table:

- **Upsert:** `INSERT OR REPLACE INTO vec_objects(id, embedding)
  VALUES (?, ?)` with `sqlite_vec.SerializeFloat32()` for encoding
- **Search:** KNN via `vec_objects` virtual table, join to `objects`
  for filtering
- **Delete:** `DELETE FROM vec_objects WHERE id = ?`

---

## 5. Integration

### Embedding pipeline step

Rewrite from stub to real implementation:

1. Call `EmbeddingProvider.Embed(text)` → `[]float32`
2. Call `VectorStore.Upsert(obj.ID, vector, nil)`
3. Set `obj.VectorIndexed = true`

### Search engine

Not modified. Vector search integration into hybrid queries
is separate Skeleton 6 work. `VectorStore.Search()` is available
as a standalone capability for direct callers.

---

## 6. Testing

### Fixture pattern

Embedding test fixtures live in `testdata/embeddings/`.
Each fixture: JSON keyed by input text → vector.

- First run: fixture file missing → call real `EmbeddingProvider`,
  write results to fixture, then use them
- Subsequent runs: load from fixture, skip provider call
- CI: fixtures committed to git, no provider dependency
- Regenerate: delete fixture file to force re-generation

### Test coverage

**Unit:**
- `vectors_test.go`: Upsert, Search (KNN order), Delete,
  upsert-overwrite, empty-table search, topK limit

**Integration:**
- Ingest → embed → store → search returns correct object
- Delete object → vector removed
- Dimension mismatch → error (not panic)

---

## Scope Boundary

This design delivers:
- Driver switch (modernc → mattn)
- VectorStore interface + config + factory
- sqlite-vec implementation
- Real embedding step (requires LLM provider plan first)

This design does NOT deliver:
- Hybrid search (FTS + vector merge)
- Progressive retrieval / sufficiency checking
- pgvector or qdrant implementations

Those build on top of this foundation.
