# Hybrid Search Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace the naive in-memory `FindByText` with a fully configurable hybrid search engine (FTS5 + vector + RRF reranking) with global defaults, per-call overrides, and per-profile strategy overrides.

**Architecture:** Four layers — storage (FTS5 query), service (parallel FTS+vector, RRF merge), config (global `search:` block + per-profile `search_strategy:` override), CLI (flag-level overrides). Resolution order at runtime: CLI flags → active profile strategy → global config defaults. `FindByText` is replaced by `FTSSearch` backed by the real `objects_fts` FTS5 table.

**Tech Stack:** Go, SQLite FTS5 (`objects_fts` virtual table already exists), existing `VectorSearch` + `SemanticSearch`, viper config, cobra CLI flags.

---

## Existing code — read before starting

| What | Where |
|---|---|
| FTS5 table definition | `internal/storage/sqlite/migrations/001_initial.sql:27`, updated in `007_text_content.sql` |
| `VectorSearch` (SQLite) | `internal/storage/sqlite/objects.go:577` |
| `SemanticSearch` (service) | `internal/service/service.go:913` |
| `FindByText` (naive, to be replaced) | `internal/service/service.go:337` — scans all objects in memory |
| `runFind` (CLI) | `cmd/ctxt/cmd/find.go:50` — `--semantic` flag for vector, default calls `FindByText` |
| `FocusProfile` (profile config) | `internal/config/config.go:174` — already has `RerankBoosts map[string]float64` |
| `ObjectFilter` | `internal/storage/storage.go` — used by `VectorSearch` |
| `ObjectStore` interface | `internal/storage/storage.go:93` — add `FTSSearch` here |
| Postgres stub | `internal/storage/postgres/objects.go` — needs stub for new interface method |

---

## Task 1: Add `SearchConfig` to global config

**Files:**
- Modify: `internal/config/config.go`

**Step 1: Write the failing test**

In `internal/config/config_test.go`, add:

```go
func TestSearchConfigDefaults(t *testing.T) {
    cfg, err := loadFromYAML(`version: 1`)
    require.NoError(t, err)
    assert.Equal(t, "hybrid", cfg.Search.DefaultMode)
    assert.Equal(t, 60, cfg.Search.RRF.K)
    assert.InDelta(t, 0.5, cfg.Search.RRF.FTSWeight, 0.001)
    assert.InDelta(t, 0.5, cfg.Search.RRF.VectorWeight, 0.001)
    assert.Equal(t, 50, cfg.Search.CandidatePool.FTS)
    assert.Equal(t, 50, cfg.Search.CandidatePool.Vector)
    assert.InDelta(t, 0.0, cfg.Search.MinScore, 0.001)
    assert.True(t, cfg.Search.FallbackToFTS)
}
```

**Step 2: Run test to verify it fails**

```bash
GOWORK=off go test ./internal/config/... -run TestSearchConfigDefaults -v
```
Expected: FAIL — `cfg.Search` field doesn't exist yet.

**Step 3: Add `SearchConfig` to `internal/config/config.go`**

Add after `DuplicatesConfig`:

```go
// SearchConfig controls hybrid search behaviour.
type SearchConfig struct {
    // DefaultMode selects the search strategy used when no flag is passed.
    // Valid values: "fts" | "vector" | "hybrid". Default: "hybrid".
    DefaultMode string `mapstructure:"default_mode" yaml:"default_mode"`

    // RRF controls Reciprocal Rank Fusion parameters.
    RRF RRFConfig `mapstructure:"rrf" yaml:"rrf"`

    // CandidatePool controls how many results each leg fetches before merge.
    CandidatePool CandidatePoolConfig `mapstructure:"candidate_pool" yaml:"candidate_pool"`

    // MinScore discards merged results below this RRF score. Default: 0.0 (off).
    MinScore float64 `mapstructure:"min_score" yaml:"min_score"`

    // FallbackToFTS controls behaviour when embedding provider is unavailable.
    // If true (default), hybrid degrades to FTS-only. If false, returns error.
    FallbackToFTS bool `mapstructure:"fallback_to_fts" yaml:"fallback_to_fts"`
}

// RRFConfig controls Reciprocal Rank Fusion parameters.
type RRFConfig struct {
    // K is the rank constant (default 60). Higher values reduce the impact of top ranks.
    K int `mapstructure:"k" yaml:"k"`
    // FTSWeight is the weight applied to FTS leg scores (default 0.5).
    FTSWeight float64 `mapstructure:"fts_weight" yaml:"fts_weight"`
    // VectorWeight is the weight applied to vector leg scores (default 0.5).
    VectorWeight float64 `mapstructure:"vector_weight" yaml:"vector_weight"`
}

// CandidatePoolConfig controls how many candidates each leg returns before merge.
type CandidatePoolConfig struct {
    // FTS is the max candidates from the FTS leg. Default: 50.
    FTS int `mapstructure:"fts" yaml:"fts"`
    // Vector is the max candidates from the vector leg. Default: 50.
    Vector int `mapstructure:"vector" yaml:"vector"`
}
```

