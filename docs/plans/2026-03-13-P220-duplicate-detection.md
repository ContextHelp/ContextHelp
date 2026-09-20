# Duplicate Detection Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Detect duplicate or near-duplicate knowledge objects at ingest time and apply a configurable policy (drop / keep / warn).

**Architecture:** Content hash exact-match (already stored in `content_hash` column) covers identical duplicates. Near-duplicate detection uses cosine similarity of existing embeddings above a configurable threshold. Policy applied in `service.Analyze()` before enqueuing the pipeline job. No new migration required for exact-match; near-duplicate requires a DB query against vector index.

**Tech Stack:** Go, existing SQLite storage + vector index (already in `internal/storage/sqlite/`), config via `internal/config/config.go` (Plan 8 Phase 1 must be merged first).

---

## Pre-flight checks

Before starting, confirm:
- `internal/storage/sqlite/migrations/003_content_hash_reinforcement.sql` — unique index `idx_objects_content_hash` on `content_hash` exists (verified).
- `internal/storage/storage.go` — `ObjectStore` interface already has `GetByContentHash` and `VectorSearch` (verified; no new interface changes needed for Tasks 2 and 3 — these methods already exist under slightly different names).
- Module path: `github.com/ideacrafterslabs/ctxt`

**Key existing facts discovered during planning:**
- `ObjectStore.GetByContentHash(ctx, hash)` already exists in both the interface (`storage.go:44`) and the SQLite implementation (`objects.go:60–72`). Task 2 requires no new interface method — only testing coverage.
- `ObjectStore.VectorSearch(ctx, vector, filter)` already exists. For near-duplicate detection, a `FindSimilar`-style helper can be implemented as a thin wrapper around `VectorSearch` with a threshold filter, added to the service layer (not the storage interface).
- `CosineSimilarity` is already implemented in `internal/pipeline/steps/embedding.go`. Reuse it from service layer (or inline equivalently).
- The `Service` struct currently has no `cfg` field — it will need a `Cfg config.Config` field added in Task 6.

---

## Task 1 — Add `DuplicatesConfig` to config

**File:** `internal/config/config.go`

**Step 1.1 — Write the failing test first**

In `internal/config/config_test.go`, add `TestDuplicatesDefaults`:

```go
func TestDuplicatesDefaults(t *testing.T) {
    cfg, err := Load("")
    require.NoError(t, err)
    assert.Equal(t, "warn", cfg.Duplicates.Policy)
    assert.Equal(t, 0.95, cfg.Duplicates.SimilarityThreshold)
    assert.True(t, cfg.Duplicates.CheckExact)
    assert.False(t, cfg.Duplicates.CheckSimilar)
}
```

Run: `go test ./internal/config/... -run TestDuplicatesDefaults`
Expected: FAIL (field does not exist).

**Step 1.2 — Implement**

Add to `internal/config/config.go`, after the `RetrievalConfig` block:

```go
// DuplicatesConfig controls duplicate and near-duplicate detection behaviour at ingest time.
type DuplicatesConfig struct {
    // Policy determines what happens when a duplicate is found.
    // Valid values: "warn" (default), "drop", "keep".
    Policy string `mapstructure:"policy"`
    // SimilarityThreshold is the cosine similarity cutoff for near-duplicate detection.
    // Range: 0.0–1.0. Default: 0.95.
    SimilarityThreshold float64 `mapstructure:"similarity_threshold"`
    // CheckExact enables content-hash exact-match deduplication. Default: true.
    CheckExact bool `mapstructure:"check_exact"`
    // CheckSimilar enables vector-embedding near-duplicate detection. Default: false.
    // Requires embeddings to have been computed (pipeline embedding step must run first).
    CheckSimilar bool `mapstructure:"check_similar"`
}
```

Add to the `Config` struct:

```go
// Duplicates controls duplicate detection policy.
Duplicates DuplicatesConfig `mapstructure:"duplicates"`
```

Add to `setDefaults()` in `internal/config/config.go`:

```go
// Duplicates defaults
v.SetDefault("duplicates.policy", "warn")
v.SetDefault("duplicates.similarity_threshold", 0.95)
v.SetDefault("duplicates.check_exact", true)
v.SetDefault("duplicates.check_similar", false)
```

Run: `go test ./internal/config/... -run TestDuplicatesDefaults`
Expected: PASS.

Run full config suite: `go test ./internal/config/...`
Expected: all PASS.

**Commit:**
```
feat(config): add DuplicatesConfig with policy, threshold, and check flags
```

---

## Task 2 — Verify and add test coverage for `GetByContentHash`

The storage interface and SQLite implementation already have `GetByContentHash`. This task adds the missing test.

**File:** `internal/storage/sqlite/objects_test.go`

**Step 2.1 — Write the test**

Add `TestGetByContentHash`:

```go
func TestGetByContentHash(t *testing.T) {
    d := newTestDriver(t)
    ctx := context.Background()

    // Not found returns nil, nil.
    got, err := d.Objects().GetByContentHash(ctx, "sha256:nonexistent")
    require.NoError(t, err)
    assert.Nil(t, got)

    // Empty hash returns nil, nil (guard).
    got, err = d.Objects().GetByContentHash(ctx, "")
    require.NoError(t, err)
    assert.Nil(t, got)

    // Insert an object with a known hash.
    now := time.Now().Truncate(time.Second)
    obj := &storage.KnowledgeObject{
        ID:          "hash-test-1",
        Type:        "text",
        Subtype:     "short",
        RawContent:  "hello world",
        ContentHash: "sha256:abc123deadbeef",
        CreatedAt:   now,
        UpdatedAt:   now,
    }
    require.NoError(t, d.Objects().Create(ctx, obj))

    got, err = d.Objects().GetByContentHash(ctx, "sha256:abc123deadbeef")
    require.NoError(t, err)
    require.NotNil(t, got)
    assert.Equal(t, "hash-test-1", got.ID)
    assert.Equal(t, "sha256:abc123deadbeef", got.ContentHash)
}
```

Run: `go test ./internal/storage/sqlite/... -run TestGetByContentHash`
Expected: PASS (implementation already exists; this locks the behaviour).

**Commit:**
```
test(storage/sqlite): add GetByContentHash coverage
```

---

## Task 3 — Add `FindSimilar` to service layer (no new storage interface method)

`VectorSearch` already exists on the `ObjectStore`. Rather than adding a new storage interface method, implement `findSimilar` as a private service helper that wraps `VectorSearch` and filters by threshold. This keeps the storage interface stable.

**File:** `internal/service/duplicates.go` (new file)

**Step 3.1 — Write the test first**

Create `internal/service/duplicates_test.go`:

```go
package service

import (
    "context"
    "testing"

    "github.com/ideacrafterslabs/ctxt/internal/config"
    "github.com/ideacrafterslabs/ctxt/internal/storage"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

// stubObjects is a minimal ObjectStore stub for duplicate tests.
type stubObjects struct {
    storage.ObjectStore
    byHash  map[string]*storage.KnowledgeObject
    similar []*storage.KnowledgeObject // returned by VectorSearch
}

func (s *stubObjects) GetByContentHash(_ context.Context, hash string) (*storage.KnowledgeObject, error) {
    if hash == "" {
        return nil, nil
    }
    return s.byHash[hash], nil
}

func (s *stubObjects) VectorSearch(_ context.Context, _ []float32, filter storage.ObjectFilter) ([]*storage.KnowledgeObject, error) {
    out := make([]*storage.KnowledgeObject, 0, len(s.similar))
    for _, obj := range s.similar {
        if score, ok := obj.Metadata["score"].(float64); ok && score >= 0 {
            out = append(out, obj)
        }
    }
    return out, nil
}
```

Add table-driven test:

```go
func TestCheckDuplicates(t *testing.T) {
    existingObj := &storage.KnowledgeObject{ID: "existing-1", ContentHash: "sha256:abc"}
    similarObj  := &storage.KnowledgeObject{
        ID:       "similar-1",
        Metadata: map[string]any{"score": float64(0.97)},
    }

    tests := []struct {
        name      string
        hash      string
        embeddings []float32
        cfg       config.DuplicatesConfig
        byHash    map[string]*storage.KnowledgeObject
        similar   []*storage.KnowledgeObject
        wantKind  DuplicateKind
        wantNil   bool
    }{
        {
            name:    "exact match found",
            hash:    "sha256:abc",
            cfg:     config.DuplicatesConfig{CheckExact: true, SimilarityThreshold: 0.95},
            byHash:  map[string]*storage.KnowledgeObject{"sha256:abc": existingObj},
            wantKind: DuplicateExact,
        },
        {
            name:      "similar match found",
            embeddings: []float32{1, 0, 0},
            cfg:       config.DuplicatesConfig{CheckSimilar: true, SimilarityThreshold: 0.95},
            similar:   []*storage.KnowledgeObject{similarObj},
            wantKind:  DuplicateSimilar,
        },
        {
            name:    "no match",
            hash:    "sha256:xyz",
            cfg:     config.DuplicatesConfig{CheckExact: true, SimilarityThreshold: 0.95},
            byHash:  map[string]*storage.KnowledgeObject{},
            wantNil: true,
        },
        {
            name:    "exact check disabled",
            hash:    "sha256:abc",
            cfg:     config.DuplicatesConfig{CheckExact: false},
            byHash:  map[string]*storage.KnowledgeObject{"sha256:abc": existingObj},
            wantNil: true,
        },
        {
            name:      "similar check disabled",
            embeddings: []float32{1, 0, 0},
            cfg:        config.DuplicatesConfig{CheckSimilar: false, SimilarityThreshold: 0.95},
            similar:    []*storage.KnowledgeObject{similarObj},
            wantNil:    true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            svc := &Service{}
            stub := &stubObjectStore{
                objs: &stubObjects{byHash: tt.byHash, similar: tt.similar},
            }
            svc.Store = stub

            result, err := svc.checkDuplicates(context.Background(), tt.hash, tt.embeddings, tt.cfg)
            require.NoError(t, err)
            if tt.wantNil {
                assert.Nil(t, result)
            } else {
                require.NotNil(t, result)
                assert.Equal(t, tt.wantKind, result.Kind)
            }
        })
    }
}
```

