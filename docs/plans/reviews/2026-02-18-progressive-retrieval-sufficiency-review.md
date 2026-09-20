# Plan Review: Progressive Retrieval Sufficiency Checking

**Plan:** `docs/plans/2026-02-18-progressive-retrieval-sufficiency.md`
**Reviewed:** 2026-02-18
**Verdict:** NOT READY FOR IMPLEMENTATION -- requires prerequisite plans to land first and significant rework of the tier model to align with the actual codebase.

---

## Executive Summary

The plan proposes a tiered progressive retrieval system (categories, items, resources) with LLM-based sufficiency checking at each tier, based on memU's approach. While the concept of progressive retrieval with early termination is sound, the plan has fundamental problems:

1. It depends on at least three major subsystems that do not exist in the codebase (LLM provider, embedding provider, vector search).
2. It forces memU's 3-tier hierarchy onto ctxt's flat object model without justification.
3. The task ordering means code written in Task 5 cannot compile until Task 6 is done.
4. Multiple runtime panics are baked into the proposed code.

The plan should be treated as a design sketch, not an implementation-ready specification.

---

## 1. Phantom Dependencies (Critical)

### 1.1 VectorSearch Does Not Exist

**Plan reference:** Task 5 (`retriever.go`), lines 585, 639, 691 call `w.store.Objects().VectorSearch(ctx, state.QueryVector, filter)`.

**Actual codebase:** The `ObjectStore` interface at `./internal/storage/storage.go:24-33` defines exactly eight methods:

```go
type ObjectStore interface {
    Create(ctx context.Context, obj *KnowledgeObject) error
    Get(ctx context.Context, id string) (*KnowledgeObject, error)
    GetByContentHash(ctx context.Context, hash string) (*KnowledgeObject, error)
    List(ctx context.Context, filter ObjectFilter) ([]*KnowledgeObject, int, error)
    Update(ctx context.Context, obj *KnowledgeObject) error
    Delete(ctx context.Context, id string) error
    ListBySQL(ctx context.Context, where string, args []any, limit, offset int) ([]*KnowledgeObject, int, error)
    Reinforce(ctx context.Context, hash string, mergeData *KnowledgeObject) (string, error)
}
```

No `VectorSearch` method. No vector index. No SQLite extension for vector similarity (e.g., sqlite-vss). Confirmed with a codebase-wide grep: zero matches for `VectorSearch`.

The plan acknowledges this in Task 6 ("Add VectorSearch method to ObjectRepository interface") but the retriever code (Task 5) is written as if it already exists. This is a sequencing error -- Task 5 cannot compile without Task 6.

More critically, Task 6 is described in a single sentence: "Add VectorSearch method to ObjectRepository interface." This grossly underestimates the work. A real `VectorSearch` requires:

- A vector storage backend (sqlite-vss, pgvector, Qdrant, or similar)
- Schema migration to store vectors in a searchable format
- An index build step (the `Embeddings []float32` field in `KnowledgeObject` exists but is never populated by any real code -- the `EmbeddingGenerator` step at `./internal/pipeline/steps/embedding.go` is a stub that only sets `VectorIndexed = true`)
- Implementation in the SQLite driver (the only current storage driver)

This is not a one-line interface addition. It is an entire feature track.

### 1.2 LLMProvider Does Not Exist

**Plan reference:** Task 2 creates `internal/providers/llm.go` with an `LLMProvider` interface.

**Actual codebase:** The `internal/providers/` package contains exclusively media-processing providers:

- `VideoProvider` (ffmpeg)
- `DocumentProvider` (golib, pdftotext)
- `OCRProvider` (tesseract)
- `TranscriptionProvider` (whisper)
- `VisionProvider` (ollama)
- `DiarizationProvider` (pyannote)

No `llm.go`. No `LLMProvider` interface. The `Factory` struct at `./internal/providers/factory.go` has no `LLM()` method. The `ProvidersConfig` at `./internal/config/config.go:93-100` has no LLM field.

A separate plan exists at `./docs/plans/llm-provider-support.md` that proposes adding LLM and embedding support via `charm.land/fantasy` and `go-embeddings`, with the code going into `internal/llm/` (not `internal/providers/`). That plan has not been implemented either (no `internal/llm/` directory exists).

**Conflict:** The progressive retrieval plan wants `LLMProvider` in `internal/providers/llm.go`. The LLM provider support plan wants it in `internal/llm/llm.go` using the Fantasy library. These plans disagree on package placement and API shape. The progressive retrieval plan invents its own `LLMProvider` interface (`Chat`, `ChatWithSystem`) while the LLM plan uses `Generate`, `GenerateWithSystem`, `Stream`.

### 1.3 EmbeddingProvider Does Not Exist