Add `Search SearchConfig` field to `Config` struct:
```go
// Search controls hybrid query execution behaviour.
Search SearchConfig `mapstructure:"search"`
```

Add defaults in `setDefaults`:
```go
v.SetDefault("search.default_mode", "hybrid")
v.SetDefault("search.rrf.k", 60)
v.SetDefault("search.rrf.fts_weight", 0.5)
v.SetDefault("search.rrf.vector_weight", 0.5)
v.SetDefault("search.candidate_pool.fts", 50)
v.SetDefault("search.candidate_pool.vector", 50)
v.SetDefault("search.min_score", 0.0)
v.SetDefault("search.fallback_to_fts", true)
```

**Step 4: Run test to verify it passes**

```bash
GOWORK=off go test ./internal/config/... -run TestSearchConfigDefaults -v
```
Expected: PASS

**Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add SearchConfig with RRF, candidate pool, and mode defaults"
```

---

## Task 2: Add `SearchStrategy` to `FocusProfile`

**Files:**
- Modify: `internal/config/config.go`

**Step 1: Write the failing test**

In `internal/config/config_test.go`, add:

```go
func TestProfileSearchStrategyOverride(t *testing.T) {
    cfg, err := loadFromYAML(`
version: 1
profile:
  profiles:
    research:
      description: Research mode
      search_strategy:
        mode: vector
        rrf:
          fts_weight: 0.2
          vector_weight: 0.8
`)
    require.NoError(t, err)
    p := cfg.Profile.Profiles["research"]
    assert.Equal(t, "vector", p.SearchStrategy.Mode)
    assert.InDelta(t, 0.2, p.SearchStrategy.RRF.FTSWeight, 0.001)
    assert.InDelta(t, 0.8, p.SearchStrategy.RRF.VectorWeight, 0.001)
}
```

**Step 2: Run to verify it fails**

```bash
GOWORK=off go test ./internal/config/... -run TestProfileSearchStrategyOverride -v
```
Expected: FAIL — `SearchStrategy` field doesn't exist on `FocusProfile`.

**Step 3: Add `ProfileSearchStrategy` and wire into `FocusProfile`**

```go
// ProfileSearchStrategy overrides global search settings for a specific profile.
// Zero values mean "inherit from global config".
type ProfileSearchStrategy struct {
    // Mode overrides search.default_mode for this profile.
    // Valid values: "" (inherit) | "fts" | "vector" | "hybrid".
    Mode string `mapstructure:"mode" yaml:"mode"`
    // RRF overrides RRF parameters. Zero values inherit from global.
    RRF RRFConfig `mapstructure:"rrf" yaml:"rrf"`
    // CandidatePool overrides pool sizes. Zero values inherit from global.
    CandidatePool CandidatePoolConfig `mapstructure:"candidate_pool" yaml:"candidate_pool"`
    // MinScore overrides the minimum score threshold. Negative means inherit.
    MinScore float64 `mapstructure:"min_score" yaml:"min_score"`
}
```

Add to `FocusProfile`:
```go
// SearchStrategy overrides global search config for this profile.
// Zero/empty fields inherit from the global search config.
SearchStrategy ProfileSearchStrategy `mapstructure:"search_strategy" yaml:"search_strategy"`
```

**Step 4: Run to verify it passes**

```bash
GOWORK=off go test ./internal/config/... -run TestProfileSearchStrategyOverride -v
```
Expected: PASS

**Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add per-profile SearchStrategy override"
```