Note: `stubObjectStore` wraps the full `StorageDriver` interface; only `Objects()` needs to return a real value. See Task 5 for the full stub wiring.

Run: `go test ./internal/service/... -run TestCheckDuplicates`
Expected: FAIL (types do not exist yet).

**Step 3.2 — Implement `DuplicateResult` type and `checkDuplicates` helper**

Create `internal/service/duplicates.go`:

```go
package service

import (
    "context"
    "math"

    "github.com/ideacrafterslabs/ctxt/internal/config"
    "github.com/ideacrafterslabs/ctxt/internal/storage"
)

// DuplicateKind classifies the type of duplicate match.
type DuplicateKind string

const (
    // DuplicateExact means the content_hash matched an existing object exactly.
    DuplicateExact DuplicateKind = "exact"
    // DuplicateSimilar means cosine similarity exceeded the configured threshold.
    DuplicateSimilar DuplicateKind = "similar"
)

// DuplicateResult describes a duplicate match.
type DuplicateResult struct {
    Kind       DuplicateKind
    Existing   *storage.KnowledgeObject
    Similarity float64 // 1.0 for exact matches
}

// checkDuplicates inspects the store for an exact or near-duplicate of the
// given content hash / embeddings, applying the caller's DuplicatesConfig.
// Returns nil, nil when no duplicate is found.
func (s *Service) checkDuplicates(ctx context.Context, hash string, embeddings []float32, cfg config.DuplicatesConfig) (*DuplicateResult, error) {
    // 1. Exact match via content_hash.
    if cfg.CheckExact && hash != "" {
        existing, err := s.Store.Objects().GetByContentHash(ctx, hash)
        if err != nil {
            return nil, err
        }
        if existing != nil {
            return &DuplicateResult{
                Kind:       DuplicateExact,
                Existing:   existing,
                Similarity: 1.0,
            }, nil
        }
    }

    // 2. Near-duplicate via vector similarity.
    if cfg.CheckSimilar && len(embeddings) > 0 {
        filter := storage.ObjectFilter{Limit: 1}
        candidates, err := s.Store.Objects().VectorSearch(ctx, embeddings, filter)
        if err != nil {
            return nil, err
        }
        for _, candidate := range candidates {
            score := 0.0
            if v, ok := candidate.Metadata["score"].(float64); ok {
                score = v
            } else {
                score = cosineSimilarity(embeddings, candidate.Embeddings)
            }
            if score >= cfg.SimilarityThreshold {
                return &DuplicateResult{
                    Kind:       DuplicateSimilar,
                    Existing:   candidate,
                    Similarity: score,
                }, nil
            }
        }
    }

    return nil, nil
}

// cosineSimilarity computes cosine similarity between two float32 vectors.
// Returns 0 for empty or length-mismatched vectors.
func cosineSimilarity(a, b []float32) float64 {
    if len(a) != len(b) || len(a) == 0 {
        return 0
    }
    var dot, normA, normB float64
    for i := range a {
        dot += float64(a[i]) * float64(b[i])
        normA += float64(a[i]) * float64(a[i])
        normB += float64(b[i]) * float64(b[i])
    }
    if normA == 0 || normB == 0 {
        return 0
    }
    return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
```

Run: `go test ./internal/service/... -run TestCheckDuplicates`
Expected: PASS.

Run full service suite: `go test ./internal/service/...`
Expected: all PASS.

**Commit:**
```
feat(service): add DuplicateResult type and checkDuplicates helper
```

---

## Task 4 — Wire `checkDuplicates` into `Analyze()`

**Files:** `internal/service/service.go`, `internal/config/config.go`

**Step 4.1 — Add `Cfg` field to `Service`**

The `Service` struct needs to hold config so `Analyze()` can read `Duplicates`. Add:

```go
type Service struct {
    Store     storage.StorageDriver
    Queue     *jobs.Queue
    Pipes     pipeline.Registry
    Search    *search.Engine
    Discovery *steps.StepDiscovery
    Executor  *steps.StepExecutor
    Bus       events.Bus
    Cfg       config.Config  // add this field
}
```

Update `New()` signature to accept config (or set it separately after construction — the simpler approach is to accept it in `New()`):

```go
func New(store storage.StorageDriver, queue *jobs.Queue, pipes pipeline.Registry, engine *search.Engine, stepsPath string, bus events.Bus, cfg config.Config) *Service {
    ...
    return &Service{
        ...
        Cfg: cfg,
    }
}
```