**Plan reference:** Task 4 (`workflow.go`) defines a local `EmbeddingProvider` interface with `Embed(ctx, texts) ([][]float32, error)`.

**Actual codebase:** No embedding capability exists anywhere in the codebase. The `EmbeddingGenerator` pipeline step (`./internal/pipeline/steps/embedding.go`) is a stub:

```go
func (s *EmbeddingGenerator) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
    // Stub: mark as ready for vector indexing. Real implementation
    // will call an embedding provider (OpenAI, local model, etc.).
    draft.VectorIndexed = true
    return draft, nil
}
```

The LLM provider support plan defines an `Embedder` interface in `internal/llm/embed.go`, but that code does not exist yet.

---

## 2. Forced Abstraction: memU's Tier Model (Important)

### 2.1 ctxt Has No "category" or "item" Object Types

**Plan reference:** Task 5 (`retriever.go`) sets `filter.Type = "category"` (line 582), `filter.Type = "item"` (line 636), and `filter.Type = "document"` (line 688).

**Actual codebase:** Object types set by the `formatdetector` and `typedetect` pipeline steps are:

- `"text"` (plain text, default)
- `"url"` (URL content)
- `"image"` (PNG, JPEG, GIF, TIFF, BMP, WebP)
- `"audio"` (MP3, WAV, OGG, FLAC, AAC, M4A)
- `"video"` (MP4, AVI, MOV, MKV)
- `"document"` (PDF, DOCX, XLSX, PPTX, ODT, RTF)
- `"note"` (used in tests)

There is no `"category"` type. There is no `"item"` type. memU has a hierarchical memory model (Categories > Items > Resources). ctxt has a flat knowledge object model where all objects are peers. The plan forces memU's hierarchy onto ctxt's data model by filtering on types that do not exist and would never be populated by any current pipeline.

### 2.2 Alternative Needed

If progressive retrieval is desired, it should work with ctxt's actual data:

- **Tier 1:** Summaries -- use the `Summaries []string` field on existing objects (already populated by pipeline steps)
- **Tier 2:** Sections -- use the `Sections []Section` field (already populated by the `sectioner` step)
- **Tier 3:** Full content -- use `RawContent`

This maps progressive retrieval to the actual structure of ctxt objects without inventing phantom types.

---

## 3. Code Safety Bugs (Critical)

### 3.1 Nil Slice Index: Summaries[0]

**Location:** `workflow.go`, line 485 (plan Task 4):

```go
parts = append(parts, fmt.Sprintf("- %s (score: %.3f)",
    hit.Data.Summaries[0], hit.Score))
```

`Summaries` is `[]string` and can be empty. This will panic with `index out of range [0]`. Must check `len(hit.Data.Summaries) > 0` first.

### 3.2 Unsafe Type Assertion: Metadata["score"]

**Location:** `retriever.go`, lines 593, 647, 699 (plan Task 5):

```go
Score: obj.Metadata["score"].(float64),
```

`Metadata` is `map[string]any`. If the key `"score"` does not exist, this is an assertion on `nil` which panics. Even if the key exists, the value might not be `float64` (JSON unmarshals numbers as `float64` but other code paths might store `int` or `string`). Must use the two-value form:

```go
score, _ := obj.Metadata["score"].(float64)
```

This panic would occur on every single retrieved object because `VectorSearch` does not exist to populate a `"score"` metadata field.

### 3.3 Duplicate `min()` Shadows Builtin

**Location:** `workflow.go`, lines 533-538 (plan Task 4):

```go
func min(a, b int) int {
    if a < b {
        return a
    }
    return b
}
```

The project uses Go 1.25.6 (`./go.mod` line 3: `go 1.25.6`). Go has had a builtin `min()` since Go 1.21. This function shadows the builtin and should be removed entirely.

### 3.4 Missing `strings` Import

**Location:** `workflow.go`, line 503 calls `strings.Join(parts, "\n")` but the import block (lines 336-342) only imports `context`, `fmt`, `providers`, and `storage`. The `"strings"` import is missing. This will not compile.

---

## 4. Task Sequencing Problems (Important)

### 4.1 Task 5 Depends on Task 6

Task 5 (Tier Retrievers) calls `w.store.Objects().VectorSearch(...)` which does not exist until Task 6 (Storage Interface Extension) adds it. If implemented in order, Task 5 cannot compile. The correct order is Task 6 before Task 5.

### 4.2 Tasks 3-5 Depend on Task 2

Tasks 3 (Sufficiency Checker), 4 (Workflow Orchestrator), and 5 (Tier Retrievers) all depend on the `LLMProvider` interface from Task 2. Task 2 itself depends on the unimplemented LLM provider support plan, which means **none of Tasks 2-5 can be implemented until the LLM provider support plan lands**.

### 4.3 Missing Prerequisite Chain