---

## Task 3: Add `ResolvedSearchConfig` helper

**Files:**
- Modify: `internal/config/config.go`

This is the resolution function that merges global config + profile override into a final `SearchConfig` that the service consumes.

**Step 1: Write the failing test**

```go
func TestResolveSearchConfig(t *testing.T) {
    global := SearchConfig{
        DefaultMode:   "hybrid",
        RRF:           RRFConfig{K: 60, FTSWeight: 0.5, VectorWeight: 0.5},
        CandidatePool: CandidatePoolConfig{FTS: 50, Vector: 50},
        MinScore:      0.0,
        FallbackToFTS: true,
    }
    profile := ProfileSearchStrategy{
        Mode: "vector",
        RRF:  RRFConfig{FTSWeight: 0.2, VectorWeight: 0.8},
    }
    resolved := ResolveSearchConfig(global, profile)
    assert.Equal(t, "vector", resolved.DefaultMode)
    assert.Equal(t, 60, resolved.RRF.K)          // inherited
    assert.InDelta(t, 0.2, resolved.RRF.FTSWeight, 0.001)   // overridden
    assert.InDelta(t, 0.8, resolved.RRF.VectorWeight, 0.001) // overridden
    assert.Equal(t, 50, resolved.CandidatePool.FTS) // inherited
}
```

**Step 2: Run to verify it fails**

```bash
GOWORK=off go test ./internal/config/... -run TestResolveSearchConfig -v
```

**Step 3: Implement `ResolveSearchConfig`**

```go
// ResolveSearchConfig merges global search config with a profile-level override.
// Profile fields with zero values inherit from global.
func ResolveSearchConfig(global SearchConfig, profile ProfileSearchStrategy) SearchConfig {
    out := global
    if profile.Mode != "" {
        out.DefaultMode = profile.Mode
    }
    if profile.RRF.K != 0 {
        out.RRF.K = profile.RRF.K
    }
    if profile.RRF.FTSWeight != 0 {
        out.RRF.FTSWeight = profile.RRF.FTSWeight
    }
    if profile.RRF.VectorWeight != 0 {
        out.RRF.VectorWeight = profile.RRF.VectorWeight
    }
    if profile.CandidatePool.FTS != 0 {
        out.CandidatePool.FTS = profile.CandidatePool.FTS
    }
    if profile.CandidatePool.Vector != 0 {
        out.CandidatePool.Vector = profile.CandidatePool.Vector
    }
    if profile.MinScore < 0 {
        // negative sentinel = inherit (do nothing)
    } else if profile.MinScore > 0 {
        out.MinScore = profile.MinScore
    }
    return out
}
```

**Step 4: Run to verify it passes**

```bash
GOWORK=off go test ./internal/config/... -run TestResolveSearchConfig -v
```

**Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add ResolveSearchConfig for profile+global merge"
```

---

## Task 4: Add `FTSSearch` to storage interface + SQLite implementation

**Files:**
- Modify: `internal/storage/storage.go`
- Modify: `internal/storage/sqlite/objects.go`
- Modify: `internal/storage/postgres/objects.go` (stub)

**Step 1: Write the failing test**

In `internal/storage/sqlite/objects_test.go`, add:

```go
func TestFTSSearch(t *testing.T) {
    store := newTestStore(t)
    ctx := context.Background()

    // Insert two objects with known content.
    obj1 := testObject("fts-1")
    obj1.Summaries = []string{"authentication best practices guide"}
    obj1.RawContent = "Use bcrypt for password hashing"
    require.NoError(t, store.Objects().Create(ctx, obj1))

    obj2 := testObject("fts-2")
    obj2.Summaries = []string{"database indexing strategies"}
    obj2.RawContent = "Composite indexes improve query performance"
    require.NoError(t, store.Objects().Create(ctx, obj2))

    // FTS index is updated on insert — search for term in obj1 only.
    results, err := store.Objects().FTSSearch(ctx, "authentication", storage.ObjectFilter{Limit: 10})
    require.NoError(t, err)
    require.Len(t, results, 1)
    assert.Equal(t, "fts-1", results[0].ID)

    // Search for term in neither.
    results, err = store.Objects().FTSSearch(ctx, "kubernetes", storage.ObjectFilter{Limit: 10})
    require.NoError(t, err)
    assert.Empty(t, results)
}
```

**Step 2: Run to verify it fails**

```bash
GOWORK=off go test ./internal/storage/sqlite/... -run TestFTSSearch -v
```
Expected: FAIL — `FTSSearch` not defined on interface.

**Step 3: Add `FTSSearch` to `ObjectStore` interface in `internal/storage/storage.go`**

```go
// FTSSearch queries the objects_fts FTS5 virtual table using SQLite FTS5 MATCH syntax.
// Returns results ranked by FTS5 bm25 score, filtered by ObjectFilter.
FTSSearch(ctx context.Context, query string, filter ObjectFilter) ([]*KnowledgeObject, error)
```

**Step 4: Implement in `internal/storage/sqlite/objects.go`**

```go
// FTSSearch queries the objects_fts FTS5 virtual table and returns matching objects
// ranked by bm25 relevance score descending.
func (s *ObjectStore) FTSSearch(ctx context.Context, query string, filter storage.ObjectFilter) ([]*storage.KnowledgeObject, error) {
    if query == "" {
        return nil, fmt.Errorf("fts search: empty query")
    }

    limit := filter.Limit
    if limit <= 0 {
        limit = 50
    }

    q := `
        SELECT o.id, o.type, o.subtype, o.pipeline, o.raw_content, o.text_content,
               o.summaries, o.tags, o.decisions, o.tasks, o.mentions, o.entities,
               o.embeddings, o.source_uri, o.source_name, o.lang, o.extra_meta,
               o.created_at, o.updated_at, o.fts_indexed, o.vector_indexed,
               o.status, o.inbox_note, o.last_reinforced_at,
               bm25(objects_fts) AS score
        FROM objects_fts
        JOIN objects o ON objects_fts.id = o.id
        WHERE objects_fts MATCH ?
    `
    args := []any{query}

    if filter.Type != "" {
        q += " AND o.type = ?"
        args = append(args, filter.Type)
    }

    q += " ORDER BY score LIMIT ?"
    args = append(args, limit)

    rows, err := s.db.QueryContext(ctx, q, args...)
    if err != nil {
        return nil, fmt.Errorf("fts search: %w", err)
    }
    defer rows.Close()

    var results []*storage.KnowledgeObject
    for rows.Next() {
        obj, err := s.scanObjectWithScore(rows)
        if err != nil {
            return nil, err
        }
        results = append(results, obj)
    }
    return results, rows.Err()
}
```

Note: `bm25()` returns negative values in SQLite FTS5 — ORDER BY score (ascending) gives best matches first. Use `ORDER BY score` (not DESC). Confirm by checking SQLite docs or running a quick test.

**Step 5: Add stub to `internal/storage/postgres/objects.go`**

```go
func (s *ObjectStore) FTSSearch(ctx context.Context, query string, filter storage.ObjectFilter) ([]*storage.KnowledgeObject, error) {
    return nil, fmt.Errorf("FTSSearch: not implemented for postgres backend")
}
```

**Step 6: Run to verify it passes**

```bash
GOWORK=off go test ./internal/storage/sqlite/... -run TestFTSSearch -v
```
Expected: PASS

**Step 7: Commit**

```bash
git add internal/storage/storage.go internal/storage/sqlite/objects.go internal/storage/postgres/objects.go
git commit -m "feat(storage): add FTSSearch using objects_fts FTS5 table"
```

---

## Task 5: Add `HybridSearch` to service

**Files:**
- Modify: `internal/service/service.go`

**Step 1: Write the failing test**

In `internal/service/` create `service_search_test.go`:

```go
func TestHybridSearch_FTSOnly_WhenNoEmbeddingProvider(t *testing.T) {
    svc := newTestService(t)
    ctx := context.Background()

    obj := testKnowledgeObject("hs-1")
    obj.Summaries = []string{"distributed systems fault tolerance"}
    require.NoError(t, svc.Store.Objects().Create(ctx, obj))

    cfg := config.SearchConfig{
        DefaultMode:   "hybrid",
        RRF:           config.RRFConfig{K: 60, FTSWeight: 0.5, VectorWeight: 0.5},
        CandidatePool: config.CandidatePoolConfig{FTS: 20, Vector: 20},
        FallbackToFTS: true,
    }

    results, err := svc.HybridSearch(ctx, "distributed", 10, nil, cfg)
    require.NoError(t, err)
    require.Len(t, results, 1)
    assert.Equal(t, "hs-1", results[0].ID)
}