Update all callers of `New()`. Find them:

```bash
grep -r "service.New(" ../ctxt --include="*.go" -l
```

Pass a zero-value or loaded `config.Config{}` at each call site.

**Step 4.2 — Write the integration test first**

In `internal/service/service_test.go`, add `TestAnalyzeDuplicateDrop`:

```go
func TestAnalyzeDuplicateDrop(t *testing.T) {
    // Setup: build a real in-memory service.
    svc := newTestService(t)
    ctx := context.Background()

    // First ingestion.
    req := AnalyzeRequest{Content: "hello duplicate world", Type: "text"}
    jobID1, err := svc.Analyze(ctx, req)
    require.NoError(t, err)
    require.NotEmpty(t, jobID1)

    // Simulate pipeline completing: create an object with a known hash.
    hash := "sha256:test-hash-drop"
    obj := &storage.KnowledgeObject{
        ID:          "obj-drop-1",
        Type:        "text",
        ContentHash: hash,
        RawContent:  "hello duplicate world",
        CreatedAt:   time.Now(),
        UpdatedAt:   time.Now(),
    }
    require.NoError(t, svc.Store.Objects().Create(ctx, obj))

    // Override config to use drop policy and point hash at known object.
    svc.Cfg.Duplicates = config.DuplicatesConfig{
        Policy:     "drop",
        CheckExact: true,
        SimilarityThreshold: 0.95,
    }

    // Second ingestion with same hash — should return existing object ID, not a new job.
    req2 := AnalyzeRequest{Content: "hello duplicate world", Type: "text", KnownHash: hash}
    result, err := svc.Analyze(ctx, req2)
    require.NoError(t, err)
    assert.Equal(t, "obj-drop-1", result, "drop policy should return existing object ID")

    // Confirm only one object exists.
    objs, total, err := svc.Store.Objects().List(ctx, storage.ObjectFilter{})
    require.NoError(t, err)
    assert.Equal(t, 1, total, "only one object should exist after drop dedup")
    assert.Equal(t, "obj-drop-1", objs[0].ID)
}
```

Note: This test requires adding `KnownHash string` to `AnalyzeRequest` (see Step 4.3).

Run: `go test ./internal/service/... -run TestAnalyzeDuplicateDrop`
Expected: FAIL.

**Step 4.3 — Extend `AnalyzeRequest` and implement policy in `Analyze()`**

In `internal/service/types.go`, add `KnownHash` to `AnalyzeRequest`:

```go
type AnalyzeRequest struct {
    Content   string `json:"content"`
    Type      string `json:"type"`
    Pipeline  string `json:"pipeline,omitempty"`
    Source    string `json:"source,omitempty"`
    KnownHash string `json:"known_hash,omitempty"` // pre-computed content hash; skips re-hashing
}
```

In `internal/service/service.go`, update `Analyze()`. After the pipeline name selection and before creating the `Job`, add:

```go
// Duplicate detection (exact match only at analyze time; embeddings not yet computed).
hashForCheck := req.KnownHash
dup, err := s.checkDuplicates(ctx, hashForCheck, nil, s.Cfg.Duplicates)
if err != nil {
    return "", fmt.Errorf("analyze: duplicate check: %w", err)
}
if dup != nil {
    switch s.Cfg.Duplicates.Policy {
    case "drop":
        // Return the existing object's ID — no new job enqueued.
        return dup.Existing.ID, nil
    case "warn":
        fmt.Fprintf(os.Stderr, "warning: duplicate detected (%s): existing object %s\n",
            dup.Kind, dup.Existing.ID)
        // Fall through — continue ingestion.
    case "keep":
        // Fall through silently.
    }
}
```

Add `"os"` to imports in `service.go`.

Run: `go test ./internal/service/... -run TestAnalyzeDuplicateDrop`
Expected: PASS.

Run: `go test ./internal/service/...`
Expected: all PASS.

**Commit:**
```
feat(service): apply duplicate detection policy in Analyze()
```

---

## Task 5 — Post-enrichment similarity dedup pipeline step

This step fires after embedding generation, when embeddings are available, and applies the near-duplicate check for the `CheckSimilar` path.

**File:** `internal/pipeline/steps/dedup_step.go` (new)

**Step 5.1 — Write the test first**

Create `internal/pipeline/steps/dedup_step_test.go`:

```go
package steps

import (
    "context"
    "testing"

    "github.com/ideacrafterslabs/ctxt/internal/config"
    "github.com/ideacrafterslabs/ctxt/internal/storage"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

type stubObjectStoreForDedup struct {
    storage.ObjectStore
    similar []*storage.KnowledgeObject
}

func (s *stubObjectStoreForDedup) VectorSearch(_ context.Context, _ []float32, _ storage.ObjectFilter) ([]*storage.KnowledgeObject, error) {
    return s.similar, nil
}

func TestDedupStep_NoDuplicatePassesThrough(t *testing.T) {
    cfg := config.DuplicatesConfig{CheckSimilar: true, SimilarityThreshold: 0.95, Policy: "warn"}
    step := NewDedupStep(&stubObjectStoreForDedup{similar: nil}, cfg)

    draft := &storage.KnowledgeObject{
        ID:         "new-1",
        Embeddings: []float32{1, 0, 0},
        Metadata:   map[string]any{},
    }
    out, err := step.Run(context.Background(), draft)
    require.NoError(t, err)
    assert.Equal(t, "new-1", out.ID)
    assert.Nil(t, out.Metadata["duplicate_of"])
}

func TestDedupStep_SimilarFoundWarnPolicy(t *testing.T) {
    existing := &storage.KnowledgeObject{
        ID:         "existing-2",
        Embeddings: []float32{1, 0, 0},
        Metadata:   map[string]any{"score": float64(0.97)},
    }
    cfg := config.DuplicatesConfig{CheckSimilar: true, SimilarityThreshold: 0.95, Policy: "warn"}
    step := NewDedupStep(&stubObjectStoreForDedup{similar: []*storage.KnowledgeObject{existing}}, cfg)

    draft := &storage.KnowledgeObject{
        ID:         "new-2",
        Embeddings: []float32{1, 0, 0},
        Metadata:   map[string]any{},
    }
    out, err := step.Run(context.Background(), draft)
    require.NoError(t, err)
    // warn policy: continues, sets duplicate_of metadata for audit.
    assert.Equal(t, "existing-2", out.Metadata["duplicate_of"])
}

func TestDedupStep_SimilarFoundDropPolicy(t *testing.T) {
    existing := &storage.KnowledgeObject{
        ID:         "existing-3",
        Embeddings: []float32{1, 0, 0},
        Metadata:   map[string]any{"score": float64(0.99)},
    }
    cfg := config.DuplicatesConfig{CheckSimilar: true, SimilarityThreshold: 0.95, Policy: "drop"}
    step := NewDedupStep(&stubObjectStoreForDedup{similar: []*storage.KnowledgeObject{existing}}, cfg)

    draft := &storage.KnowledgeObject{
        ID:         "new-3",
        Embeddings: []float32{1, 0, 0},
        Metadata:   map[string]any{},
    }
    out, err := step.Run(context.Background(), draft)
    require.NoError(t, err)
    // drop policy: sets duplicate_of and signals pipeline to suppress via SuppressOutput flag.
    assert.Equal(t, "existing-3", out.Metadata["duplicate_of"])
    assert.Equal(t, true, out.Metadata["suppress_output"])
}

func TestDedupStep_CheckSimilarDisabled(t *testing.T) {
    existing := &storage.KnowledgeObject{ID: "existing-4", Metadata: map[string]any{"score": float64(0.99)}}
    cfg := config.DuplicatesConfig{CheckSimilar: false, SimilarityThreshold: 0.95, Policy: "drop"}
    step := NewDedupStep(&stubObjectStoreForDedup{similar: []*storage.KnowledgeObject{existing}}, cfg)

    draft := &storage.KnowledgeObject{
        ID:         "new-4",
        Embeddings: []float32{1, 0, 0},
        Metadata:   map[string]any{},
    }
    out, err := step.Run(context.Background(), draft)
    require.NoError(t, err)
    assert.Nil(t, out.Metadata["duplicate_of"])
}
```

Run: `go test ./internal/pipeline/steps/... -run TestDedupStep`
Expected: FAIL (DedupStep does not exist).

**Step 5.2 — Implement `DedupStep`**

Create `internal/pipeline/steps/dedup_step.go`:

```go
package steps

import (
    "context"
    "fmt"
    "math"

    "github.com/ideacrafterslabs/ctxt/internal/config"
    "github.com/ideacrafterslabs/ctxt/internal/pipeline"
    "github.com/ideacrafterslabs/ctxt/internal/storage"
)

// DedupStep is a pipeline step that checks for near-duplicate objects using
// vector embeddings after the embedding step has run. It applies the configured
// policy: warn (annotate + continue), drop (annotate + suppress), keep (pass through).
//
// Register in builtins.go as "dedup" step, inserted after "embedding" when
// cfg.Duplicates.CheckSimilar == true.
type DedupStep struct {
    pipeline.BaseContract
    store storage.ObjectStore
    cfg   config.DuplicatesConfig
}

// NewDedupStep creates a DedupStep. Pass nil store for passthrough (no-op) mode.
func NewDedupStep(store storage.ObjectStore, cfg config.DuplicatesConfig) *DedupStep {
    return &DedupStep{
        BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
            Requires: []string{"Embeddings"},
            Produces: []string{"Metadata"},
        }),
        store: store,
        cfg:   cfg,
    }
}

func (s *DedupStep) Name() string { return "dedup" }

func (s *DedupStep) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
    if draft.Metadata == nil {
        draft.Metadata = make(map[string]any)
    }

    if !s.cfg.CheckSimilar || len(draft.Embeddings) == 0 || s.store == nil {
        return draft, nil
    }

    candidates, err := s.store.VectorSearch(ctx, draft.Embeddings, storage.ObjectFilter{Limit: 1})
    if err != nil {
        // Non-fatal: log and continue.
        fmt.Printf("dedup: vector search error (non-fatal): %v\n", err)
        return draft, nil
    }

    for _, candidate := range candidates {
        if candidate.ID == draft.ID {
            continue // skip self
        }
        score := 0.0
        if v, ok := candidate.Metadata["score"].(float64); ok {
            score = v
        } else {
            score = dedupCosineSimilarity(draft.Embeddings, candidate.Embeddings)
        }
        if score < s.cfg.SimilarityThreshold {
            continue
        }

        // Found a near-duplicate. Apply policy.
        draft.Metadata["duplicate_of"] = candidate.ID
        draft.Metadata["duplicate_similarity"] = score
        draft.Metadata["duplicate_kind"] = string(DuplicateSimilarKind)

        switch s.cfg.Policy {
        case "drop":
            // Signal to the caller/pipeline executor that this object should not be persisted.
            draft.Metadata["suppress_output"] = true
        case "warn":
            fmt.Printf("warning: near-duplicate detected (similarity=%.4f): existing object %s\n",
                score, candidate.ID)
            // Continue ingestion.
        case "keep":
            // Continue silently.
        }
        break // only check first match
    }

    return draft, nil
}

// DuplicateSimilarKind is the string label stored in metadata.
const DuplicateSimilarKind = "similar"

// dedupCosineSimilarity is a local copy to avoid an import cycle with the service package.
func dedupCosineSimilarity(a, b []float32) float64 {
    if len(a) != len(b) || len(a) == 0 {
        return 0
    }
    var dot, normA, normB float64
    for i := range a {
        dot += float64(a[i]) * float64(b[i])
        normA += float64(a[i]) * float64(a[i])
        normB += float64(b[i]) * float64(b[i])
    }
    if normA == 0 || normB == 0 {
        return 0
    }
    return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
```

Run: `go test ./internal/pipeline/steps/... -run TestDedupStep`
Expected: PASS.

Run full steps suite: `go test ./internal/pipeline/steps/...`
Expected: all PASS.

**Commit:**
```
feat(pipeline/steps): add DedupStep for post-enrichment near-duplicate detection
```

---

## Task 6 — Register `DedupStep` in builtins

**File:** `internal/pipeline/builtins/builtins.go`

**Step 6.1 — Add constructor to `stepConstructors` map**

The `DedupStep` requires a store and config that are not available at zero-arg construction time. The builtins registry uses zero-arg constructors; for DedupStep, register it with a nil store (passthrough mode) so it always compiles. The actual store is injected at runtime via the executor when config is available.

Add to `stepConstructors`:

```go
"dedup": func() pipeline.PipelineStep { return steps.NewDedupStep(nil, config.DuplicatesConfig{}) },
```

Add `"github.com/ideacrafterslabs/ctxt/internal/config"` to builtins imports.

**Step 6.2 — Conditional insertion into pipeline definitions**

When building a pipeline that has `CheckSimilar: true`, the executor or builder should insert `"dedup"` after `"embedding"`. This is done in the pipeline executor's step instantiation loop. Add a helper in `internal/pipeline/builtins/capabilities.go` or in the executor:

```go
// InjectDedupStep inserts the dedup step after the embedding step in a pipeline
// definition when near-duplicate checking is enabled.
func InjectDedupStep(steps []string, cfg config.DuplicatesConfig) []string {
    if !cfg.CheckSimilar {
        return steps
    }
    out := make([]string, 0, len(steps)+1)
    for _, s := range steps {
        out = append(out, s)
        if s == "embedding" {
            out = append(out, "dedup")
        }
    }
    return out
}
```

**Step 6.3 — Write test for `InjectDedupStep`**

In `internal/pipeline/builtins/builtins_test.go` (or a new `dedup_injection_test.go`):

```go
func TestInjectDedupStep(t *testing.T) {
    steps := []string{"typedetector", "embedding", "tagger"}

    // CheckSimilar disabled: no injection.
    out := InjectDedupStep(steps, config.DuplicatesConfig{CheckSimilar: false})
    assert.Equal(t, steps, out)

    // CheckSimilar enabled: dedup inserted after embedding.
    out = InjectDedupStep(steps, config.DuplicatesConfig{CheckSimilar: true})
    assert.Equal(t, []string{"typedetector", "embedding", "dedup", "tagger"}, out)
}
```

Run: `go test ./internal/pipeline/builtins/... -run TestInjectDedupStep`
Expected: PASS.

Run: `go test ./internal/pipeline/builtins/...`
Expected: all PASS.

**Commit:**
```
feat(pipeline/builtins): register dedup step and add InjectDedupStep helper
```

---

## Task 7 — Config validation