The actual dependency chain is:

```
LLM Provider Support Plan (unimplemented)
    -> This plan, Task 2 (LLM Provider Interface)
    -> This plan, Tasks 3, 4 (Sufficiency Checker, Workflow)

Embedding Support (from LLM Provider Support Plan, unimplemented)
    -> EmbeddingGenerator Step (currently a stub)
    -> Vector Storage Backend (does not exist)
    -> This plan, Task 6 (Storage Interface Extension)
    -> This plan, Task 5 (Tier Retrievers)
```

The plan has no explicit prerequisite section declaring these dependencies.

---

## 5. Architectural Misalignments (Important)

### 5.1 Package Placement Conflict

The plan creates `internal/providers/llm.go`. The existing LLM provider support plan puts LLM code in `internal/llm/`. The `internal/providers/` package is for media-processing providers (video, OCR, transcription, etc.), not LLM inference. Putting LLM interfaces here violates the existing package responsibility boundary.

### 5.2 API Integration Uses Wrong Package

**Plan reference:** Task 8 creates `internal/server/retrieve.go` in package `server`.

**Actual codebase:** The HTTP server is at `./internal/server/http/server.go` in package `http`. All handler files follow the pattern `handlers_*.go` in that directory. The plan's `internal/server/retrieve.go` would be in the wrong package and would not integrate with the chi router in `NewRouter()`.

The correct file would be `./internal/server/http/handlers_retrieve.go` in package `http`, following the existing pattern:

```
handlers_objects.go
handlers_analyze.go
handlers_jobs.go
handlers_search.go
handlers_entities.go
handlers_pipelines.go
handlers_steps.go
handlers_registries.go
handlers_system.go
```

### 5.3 Handler Does Not Use Service Layer

All existing handlers take `svc *service.Service` and delegate business logic to the service layer. The plan's `RetrieveHandler` struct holds a direct `*retrieval.Workflow` reference, bypassing the service layer. This breaks the established pattern where handlers are thin HTTP adapters and logic lives in the service.

### 5.4 `StorageDriver` vs `ObjectStore`

The `Workflow` struct holds `store storage.StorageDriver` and calls `w.store.Objects().VectorSearch(...)`. It only ever uses `Objects()`, so it should depend on `storage.ObjectStore` directly, not the entire `StorageDriver`. Taking the narrower interface follows the interface segregation principle and makes testing simpler.

---

## 6. Design Quality Issues (Suggestions)

### 6.1 Recreating SufficiencyChecker on Every Call

In `workflow.go`, both `checkRouteIntention` and `checkSufficiency` create a new `SufficiencyChecker` instance each time:

```go
func (w *Workflow) checkRouteIntention(...) {
    checker := NewSufficiencyChecker(w.llm) // new instance every call
    return checker.Check(...)
}
```

The `SufficiencyChecker` is stateless and only wraps an LLM reference. It should be created once in `NewWorkflow` and stored as a field, or the `Check` function should be a standalone function rather than a method.

### 6.2 Prompt Template Uses String Replacement

The `buildSufficiencyPrompt` function uses nested `strings.ReplaceAll` calls for template expansion. This is fragile -- if `{query}` appears inside the conversation history or retrieved content, it would be replaced. Use `text/template` or at minimum a more unique delimiter.

### 6.3 Response Returns Only IDs

The `RetrieveResponse` in Task 8 returns `[]string` for categories, items, and resources -- just the IDs. The client would need to make N additional requests to get the actual content. The response should include summaries or relevant content snippets to be useful.

### 6.4 No Error Context in Sufficiency Check Failure

When `Check()` returns an error, it falls back to `return true, query, err`. This means on LLM failure, the system always retrieves. While this is a reasonable degradation strategy, the error is also propagated up. In `workflow.go`, errors from `checkSufficiency` are returned to the caller, terminating the entire retrieval. The error handling is inconsistent -- either swallow the error and proceed with retrieval, or return it and stop.

### 6.5 No Metrics or Observability

The plan mentions "Performance Expectations" but provides no instrumentation. Latency per tier, sufficiency check hit rate, and tier progression ratios are essential for validating the claimed 30-60% savings. At minimum, the workflow should emit structured log entries or support context-based tracing.

---

## 7. What the Plan Gets Right

To be fair, several aspects of the plan are well-conceived:

- **The core concept is sound.** Progressive retrieval with early termination is a valid optimization for query answering. The sufficiency checking pattern (route intention, then tier-by-tier with possible early exit) is well-structured.
- **The state machine design is clean.** The `State` struct and the linear progression through tiers with gating checks is easy to reason about and test.
- **The sufficiency prompt design is reasonable.** The XML-tagged response format (`<decision>`, `<rewritten_query>`) with fallback parsing is a practical approach to structured LLM output.
- **The testing strategy outline is correct.** Unit tests for decision extraction, workflow tests for tier progression, and integration tests with real storage are the right layers.
- **Query rewriting between tiers is a good idea.** Refining the query based on what was already retrieved (from categories to a more specific item query) is a legitimate improvement over single-shot retrieval.