func TestHybridSearch_ErrorWhenNoProvider_FallbackDisabled(t *testing.T) {
    svc := newTestService(t)
    ctx := context.Background()
    cfg := config.SearchConfig{
        DefaultMode:   "hybrid",
        FallbackToFTS: false,
    }
    _, err := svc.HybridSearch(ctx, "anything", 10, nil, cfg)
    require.Error(t, err)
}
```

**Step 2: Run to verify it fails**

```bash
GOWORK=off go test ./internal/service/... -run TestHybridSearch -v
```

**Step 3: Implement `HybridSearch` in `internal/service/service.go`**

```go
// HybridSearch runs FTS and vector search concurrently, merges results with
// Reciprocal Rank Fusion (RRF), and returns the top-limit objects.
// If ep is nil and cfg.FallbackToFTS is true, degrades to FTS-only.
// If ep is nil and cfg.FallbackToFTS is false, returns an error.
func (s *Service) HybridSearch(ctx context.Context, query string, limit int, ep providers.EmbeddingProvider, cfg config.SearchConfig) ([]*storage.KnowledgeObject, error) {
    const rrfK = 60 // overridden by cfg.RRF.K below

    k := cfg.RRF.K
    if k <= 0 {
        k = rrfK
    }

    ftsFilter := storage.ObjectFilter{Limit: cfg.CandidatePool.FTS}
    if ftsFilter.Limit <= 0 {
        ftsFilter.Limit = 50
    }

    type legResult struct {
        results []*storage.KnowledgeObject
        err     error
    }

    // FTS leg — always runs.
    ftsCh := make(chan legResult, 1)
    go func() {
        res, err := s.Store.Objects().FTSSearch(ctx, query, ftsFilter)
        ftsCh <- legResult{res, err}
    }()

    // Vector leg — only if provider available.
    vecCh := make(chan legResult, 1)
    if ep != nil {
        vecPool := cfg.CandidatePool.Vector
        if vecPool <= 0 {
            vecPool = 50
        }
        go func() {
            vec, err := ep.Embed(ctx, query)
            if err != nil {
                vecCh <- legResult{nil, err}
                return
            }
            res, err := s.Store.Objects().VectorSearch(ctx, vec, storage.ObjectFilter{Limit: vecPool})
            vecCh <- legResult{res, err}
        }()
    } else {
        if !cfg.FallbackToFTS {
            <-ftsCh // drain
            return nil, fmt.Errorf("hybrid search: no embedding provider and fallback_to_fts is false")
        }
        vecCh <- legResult{nil, nil} // empty vector leg
    }

    ftsRes := <-ftsCh
    vecRes := <-vecCh

    if ftsRes.err != nil {
        return nil, fmt.Errorf("hybrid search fts leg: %w", ftsRes.err)
    }
    if vecRes.err != nil {
        return nil, fmt.Errorf("hybrid search vector leg: %w", vecRes.err)
    }

    // RRF merge.
    scores := map[string]float64{}
    byID := map[string]*storage.KnowledgeObject{}

    addLeg := func(results []*storage.KnowledgeObject, weight float64) {
        for rank, obj := range results {
            scores[obj.ID] += weight * (1.0 / float64(k+rank+1))
            byID[obj.ID] = obj
        }
    }

    addLeg(ftsRes.results, cfg.RRF.FTSWeight)
    addLeg(vecRes.results, cfg.RRF.VectorWeight)

    // Collect, filter by min score, sort descending.
    type scored struct {
        id    string
        score float64
    }
    var merged []scored
    for id, score := range scores {
        if score >= cfg.MinScore {
            merged = append(merged, scored{id, score})
        }
    }
    sort.Slice(merged, func(i, j int) bool { return merged[i].score > merged[j].score })

    if limit > len(merged) {
        limit = len(merged)
    }
    out := make([]*storage.KnowledgeObject, limit)
    for i := 0; i < limit; i++ {
        obj := byID[merged[i].id]
        if obj.Metadata == nil {
            obj.Metadata = make(map[string]any)
        }
        obj.Metadata["rrf_score"] = merged[i].score
        out[i] = obj
    }
    return out, nil
}
```

Add `"sort"` to imports.

**Step 4: Run to verify it passes**

```bash
GOWORK=off go test ./internal/service/... -run TestHybridSearch -v
```

**Step 5: Commit**

```bash
git add internal/service/service.go internal/service/service_search_test.go
git commit -m "feat(service): add HybridSearch with RRF merge and FTS fallback"
```

---

## Task 6: Replace `FindByText` with `FTSSearch`

**Files:**
- Modify: `internal/service/service.go`

`FindByText` is a naive in-memory string scan over all objects. Now that `FTSSearch` exists, replace it.

**Step 1: Write the failing test verifying FTS backing**

In `internal/service/service_search_test.go`, add:

```go
func TestFindByText_UsesFTS(t *testing.T) {
    svc := newTestService(t)
    ctx := context.Background()

    obj := testKnowledgeObject("fbt-1")
    obj.Summaries = []string{"microservices resilience patterns"}
    require.NoError(t, svc.Store.Objects().Create(ctx, obj))

    // Should find via FTS, not naive scan.
    results, err := svc.FindByText(ctx, "resilience", 10)
    require.NoError(t, err)
    require.Len(t, results, 1)
    assert.Equal(t, "fbt-1", results[0].ID)
}
```

**Step 2: Run to verify it passes already (or fails — either is fine)**

```bash
GOWORK=off go test ./internal/service/... -run TestFindByText_UsesFTS -v
```

**Step 3: Replace `FindByText` implementation**

```go
// FindByText searches knowledge objects using FTS5 full-text search.
func (s *Service) FindByText(ctx context.Context, query string, limit int) ([]*storage.KnowledgeObject, error) {
    return s.Store.Objects().FTSSearch(ctx, query, storage.ObjectFilter{Limit: limit})
}
```

**Step 4: Run full service tests**

```bash
GOWORK=off go test ./internal/service/... -v
```
All must pass.

**Step 5: Commit**

```bash
git add internal/service/service.go
git commit -m "refactor(service): replace naive FindByText with FTS5-backed FTSSearch"
```

---

## Task 7: Wire hybrid search into `find.go` with per-call flag overrides

**Files:**
- Modify: `cmd/ctxt/cmd/find.go`

**Step 1: Write the failing test**

In `cmd/ctxt/cmd/find_test.go` (or create it), add:

```go
func TestFindCmd_HybridFlag(t *testing.T) {
    // Verify --hybrid flag is registered and sets viper key.
    cmd := findCmd
    err := cmd.Flags().Set("hybrid", "true")
    require.NoError(t, err)
    assert.True(t, viper.GetBool("find.hybrid"))
}