**File:** `internal/config/validate.go` (create if it does not exist; otherwise add to existing validation logic)

**Step 7.1 — Check if validate.go exists**

```bash
ls ./internal/config/
```

If absent, create it. If present, add to the existing `Validate` function.

**Step 7.2 — Write the test first**

In `internal/config/config_test.go`, add `TestDuplicatesValidation`:

```go
func TestDuplicatesValidation(t *testing.T) {
    tests := []struct {
        name    string
        cfg     Config
        wantErr bool
    }{
        {
            name: "valid warn policy",
            cfg:  Config{Duplicates: DuplicatesConfig{Policy: "warn", SimilarityThreshold: 0.95}},
        },
        {
            name: "valid drop policy",
            cfg:  Config{Duplicates: DuplicatesConfig{Policy: "drop", SimilarityThreshold: 0.80}},
        },
        {
            name: "valid keep policy",
            cfg:  Config{Duplicates: DuplicatesConfig{Policy: "keep", SimilarityThreshold: 0.99}},
        },
        {
            name:    "invalid policy",
            cfg:     Config{Duplicates: DuplicatesConfig{Policy: "delete", SimilarityThreshold: 0.95}},
            wantErr: true,
        },
        {
            name:    "threshold too high",
            cfg:     Config{Duplicates: DuplicatesConfig{Policy: "warn", SimilarityThreshold: 1.01}},
            wantErr: true,
        },
        {
            name:    "threshold negative",
            cfg:     Config{Duplicates: DuplicatesConfig{Policy: "warn", SimilarityThreshold: -0.1}},
            wantErr: true,
        },
        {
            name: "empty policy defaults to warn (pass)",
            cfg:  Config{Duplicates: DuplicatesConfig{Policy: "", SimilarityThreshold: 0.95}},
            // empty policy treated as "warn" by validator
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := tt.cfg.Validate()
            if tt.wantErr {
                require.Error(t, err)
            } else {
                require.NoError(t, err)
            }
        })
    }
}
```

