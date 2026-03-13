# SQLite Driver Switch + Vector Storage Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans
> to implement this plan task-by-task.

**Goal:** Switch SQLite driver to mattn/go-sqlite3, add a pluggable
VectorStore interface, and implement sqlite-vec as the first backend.

**Architecture:** Replace the CGo-free modernc driver with mattn's CGo
driver to enable native C extension loading. Define a VectorStore
interface in the storage layer. Implement sqlite-vec behind it. Wire
backend selection through config.

**Tech Stack:** mattn/go-sqlite3, sqlite-vec CGo bindings, go-embeddings

**Design doc:** `docs/plans/2026-02-18-sqlite-driver-vector-storage-design.md`

**Prerequisite:** LLM provider plan (`docs/plans/llm-provider-support.md`)
must deliver `EmbeddingProvider` + one real backend first. This plan
does not land without a working end-to-end embedding path.

---

## Task List

1. Switch SQLite driver (modernc → mattn)
2. Merge migrations into single 001_initial.sql
3. Add VectorStore interface and types
4. Add VectorConfig to config
5. Implement sqlite-vec VectorStore
6. Add VectorStore to StorageDriver + factory
7. Rewrite embedding pipeline step (real, not stub)
8. Add embedding test fixtures
9. Integration test: ingest → embed → search
10. Update CI for CGo builds

---

### Task 1: Switch SQLite Driver

**Files:**
- Modify: `internal/storage/sqlite/driver.go`
- Modify: `go.mod`

**Step 1: Update go.mod**

Run:
```
go get github.com/mattn/go-sqlite3
go get github.com/asg017/sqlite-vec-go-bindings/cgo
```

**Step 2: Swap driver import and Open call**

In `internal/storage/sqlite/driver.go`, replace:

```go
import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)
```

With:

```go
import (
	"context"
	"database/sql"
	"fmt"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)
```

Add `sqlite_vec.Auto()` call before `sql.Open`:

```go
func New(path string) (*Driver, error) {
	sqlite_vec.Auto()
	db, err := sql.Open("sqlite3", path)
```

**Step 3: Remove modernc from go.mod**

Run:
```
go mod tidy
```

**Step 4: Run existing tests to verify nothing breaks**

Run: `go test ./internal/storage/sqlite/... -v -count=1`
Expected: All existing tests pass (driver is transparent to SQL).

**Step 5: Commit**

```
git add internal/storage/sqlite/driver.go go.mod go.sum
git commit -m "feat(storage): switch sqlite driver to mattn/go-sqlite3

Enable C extension loading for sqlite-vec."
```

---

### Task 2: Merge Migrations

**Files:**
- Modify: `internal/storage/sqlite/migrations/001_initial.sql`
- Delete: `internal/storage/sqlite/migrations/002_feeds_and_batches.sql`
- Delete: `internal/storage/sqlite/migrations/003_content_hash_reinforcement.sql`
- Modify: `internal/storage/sqlite/migrations.go`

**Step 1: Merge all SQL into 001_initial.sql**

Append the content of 002 and 003 to the end of 001_initial.sql.
From 002, add: feeds, feed_items, batches tables + indexes.
From 003, add the three columns directly to the objects CREATE TABLE
(content_hash, reinforcement_count, last_reinforced_at) plus the
unique index on content_hash.

Do NOT use ALTER TABLE — put columns in the original CREATE TABLE.

Add the vec_objects table at the end. Use `{DIMENSION}` placeholder
(the driver will template it):

```sql
-- Vector search (sqlite-vec)
CREATE VIRTUAL TABLE IF NOT EXISTS vec_objects USING vec0(
    id TEXT PRIMARY KEY,
    embedding float[{DIMENSION}]
);
```

**Step 2: Delete migration 002 and 003 files**

Delete `internal/storage/sqlite/migrations/002_feeds_and_batches.sql`
and `internal/storage/sqlite/migrations/003_content_hash_reinforcement.sql`.

**Step 3: Update migrations.go**

Replace the migrations slice with a single entry. Add dimension
templating:

```go
//go:embed migrations/001_initial.sql
var migration001 string

func (d *Driver) Migrate(ctx context.Context, dimension int) error {
	if dimension == 0 {
		dimension = 384
	}
	ddl := strings.ReplaceAll(migration001, "{DIMENSION}", strconv.Itoa(dimension))

	if _, err := d.db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("apply migration: %w", err)
	}
	return nil
}
```

Remove the schema_version table creation and version tracking logic.
Remove the `migration` struct and `migrations` slice.

**Step 4: Update Init() call site**

`Init` currently calls `d.Migrate(ctx)`. Update the `Driver` to accept
dimension at construction or via Init. Simplest: store dimension on
the Driver struct, set from config during `New()`.

Update `New()` signature:

```go
func New(path string, vectorDimension int) (*Driver, error)
```

Update Init:

```go
func (d *Driver) Init(ctx context.Context) error {
	return d.Migrate(ctx, d.vectorDimension)
}
```

Update all callers of `New()` (storageutil/factory.go,
storageutil/testutil.go, driver_test.go) to pass dimension.
Use 384 as default in test helpers.

**Step 5: Run tests**

Run: `go test ./internal/storage/sqlite/... -v -count=1`
Expected: All pass.

**Step 6: Commit**

```
git add internal/storage/sqlite/
git commit -m "refactor(storage): merge migrations into single initial schema

Include vec_objects table with configurable dimension.
Pre-release software; no production data to migrate."
```

---

### Task 3: Add VectorStore Interface and Types

**Files:**
- Modify: `internal/storage/types.go`
- Modify: `internal/storage/storage.go`
- Test: `internal/storage/storage_test.go` (compile check)

**Step 1: Write the VectorHit type**

Add to `internal/storage/types.go`:

```go
// VectorHit represents a vector search result with similarity score.
type VectorHit struct {
	ID       string
	Score    float64
	Metadata map[string]any
}
```

**Step 2: Write the VectorStore interface**

Add to `internal/storage/storage.go`:

```go
// VectorStore persists and searches vector embeddings.
type VectorStore interface {
	Upsert(ctx context.Context, id string, vector []float32,
		metadata map[string]any) error
	Search(ctx context.Context, vector []float32, topK int,
		filter ObjectFilter) ([]VectorHit, error)
	Delete(ctx context.Context, id string) error
}
```

**Step 3: Verify compilation**

Run: `go build ./internal/storage/...`
Expected: Compiles.

**Step 4: Commit**

```
git add internal/storage/types.go internal/storage/storage.go
git commit -m "feat(storage): add VectorStore interface and VectorHit type"
```

---

### Task 4: Add VectorConfig to Config

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

**Step 1: Write the failing test**

Add to `internal/config/config_test.go`:

```go
func TestDefaultVectorConfig(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Vector.Backend != "sqlite-vec" {
		t.Errorf("backend: got %q, want sqlite-vec", cfg.Vector.Backend)
	}
	if cfg.Vector.Dimension != 384 {
		t.Errorf("dimension: got %d, want 384", cfg.Vector.Dimension)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -run TestDefaultVectorConfig -v`
Expected: FAIL (no Vector field on Config).

**Step 3: Add VectorConfig type and wire defaults**

Add to `internal/config/config.go`:

```go
// VectorConfig controls vector storage backend selection.
type VectorConfig struct {
	Backend   string `mapstructure:"backend"`
	Dimension int    `mapstructure:"dimension"`
	Endpoint  string `mapstructure:"endpoint"`
}
```

Add field to Config struct:

```go
// Vector configuration
Vector VectorConfig `mapstructure:"vector"`
```

Add defaults in `setDefaults()`:

```go
v.SetDefault("vector.backend", "sqlite-vec")
v.SetDefault("vector.dimension", 384)
v.SetDefault("vector.endpoint", "")
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/config/... -run TestDefaultVectorConfig -v`
Expected: PASS.

**Step 5: Commit**

```
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add vector storage configuration

Defaults to sqlite-vec with dimension 384."
```