func TestFindCmd_FlagOverrides(t *testing.T) {
    cmd := findCmd
    require.NoError(t, cmd.Flags().Set("rrf-k", "30"))
    require.NoError(t, cmd.Flags().Set("fts-weight", "0.3"))
    require.NoError(t, cmd.Flags().Set("vector-weight", "0.7"))
    assert.Equal(t, 30, viper.GetInt("find.rrf_k"))
    assert.InDelta(t, 0.3, viper.GetFloat64("find.fts_weight"), 0.001)
    assert.InDelta(t, 0.7, viper.GetFloat64("find.vector_weight"), 0.001)
}
```

**Step 2: Run to verify it fails**

```bash
GOWORK=off go test ./cmd/ctxt/... -run TestFindCmd_HybridFlag -v
```

**Step 3: Update `find.go`**

Replace the flags block in `init()`:

```go
findCmd.Flags().Int("limit", 10, "maximum results")
findCmd.Flags().Bool("semantic", false, "vector-only search")
findCmd.Flags().Bool("hybrid", false, "hybrid FTS+vector search with RRF (default mode from config)")
findCmd.Flags().Bool("fts", false, "FTS-only search")

// Per-call RRF overrides (zero = use config/profile value)
findCmd.Flags().Int("rrf-k", 0, "RRF k constant override (default: from config)")
findCmd.Flags().Float64("fts-weight", 0, "RRF FTS leg weight override (default: from config)")
findCmd.Flags().Float64("vector-weight", 0, "RRF vector leg weight override (default: from config)")
findCmd.Flags().Int("fts-pool", 0, "FTS candidate pool size override")
findCmd.Flags().Int("vector-pool", 0, "vector candidate pool size override")
findCmd.Flags().Float64("min-score", -1, "minimum RRF score threshold override (-1 = use config)")