Run: `go test ./internal/config/... -run TestDuplicatesValidation`
Expected: FAIL (Validate method does not exist or doesn't cover duplicates).

**Step 7.3 — Implement validation**

Create (or update) `internal/config/validate.go`:

```go
package config

import "fmt"

// Validate checks that the Config is internally consistent.
// Called after Load() when strict validation is desired.
func (c *Config) Validate() error {
    if err := c.validateDuplicates(); err != nil {
        return err
    }
    return nil
}

func (c *Config) validateDuplicates() error {
    d := c.Duplicates
    policy := d.Policy
    if policy == "" {
        policy = "warn" // treat empty as default
    }
    switch policy {
    case "warn", "drop", "keep":
        // valid
    default:
        return fmt.Errorf("config: duplicates.policy must be one of \"warn\", \"drop\", \"keep\"; got %q", d.Policy)
    }
    if d.SimilarityThreshold < 0.0 || d.SimilarityThreshold > 1.0 {
        return fmt.Errorf("config: duplicates.similarity_threshold must be 0.0–1.0; got %f", d.SimilarityThreshold)
    }
    return nil
}
```

Run: `go test ./internal/config/... -run TestDuplicatesValidation`
Expected: PASS.

Run: `go test ./internal/config/...`
Expected: all PASS.

**Commit:**
```
feat(config): add Validate() with duplicates policy and threshold checks
```

---

## Task 8 — CLI surface: `duplicate_of` annotation in `ctxt list`

**Step 8.1 — Locate the `ctxt list` command output renderer**

```bash
grep -r "duplicate_of\|ctxt list\|ListObjects\|KnowledgeObject" \
    ./cmd --include="*.go" -l
```

The `ctxt list` output is typically in `cmd/list.go` or equivalent. Identify the file that calls `svc.ListObjects()` and formats the output.

**Step 8.2 — Add annotation**

In the row formatter for each object, append `(duplicate of <id>)` when `obj.Metadata["duplicate_of"]` is set:

```go
dupOf, _ := obj.Metadata["duplicate_of"].(string)
if dupOf != "" {
    fmt.Fprintf(w, " (duplicate of %s)", dupOf)
}
```

Update the `--help` text for `ctxt list` to document the annotation:

```
Flags:
  ...
  Objects with a known duplicate are annotated with "(duplicate of <id>)".
  The duplicate_of field is set by the dedup pipeline step or by the "warn"/"keep"
  policy when near-duplicate detection (duplicates.check_similar) is enabled.
```

**Step 8.3 — Manual verification**

Since this is a display-only change, test manually after Task 9 integration test:

```bash
ctxt list
# Should show: <id>  <type>  <source>  (duplicate of <existing-id>)
# for any object with Metadata["duplicate_of"] set.
```

**Commit:**
```
feat(cmd): annotate duplicate objects in ctxt list output
```

---

## Task 9 — Integration test + end-to-end verification

**Step 9.1 — Build the binary**

```bash
cd ../ctxt && go build ./cmd/ctxt/...
```

Expected: clean build, no errors.

**Step 9.2 — Integration test: exact-match drop**

```bash
# Set up a temp data directory.
export CTXT_DATA_DIR=$(mktemp -d)

# First ingest.
./ctxt analyze "The quick brown fox jumps over the lazy dog"
# Note the returned job ID (job1).

# Wait for pipeline completion (or use --sync if available).
sleep 2

# List objects — expect 1.
./ctxt list
# Expected: 1 object.

# Second ingest of identical content with policy=drop configured.
# (Set policy via config file or env: CTXT_DUPLICATES_POLICY=drop ./ctxt analyze ...)
CTXT_DUPLICATES_POLICY=drop ./ctxt analyze "The quick brown fox jumps over the lazy dog"
# Expected: returns the same object ID as the first ingest, no new job enqueued.

# List objects — still expect 1.
./ctxt list
# Expected: 1 object, no duplicates.
```

**Step 9.3 — Integration test: warn policy**

```bash
# Second ingest with policy=warn.
CTXT_DUPLICATES_POLICY=warn ./ctxt analyze "The quick brown fox jumps over the lazy dog"
# Expected: warning printed to stderr, new job enqueued, second object created.

# List objects — now 2.
./ctxt list
```

**Step 9.4 — Run the full test suite**

```bash
cd ../ctxt && go test ./...
```

Expected: all packages pass.

**Commit:**
```
test(integration): add duplicate detection end-to-end verification notes
```

---

## Task 10 — Final review checklist

Before marking this plan complete, verify:

- [ ] `go build ./...` passes with no errors
- [ ] `go test ./...` passes with no failures
- [ ] `go vet ./...` reports no issues
- [ ] `internal/config/config.go` — `DuplicatesConfig` struct present with four fields
- [ ] `internal/config/config.go` — `setDefaults` sets all four duplicate defaults
- [ ] `internal/config/validate.go` — `Validate()` rejects invalid policy and out-of-range threshold
- [ ] `internal/service/duplicates.go` — `DuplicateResult`, `DuplicateKind`, `checkDuplicates` present
- [ ] `internal/service/service.go` — `Analyze()` calls `checkDuplicates` before enqueueing
- [ ] `internal/service/service.go` — `Service.Cfg` field added; `New()` accepts config
- [ ] `internal/pipeline/steps/dedup_step.go` — `DedupStep` implements `pipeline.PipelineStep`
- [ ] `internal/pipeline/builtins/builtins.go` — `"dedup"` step registered in `stepConstructors`
- [ ] `internal/pipeline/builtins/` — `InjectDedupStep` helper available
- [ ] `internal/storage/sqlite/objects_test.go` — `TestGetByContentHash` added
- [ ] CLI `ctxt list` annotates `(duplicate of <id>)` for objects with `duplicate_of` metadata

---

## File map

| File | Action | Reason |
|------|--------|--------|
| `internal/config/config.go` | Edit | Add `DuplicatesConfig` struct and `Config.Duplicates` field; add `setDefaults` entries |
| `internal/config/config_test.go` | Edit | Add `TestDuplicatesDefaults`, `TestDuplicatesValidation` |
| `internal/config/validate.go` | Create | `Validate()` method with duplicates checks |
| `internal/service/duplicates.go` | Create | `DuplicateKind`, `DuplicateResult`, `checkDuplicates`, `cosineSimilarity` |
| `internal/service/duplicates_test.go` | Create | Table-driven unit tests for `checkDuplicates` |
| `internal/service/service.go` | Edit | Add `Cfg config.Config` to `Service`; update `New()`; wire `checkDuplicates` in `Analyze()` |
| `internal/service/types.go` | Edit | Add `KnownHash string` to `AnalyzeRequest` |
| `internal/storage/sqlite/objects_test.go` | Edit | Add `TestGetByContentHash` |
| `internal/pipeline/steps/dedup_step.go` | Create | `DedupStep` pipeline step for near-duplicate detection |
| `internal/pipeline/steps/dedup_step_test.go` | Create | Unit tests for `DedupStep` with stub store |
| `internal/pipeline/builtins/builtins.go` | Edit | Register `"dedup"` step constructor; add `InjectDedupStep` |
| `internal/pipeline/builtins/builtins_test.go` | Edit | Add `TestInjectDedupStep` |
| `cmd/list.go` (or equivalent) | Edit | Append `(duplicate of <id>)` annotation when `duplicate_of` metadata is set |

---

## No new migrations required

- `content_hash` column and unique index `idx_objects_content_hash` exist since migration `003_content_hash_reinforcement.sql`.
- `object_embeddings` table exists since migration `004_vector_search.sql`.
- `Metadata` is a JSON blob column in `objects` — new keys (`duplicate_of`, `duplicate_similarity`, `suppress_output`) require no schema change.
- Next planned migration is `008_watches.sql` (Plan 6). This plan does not touch migrations.