---

### Task 5: Implement sqlite-vec VectorStore

**Files:**
- Create: `internal/storage/sqlite/vectors.go`
- Create: `internal/storage/sqlite/vectors_test.go`

**Step 1: Write failing tests**

Create `internal/storage/sqlite/vectors_test.go`:

```go
package sqlite

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestVectorUpsertAndSearch(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()
	vs := d.Vectors()

	// Insert a vector
	vec := []float32{0.1, 0.2, 0.3}
	err := vs.Upsert(ctx, "obj-1", vec, nil)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// Search should return it
	query := []float32{0.1, 0.2, 0.3}
	hits, err := vs.Search(ctx, query, 10, storage.ObjectFilter{})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits: got %d, want 1", len(hits))
	}
	if hits[0].ID != "obj-1" {
		t.Errorf("id: got %q, want obj-1", hits[0].ID)
	}
}

func TestVectorDelete(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()
	vs := d.Vectors()

	vs.Upsert(ctx, "obj-1", []float32{0.1, 0.2, 0.3}, nil)
	err := vs.Delete(ctx, "obj-1")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	hits, err := vs.Search(ctx, []float32{0.1, 0.2, 0.3}, 10,
		storage.ObjectFilter{})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("hits: got %d, want 0", len(hits))
	}
}

func TestVectorUpsertOverwrite(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()
	vs := d.Vectors()

	vs.Upsert(ctx, "obj-1", []float32{0.1, 0.2, 0.3}, nil)
	vs.Upsert(ctx, "obj-1", []float32{0.9, 0.8, 0.7}, nil)

	hits, err := vs.Search(ctx, []float32{0.9, 0.8, 0.7}, 10,
		storage.ObjectFilter{})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits: got %d, want 1", len(hits))
	}
}

func TestVectorSearchEmpty(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()
	vs := d.Vectors()

	hits, err := vs.Search(ctx, []float32{0.1, 0.2, 0.3}, 10,
		storage.ObjectFilter{})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("hits: got %d, want 0", len(hits))
	}
}

func TestVectorSearchTopK(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()
	vs := d.Vectors()

	for i := 0; i < 5; i++ {
		v := float32(i) * 0.1
		vs.Upsert(ctx, fmt.Sprintf("obj-%d", i),
			[]float32{v, v, v}, nil)
	}

	hits, err := vs.Search(ctx, []float32{0.4, 0.4, 0.4}, 2,
		storage.ObjectFilter{})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 2 {
		t.Errorf("hits: got %d, want 2", len(hits))
	}
}
```

Note: `newTestDriver` must be updated so the vec_objects table
uses dimension=3 for tests. Update `driver_test.go`'s helper
to pass dimension=3, or add a `newTestDriverWithDimension` helper.

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/storage/sqlite/... -run TestVector -v`
Expected: FAIL (no Vectors() method, no VectorStore type).

**Step 3: Write the implementation**

Create `internal/storage/sqlite/vectors.go`:

```go
package sqlite

import (
	"context"
	"database/sql"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// VectorStore implements storage.VectorStore using sqlite-vec.
type VectorStore struct {
	db *sql.DB
}

func (s *VectorStore) Upsert(
	ctx context.Context,
	id string,
	vector []float32,
	metadata map[string]any,
) error {
	blob, err := sqlite_vec.SerializeFloat32(vector)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		"INSERT OR REPLACE INTO vec_objects(id, embedding) VALUES (?, ?)",
		id, blob)
	return err
}