viper.BindPFlag("find.limit", findCmd.Flags().Lookup("limit"))
viper.BindPFlag("find.semantic", findCmd.Flags().Lookup("semantic"))
viper.BindPFlag("find.hybrid", findCmd.Flags().Lookup("hybrid"))
viper.BindPFlag("find.fts", findCmd.Flags().Lookup("fts"))
viper.BindPFlag("find.rrf_k", findCmd.Flags().Lookup("rrf-k"))
viper.BindPFlag("find.fts_weight", findCmd.Flags().Lookup("fts-weight"))
viper.BindPFlag("find.vector_weight", findCmd.Flags().Lookup("vector-weight"))
viper.BindPFlag("find.fts_pool", findCmd.Flags().Lookup("fts-pool"))
viper.BindPFlag("find.vector_pool", findCmd.Flags().Lookup("vector-pool"))
viper.BindPFlag("find.min_score", findCmd.Flags().Lookup("min-score"))
```

Replace `runFind` to resolve config → profile → CLI flags:

```go
func runFind(cmd *cobra.Command, args []string) error {
    query, source, err := cli.GetInput(args)
    if err != nil {
        return err
    }
    if source == "clipboard" {
        fmt.Fprintf(os.Stderr, "Searching clipboard content: %q\n", query)
    }

    limit := viper.GetInt("find.limit")

    svc, cleanup, err := newService()
    if err != nil {
        return err
    }
    defer cleanup()

    ctx := context.Background()

    // Resolve search config: global → active profile → CLI flags.
    searchCfg := cfg.Search
    activeProfile := viper.GetString("profile.default")
    if p, ok := cfg.Profile.Profiles[activeProfile]; ok {
        searchCfg = config.ResolveSearchConfig(searchCfg, p.SearchStrategy)
    }
    // CLI flag overrides.
    if k := viper.GetInt("find.rrf_k"); k > 0 {
        searchCfg.RRF.K = k
    }
    if w := viper.GetFloat64("find.fts_weight"); w > 0 {
        searchCfg.RRF.FTSWeight = w
    }
    if w := viper.GetFloat64("find.vector_weight"); w > 0 {
        searchCfg.RRF.VectorWeight = w
    }
    if p := viper.GetInt("find.fts_pool"); p > 0 {
        searchCfg.CandidatePool.FTS = p
    }
    if p := viper.GetInt("find.vector_pool"); p > 0 {
        searchCfg.CandidatePool.Vector = p
    }
    if s := viper.GetFloat64("find.min_score"); s >= 0 {
        searchCfg.MinScore = s
    }

    // Determine mode: explicit flags override config.
    mode := searchCfg.DefaultMode
    if viper.GetBool("find.fts") {
        mode = "fts"
    }
    if viper.GetBool("find.semantic") {
        mode = "vector"
    }
    if viper.GetBool("find.hybrid") {
        mode = "hybrid"
    }

    var results []*storage.KnowledgeObject

    switch mode {
    case "vector":
        factory := providers.NewFactory(cfg.Providers, nil)
        ep := factory.Embedding()
        results, err = svc.SemanticSearch(ctx, query, limit, ep)
    case "fts":
        results, err = svc.FindByText(ctx, query, limit)
    default: // "hybrid"
        var ep providers.EmbeddingProvider
        factory := providers.NewFactory(cfg.Providers, nil)
        ep = factory.Embedding()
        results, err = svc.HybridSearch(ctx, query, limit, ep, searchCfg)
    }

    if err != nil {
        return fmt.Errorf("find (%s): %w", mode, err)
    }

    if isJSONOutput() {
        return outputJSON(os.Stdout, map[string]any{
            "objects": results,
            "total":   len(results),
            "query":   query,
            "mode":    mode,
        })
    }

    fmt.Printf("Search [%s]: %q (%d results)\n\n", mode, query, len(results))
    headers := []string{"ID", "Type", "Created"}
    var rows [][]string
    for _, obj := range results {
        rows = append(rows, []string{
            obj.ID,
            obj.Type,
            obj.CreatedAt.Format("2006-01-02 15:04"),
        })
    }
    printTable(os.Stdout, headers, rows)
    return nil
}
```

**Step 4: Run tests**

```bash
GOWORK=off go test ./cmd/ctxt/... -run TestFindCmd -v
GOWORK=off go build ./cmd/ctxt/...
```
Both must pass.

**Step 5: Commit**

```bash
git add cmd/ctxt/cmd/find.go cmd/ctxt/cmd/find_test.go
git commit -m "feat(cli): wire hybrid search into find command with per-call flag overrides"
```

---

## Task 8: Full integration test

**Files:**
- Create: `test/integration/hybrid_search_test.go`

**Step 1: Write the integration test**

```go
//go:build integration