---

## 8. Recommendations

### 8.1 Sequence This Plan After Its Prerequisites

This plan cannot be implemented until:

1. **LLM provider support lands** (`docs/plans/llm-provider-support.md`) -- provides the `LLMProvider` / `Client` and `Embedder` interfaces.
2. **Embedding pipeline step is implemented** (currently a stub at `internal/pipeline/steps/embedding.go`).
3. **Vector storage is added** -- either via sqlite-vss extension or a separate vector store.

Add an explicit "Prerequisites" section to the plan listing these dependencies.

### 8.2 Replace memU's Tier Model With ctxt's Object Structure

Replace the `category` / `item` / `document` type filtering with a progressive deepening within existing objects:

| Tier | Content Source | When Used |
|------|---------------|-----------|
| Summaries | `KnowledgeObject.Summaries` | First pass: "what do you know about X?" |
| Sections | `KnowledgeObject.Sections` | When summaries are too vague |
| Full Content | `KnowledgeObject.RawContent` | When full source verification needed |

This works with the existing data model without phantom types.

### 8.3 Fix Task Ordering

Correct the implementation order to:

1. Task 1 (Core Types)
2. Task 6 (Storage Interface Extension) -- must come before Task 5
3. Task 2 (LLM Provider Interface) -- or depend on the separate LLM plan
4. Task 7 (Configuration)
5. Task 3 (Sufficiency Checker)
6. Task 4 (Workflow Orchestrator)
7. Task 5 (Tier Retrievers)
8. Task 8 (API Integration)

### 8.4 Fix All Safety Bugs Before Implementation

- Add `len()` checks before slice indexing on `Summaries`
- Use two-value type assertions for `Metadata["score"]`
- Remove the `min()` function (builtin since Go 1.21)
- Add the missing `"strings"` import
- Fix the handler package to `internal/server/http/handlers_retrieve.go`

### 8.5 Align With Existing Architecture Patterns

- Use `internal/llm/` for LLM interfaces, not `internal/providers/llm.go`
- Route the handler through the `service.Service` layer, not a direct `*retrieval.Workflow`
- Follow the `handlers_*.go` naming convention
- Depend on `storage.ObjectStore` instead of `storage.StorageDriver`

### 8.6 Add an Explicit Degradation Mode

For use before vector search is available, the plan should include a fallback mode that uses the existing RSQL search engine (`internal/search/engine.go`) for retrieval, with sufficiency checking applied to those results. This allows the sufficiency checking logic to be validated independently of the vector search dependency.

---

## Issue Summary

| # | Severity | Issue | Location in Plan |
|---|----------|-------|-----------------|
| 1 | CRITICAL | `VectorSearch` method does not exist on `ObjectStore` | Task 5, Task 6 |
| 2 | CRITICAL | `LLMProvider` interface does not exist anywhere | Task 2 |
| 3 | CRITICAL | `EmbeddingProvider` does not exist anywhere | Task 4 |
| 4 | CRITICAL | `Summaries[0]` panics on empty slice | Task 4, line 485 |
| 5 | CRITICAL | `Metadata["score"].(float64)` panics on missing key | Task 5, lines 593/647/699 |
| 6 | IMPORTANT | "category" and "item" object types do not exist in ctxt | Task 5, lines 582/636 |
| 7 | IMPORTANT | Task 5 cannot compile without Task 6 (wrong ordering) | Task 5/6 |
| 8 | IMPORTANT | Handler in wrong package (`server` vs `http`) | Task 8 |
| 9 | IMPORTANT | Handler bypasses service layer | Task 8 |
| 10 | IMPORTANT | No prerequisite section; depends on unimplemented LLM plan | Entire plan |
| 11 | IMPORTANT | LLM interface conflicts with existing LLM provider plan | Task 2 |
| 12 | SUGGESTION | Duplicate `min()` shadows Go 1.25 builtin | Task 4, line 533 |
| 13 | SUGGESTION | Missing `"strings"` import (will not compile) | Task 4, line 503 |
| 14 | SUGGESTION | String replacement for prompt templates is fragile | Task 3 |
| 15 | SUGGESTION | SufficiencyChecker recreated on every call | Task 4 |
| 16 | SUGGESTION | Response returns only IDs, not content | Task 8 |
| 17 | SUGGESTION | No observability / metrics instrumentation | Entire plan |
| 18 | SUGGESTION | `StorageDriver` dependency is too broad; use `ObjectStore` | Task 4 |