func (s *VectorStore) Search(
	ctx context.Context,
	vector []float32,
	topK int,
	filter storage.ObjectFilter,
) ([]storage.VectorHit, error) {
	blob, err := sqlite_vec.SerializeFloat32(vector)
	if err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, distance
		 FROM vec_objects
		 WHERE embedding MATCH ?
		 ORDER BY distance
		 LIMIT ?`,
		blob, topK)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hits []storage.VectorHit
	for rows.Next() {
		var h storage.VectorHit
		if err := rows.Scan(&h.ID, &h.Score); err != nil {
			return nil, err
		}
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

func (s *VectorStore) Delete(
	ctx context.Context,
	id string,
) error {
	_, err := s.db.ExecContext(ctx,
		"DELETE FROM vec_objects WHERE id = ?", id)
	return err
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/storage/sqlite/... -run TestVector -v`
Expected: PASS.

**Step 5: Commit**

```
git add internal/storage/sqlite/vectors.go \
        internal/storage/sqlite/vectors_test.go
git commit -m "feat(storage): implement sqlite-vec VectorStore

Upsert, Search (KNN), Delete backed by vec0 virtual table."
```

---

### Task 6: Add VectorStore to StorageDriver + Factory

**Files:**
- Modify: `internal/storage/storage.go`
- Modify: `internal/storage/sqlite/driver.go`
- Create: `internal/storageutil/vector_factory.go`
- Modify: `internal/storageutil/factory.go`
- Modify: `internal/storageutil/testutil.go`
- Modify: `internal/service/service.go`
- Modify: `internal/service/types.go` (if needed)

**Step 1: Add Vectors() to StorageDriver**

In `internal/storage/storage.go`, add to StorageDriver interface:

```go
Vectors() VectorStore
```

**Step 2: Implement on sqlite Driver**

In `internal/storage/sqlite/driver.go`, add:

```go
vectors *VectorStore
```

to the Driver struct. In `New()`, initialize it:

```go
d.vectors = &VectorStore{db: db}
```

Add accessor:

```go
func (d *Driver) Vectors() storage.VectorStore { return d.vectors }
```

**Step 3: Create vector factory**

Create `internal/storageutil/vector_factory.go`:

```go
package storageutil

import (
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// NewVectorStore resolves a VectorStore from config.
// Falls back to the driver's bundled implementation for sqlite-vec.
func NewVectorStore(
	cfg config.VectorConfig,
	driver storage.StorageDriver,
) (storage.VectorStore, error) {
	switch cfg.Backend {
	case "sqlite-vec", "":
		return driver.Vectors(), nil
	case "pgvector":
		return nil, fmt.Errorf("pgvector backend not yet implemented")
	case "qdrant":
		return nil, fmt.Errorf("qdrant backend not yet implemented")
	default:
		return nil, fmt.Errorf("unknown vector backend: %s", cfg.Backend)
	}
}
```

**Step 4: Add VectorStore to Service**

In `internal/service/service.go`, add field and update constructor:

```go
type Service struct {
	Store     storage.StorageDriver
	Queue     *jobs.Queue
	Pipes     pipeline.Registry
	Search    *search.Engine
	Vectors   storage.VectorStore
	Discovery *steps.StepDiscovery
	Executor  *steps.StepExecutor
}

func New(
	store storage.StorageDriver,
	queue *jobs.Queue,
	pipes pipeline.Registry,
	engine *search.Engine,
	vectors storage.VectorStore,
	stepsPath string,
) *Service {
```

**Step 5: Update all callers of service.New()**

These files call `service.New()` and need the new `vectors` param:

- `cmd/ctxt/cmd/helpers.go` (or wherever newService is)
- `cmd/dpkms/cmd/serve.go`
- `test/integration/e2e_test.go` (`startTestEnv`)

For now, pass `driver.Vectors()` directly at each call site.

**Step 6: Update storageutil.NewDriver signature**

Pass dimension through:

```go
func NewDriver(typ, path string, vectorDimension int) (
	storage.StorageDriver, error,
)
```

Update callers. Update `NewTestDriver` to default dimension=384.

**Step 7: Run full test suite**

Run: `go test ./... -count=1`
Expected: All pass.

**Step 8: Commit**

```
git add internal/storage/storage.go \
        internal/storage/sqlite/driver.go \
        internal/storageutil/ \
        internal/service/service.go \
        cmd/ test/
git commit -m "feat(storage): wire VectorStore into driver and service

Config-driven backend selection via storageutil.NewVectorStore.
sqlite-vec is the default; pgvector and qdrant are planned."
```

---

### Task 7: Rewrite Embedding Pipeline Step

**Files:**
- Modify: `internal/pipeline/steps/embedding.go`
- Modify: `internal/pipeline/steps/embedding_test.go`

**Prerequisite:** `internal/llm/embed.go` must exist with a working
`Embedder` interface from the LLM provider plan. If it does not exist
yet, STOP. That plan must land first.

**Step 1: Write failing test**

Replace `internal/pipeline/steps/embedding_test.go`:

```go
package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type mockEmbedder struct {
	vec []float32
}

func (m *mockEmbedder) Embed(_ context.Context,
	texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = m.vec
	}
	return out, nil
}

func (m *mockEmbedder) Dimension() int   { return len(m.vec) }
func (m *mockEmbedder) Model() string    { return "mock" }

type mockVectorStore struct {
	stored map[string][]float32
}

func newMockVectorStore() *mockVectorStore {
	return &mockVectorStore{stored: make(map[string][]float32)}
}

func (m *mockVectorStore) Upsert(_ context.Context,
	id string, vector []float32, _ map[string]any) error {
	m.stored[id] = vector
	return nil
}

func (m *mockVectorStore) Search(_ context.Context,
	_ []float32, _ int, _ storage.ObjectFilter,
) ([]storage.VectorHit, error) {
	return nil, nil
}

func (m *mockVectorStore) Delete(_ context.Context,
	id string) error {
	delete(m.stored, id)
	return nil
}

func TestEmbeddingGeneratorProducesRealEmbeddings(t *testing.T) {
	vec := []float32{0.1, 0.2, 0.3}
	embedder := &mockEmbedder{vec: vec}
	vs := newMockVectorStore()

	step := NewEmbeddingGenerator(embedder, vs)
	draft := &storage.KnowledgeObject{
		ID:         "obj-1",
		RawContent: "some text",
	}

	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !got.VectorIndexed {
		t.Error("VectorIndexed should be true")
	}
	if len(got.Embeddings) != 3 {
		t.Errorf("embeddings len: got %d, want 3", len(got.Embeddings))
	}
	if _, ok := vs.stored["obj-1"]; !ok {
		t.Error("vector not stored in VectorStore")
	}
}

func TestEmbeddingGeneratorRequiresContent(t *testing.T) {
	step := NewEmbeddingGenerator(&mockEmbedder{}, newMockVectorStore())
	draft := &storage.KnowledgeObject{ID: "obj-1", RawContent: ""}

	_, err := step.Run(context.Background(), draft)
	if err == nil {
		t.Error("expected error for empty content")
	}
}
```

**Step 2: Run to verify failure**

Run: `go test ./internal/pipeline/steps/... -run TestEmbeddingGenerator -v`
Expected: FAIL (signature changed).

**Step 3: Rewrite embedding.go**

```go
package steps

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/llm"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type EmbeddingGenerator struct {
	pipeline.BaseContract
	embedder llm.Embedder
	vectors  storage.VectorStore
}

func NewEmbeddingGenerator(
	embedder llm.Embedder,
	vectors storage.VectorStore,
) *EmbeddingGenerator {
	return &EmbeddingGenerator{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"RawContent"},
			Produces: []string{"Embeddings", "VectorIndexed"},
		}),
		embedder: embedder,
		vectors:  vectors,
	}
}

func (s *EmbeddingGenerator) Name() string {
	return "embedding_generator"
}

func (s *EmbeddingGenerator) Run(
	ctx context.Context,
	draft *storage.KnowledgeObject,
) (*storage.KnowledgeObject, error) {
	if draft.RawContent == "" {
		return nil, fmt.Errorf("embedding: empty content")
	}

	vecs, err := s.embedder.Embed(ctx, []string{draft.RawContent})
	if err != nil {
		return nil, fmt.Errorf("embedding: %w", err)
	}
	if len(vecs) == 0 || len(vecs[0]) == 0 {
		return nil, fmt.Errorf("embedding: provider returned empty vector")
	}

	draft.Embeddings = vecs[0]

	if err := s.vectors.Upsert(ctx, draft.ID, vecs[0], nil); err != nil {
		return nil, fmt.Errorf("embedding: vector store: %w", err)
	}

	draft.VectorIndexed = true
	return draft, nil
}
```

**Step 4: Update all callers of NewEmbeddingGenerator()**

Find with: `grep -r NewEmbeddingGenerator internal/ cmd/`

Each call site now needs embedder and vectors params. Pipeline
registration (builtins) must receive these deps.

**Step 5: Run tests**

Run: `go test ./internal/pipeline/steps/... -run TestEmbeddingGenerator -v`
Expected: PASS.

**Step 6: Commit**

```
git add internal/pipeline/steps/embedding.go \
        internal/pipeline/steps/embedding_test.go
git commit -m "feat(pipeline): rewrite embedding step with real provider

Calls Embedder.Embed() then VectorStore.Upsert(). No more stub."
```

---

### Task 8: Add Embedding Test Fixtures

**Files:**
- Create: `testdata/embeddings/` directory
- Create: `internal/testutil/embeddings.go`

**Step 1: Write the fixture loader**

Create `internal/testutil/embeddings.go`:

```go
package testutil

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/llm"
)

// LoadOrComputeEmbedding returns a cached embedding vector for text.
// On first call (fixture missing), computes via real provider and
// writes the fixture. Subsequent calls load from disk.
func LoadOrComputeEmbedding(
	t *testing.T,
	provider llm.Embedder,
	text string,
	fixtureName string,
) []float32 {
	t.Helper()

	_, thisFile, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(thisFile), "..", "..", "testdata",
		"embeddings")
	os.MkdirAll(root, 0755)

	path := filepath.Join(root, fixtureName+".json")

	// Try loading from fixture.
	type fixture struct {
		Text   string    `json:"text"`
		Vector []float32 `json:"vector"`
		Model  string    `json:"model"`
	}

	if data, err := os.ReadFile(path); err == nil {
		var f fixture
		if err := json.Unmarshal(data, &f); err == nil {
			return f.Vector
		}
	}

	// Compute with real provider.
	vecs, err := provider.Embed(context.Background(), []string{text})
	if err != nil {
		t.Fatalf("compute embedding: %v", err)
	}
	if len(vecs) == 0 {
		t.Fatal("provider returned no vectors")
	}

	// Write fixture.
	f := fixture{
		Text:   text,
		Vector: vecs[0],
		Model:  provider.Model(),
	}
	data, _ := json.MarshalIndent(f, "", "  ")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	t.Logf("wrote embedding fixture: %s", path)

	return vecs[0]
}
```

**Step 2: Commit**

```
git add internal/testutil/embeddings.go testdata/embeddings/
git commit -m "feat(testutil): add embedding fixture loader

First run computes real embeddings; subsequent runs load from disk."
```

---

### Task 9: Integration Test — Ingest → Embed → Search

**Files:**
- Create: `test/integration/us_vector_search_test.go`

**Step 1: Write integration test**

```go
package integration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/ideacrafterslabs/ctxt/internal/testutil"
)

func TestVectorSearchEndToEnd(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	ctx := context.Background()
	vs := driver.Vectors()

	// Use pre-computed fixture vectors (dimension must match test DB).
	// These are short 3-dim vectors for test speed.
	vecA := []float32{0.9, 0.1, 0.0}
	vecB := []float32{0.0, 0.1, 0.9}

	// Store two objects.
	objA := &storage.KnowledgeObject{
		ID:         "obj-a",
		Type:       "text",
		RawContent: "Go concurrency patterns",
	}
	objB := &storage.KnowledgeObject{
		ID:         "obj-b",
		Type:       "text",
		RawContent: "French cooking techniques",
	}
	require.NoError(t, driver.Objects().Create(ctx, objA))
	require.NoError(t, driver.Objects().Create(ctx, objB))

	// Store vectors.
	require.NoError(t, vs.Upsert(ctx, "obj-a", vecA, nil))
	require.NoError(t, vs.Upsert(ctx, "obj-b", vecB, nil))

	// Search near vecA — should return obj-a first.
	query := []float32{0.8, 0.1, 0.1}
	hits, err := vs.Search(ctx, query, 2, storage.ObjectFilter{})
	require.NoError(t, err)
	require.Len(t, hits, 2)
	require.Equal(t, "obj-a", hits[0].ID)

	// Delete obj-a vector, search again.
	require.NoError(t, vs.Delete(ctx, "obj-a"))
	hits, err = vs.Search(ctx, query, 10, storage.ObjectFilter{})
	require.NoError(t, err)
	require.Len(t, hits, 1)
	require.Equal(t, "obj-b", hits[0].ID)
}

func TestVectorDimensionMismatch(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	ctx := context.Background()
	vs := driver.Vectors()

	// Test DB uses dimension=3. Insert wrong dimension.
	wrongDim := []float32{0.1, 0.2, 0.3, 0.4, 0.5}
	err := vs.Upsert(ctx, "obj-bad", wrongDim, nil)
	require.Error(t, err)
}
```

**Step 2: Run integration tests**

Run: `go test ./test/integration/... -run TestVector -v`
Expected: PASS.

**Step 3: Commit**

```
git add test/integration/us_vector_search_test.go
git commit -m "test: add vector search integration tests

End-to-end: store objects, upsert vectors, KNN search, delete.
Verifies dimension mismatch returns error."
```

---

### Task 10: Update CI for CGo Builds

**Files:**
- Modify: `.github/workflows/*.yml` (or equivalent CI config)
- Modify: `Dockerfile` (if exists)

**Step 1: Identify CI files**

Run: `ls .github/workflows/ Makefile Dockerfile 2>/dev/null`

**Step 2: Add gcc to CI**

For GitHub Actions, ensure the build step has:

```yaml
- name: Install build deps
  run: sudo apt-get install -y gcc
```

For macOS runners, gcc/clang is already present.

For Windows runners:

```yaml
- name: Install MinGW
  uses: msys2/setup-msys2@v2
```

**Step 3: Set CGO_ENABLED=1**

Ensure all `go test` and `go build` steps have:

```yaml
env:
  CGO_ENABLED: 1
```

**Step 4: If Dockerfile exists, add gcc to build stage**

```dockerfile
RUN apk add --no-cache gcc musl-dev
```

or for Debian-based:

```dockerfile
RUN apt-get update && apt-get install -y gcc
```

**Step 5: Run CI locally or push to verify**

Run: `go test ./... -count=1`
Expected: All pass with CGO_ENABLED=1.

**Step 6: Commit**

```
git add .github/ Dockerfile Makefile
git commit -m "ci: enable CGo builds for mattn/go-sqlite3

Add gcc to build environments for sqlite-vec extension support."
```

---

## Dependency Graph

```
Task 1 (driver switch)
  └─ Task 2 (merge migrations)
       └─ Task 3 (VectorStore interface)
            ├─ Task 4 (VectorConfig)
            ├─ Task 5 (sqlite-vec impl)
            │    └─ Task 6 (wire into driver + service)
            │         └─ Task 7 (embedding step rewrite) [BLOCKED: LLM plan]
            │              └─ Task 8 (test fixtures)
            │                   └─ Task 9 (integration test)
            └─ Task 10 (CI) [independent, do anytime after Task 1]
```

---

## Success Criteria

- [ ] `go test ./...` passes with mattn/go-sqlite3
- [ ] `vec_version()` returns a version string from sqlite-vec
- [ ] VectorStore.Upsert stores a vector, Search retrieves it by KNN
- [ ] VectorStore.Delete removes a vector
- [ ] Dimension mismatch returns error, not panic
- [ ] Embedding step calls real provider, stores in VectorStore
- [ ] Config selects vector backend (`sqlite-vec` default)
- [ ] CI builds with CGO_ENABLED=1 on all targets
- [ ] No stubs remain in the embedding path