package integration

func TestHybridSearch_EndToEnd(t *testing.T) {
    svc, cleanup := newTestService(t)
    defer cleanup()
    ctx := context.Background()

    // Insert objects with distinct content.
    for _, tc := range []struct{ id, summary, raw string }{
        {"hs-auth",  "oauth2 authentication flow",           "implement token refresh"},
        {"hs-cache", "redis caching strategies",             "eviction policy LRU"},
        {"hs-db",    "postgres query optimisation",          "explain analyse index scan"},
    } {
        obj := testKnowledgeObject(tc.id)
        obj.Summaries = []string{tc.summary}
        obj.RawContent = tc.raw
        require.NoError(t, svc.Store.Objects().Create(ctx, obj))
    }

    cfg := config.SearchConfig{
        DefaultMode:   "hybrid",
        RRF:           config.RRFConfig{K: 60, FTSWeight: 0.5, VectorWeight: 0.5},
        CandidatePool: config.CandidatePoolConfig{FTS: 20, Vector: 20},
        FallbackToFTS: true,
    }

    // FTS fallback (no embedding provider).
    results, err := svc.HybridSearch(ctx, "authentication", 5, nil, cfg)
    require.NoError(t, err)
    require.Len(t, results, 1)
    assert.Equal(t, "hs-auth", results[0].ID)
    assert.Contains(t, results[0].Metadata, "rrf_score")

    // FTS-only mode.
    results, err = svc.FindByText(ctx, "caching", 5)
    require.NoError(t, err)
    require.Len(t, results, 1)
    assert.Equal(t, "hs-cache", results[0].ID)

    // MinScore filter — set high to exclude all.
    cfgHighScore := cfg
    cfgHighScore.MinScore = 999.0
    results, err = svc.HybridSearch(ctx, "postgres", 5, nil, cfgHighScore)
    require.NoError(t, err)
    assert.Empty(t, results)
}
```

**Step 2: Run**

```bash
GOWORK=off go test -tags integration ./test/integration/... -run TestHybridSearch -v
```
Expected: PASS

**Step 3: Commit**

```bash
git add test/integration/hybrid_search_test.go
git commit -m "test(integration): add hybrid search end-to-end test"
```

---

## Task 9: Update T-0015 in TLC

```bash
tlc task update T-0015 --status IN_PROGRESS
tlc task complete T-0015 --note "Hybrid search implemented: FTSSearch (FTS5), HybridSearch (RRF), per-call flag overrides, global SearchConfig, per-profile SearchStrategy override."
```
