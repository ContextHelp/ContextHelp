# Plan Review: Inline [ref:ID] Citations in Composition

**Plan:** `docs/plans/2026-02-18-inline-citations-plan.md`
**Reviewed:** 2026-02-18
**Verdict:** NOT READY FOR IMPLEMENTATION -- Requires prerequisite work and plan revision

---

## Executive Summary

The inline citations plan proposes a valuable feature: tracing generated composition content back to source knowledge objects via `[ref:ID]` syntax. The concept is sound and aligns well with ADR-017's provenance preservation goals. However, the plan builds on infrastructure that does not exist in the codebase. Every phase depends on at least one phantom dependency. The plan cannot be implemented as written without first completing 2-3 prerequisite efforts that are themselves multi-day projects.

---

## 1. Accuracy vs Actual Codebase

### CRITICAL: Phantom AI Provider Dependency

**Plan assumption (Phase 1, line 104):**
```go
content := s.aiProvider.Generate(ctx, prompt)
```

**Actual codebase (`internal/service/service.go:19-27`):**
```go
type Service struct {
    Store     storage.StorageDriver
    Queue     *jobs.Queue
    Pipes     pipeline.Registry
    Search    *search.Engine
    Discovery *steps.StepDiscovery
    Executor  *steps.StepExecutor
}
```

There is no `aiProvider` field. There is no `internal/llm/` package. The `docs/plans/llm-provider-support.md` plan exists but has NOT been implemented -- `internal/llm/` does not exist on disk. The existing `internal/providers/` package contains only media processing providers (video, OCR, transcription, vision, document, diarization) with no text generation capability.

**Impact:** Phase 1 is entirely unimplementable. This is not a minor gap; it is the core mechanism the entire plan depends on.

### CRITICAL: Compose() Signature Mismatch

**Plan proposes (`docs/plans/2026-02-18-inline-citations-plan.md:92`):**
```go
func (s *Service) Compose(ctx context.Context, objects []*KnowledgeObject,
    compositionType string) (*Composition, error)
```

**Actual implementation (`internal/service/service.go:364`):**
```go
func (s *Service) Compose(ctx context.Context, objects []*storage.KnowledgeObject,
    compositionType string) (string, error)
```

The actual method returns `(string, error)`. The plan changes it to return `(*Composition, error)`. This is a breaking change to the API contract. The caller in `cmd/ctxt/cmd/make.go:100` treats the result as a string:

```go
result, err := svc.Compose(ctx, objects, compositionType)
// ...
fmt.Print(result)
```

**Impact:** Changing the return type breaks the existing `make` command and any tests depending on it. The plan does not acknowledge or address this migration.

### IMPORTANT: No Composition Type in Storage

**Plan proposes (`docs/plans/2026-02-18-inline-citations-plan.md:307-315`):**
```go
type Composition struct {
    ID           string
    Type         string
    Content      string
    Citations    []CitationRef
    SourceIDs    []string
    GeneratedAt  time.Time
    Template     string
}
```

**Actual (`internal/storage/types.go`):** No `Composition` type exists. No `CompositionStore` interface exists. The `StorageDriver` interface at `internal/storage/storage.go:6-21` has no `Compositions()` method. Adding one would require:

1. New type definitions in `internal/storage/types.go`
2. New interface in `internal/storage/storage.go`
3. SQLite implementation in `internal/storage/sqlite/`
4. Migration SQL
5. Updates to all `StorageDriver` implementations and mocks

The plan acknowledges the SQL migration but does not address the Go interface changes needed.

### IMPORTANT: CLI Flag Mismatches

**Plan proposes (`docs/plans/2026-02-18-inline-citations-plan.md:264-273`):**
```bash
ctxt make brief --from o-abc123,o-def456
ctxt make brief --from o-abc123 --export json
ctxt make brief --from o-abc123 --no-citations
```

**Actual CLI (`cmd/ctxt/cmd/make.go:44-59`):**
```go
makeCmd.Flags().String("mention", "", "focus on specific mentions")
makeCmd.Flags().String("tag", "", "focus on specific tags (comma-separated)")
makeCmd.Flags().String("since", "", "include knowledge since date (ISO)")
makeCmd.Flags().StringP("output-file", "o", "", "write to file")
```

Missing flags: `--from`, `--export`, `--no-citations`. The plan does not mention that `--from` was already specified in ADR-017 and US-0022 but was never implemented. The current CLI filters objects by tag/mention/since and lists matching objects. The plan's `--from` implies direct object ID selection, which the current `ObjectFilter` at `internal/storage/types.go:89-101` does not support (no `IDs []string` field).

### IMPORTANT: API Endpoint on Non-Existent Route

**Plan proposes (`docs/plans/2026-02-18-inline-citations-plan.md:278`):**
```
POST /compositions/brief
```

**Actual router (`internal/server/http/server.go:24-70`):** The HTTP API exists but has no composition routes. Routes are under `/api/v1/`. The plan's proposed route is not namespaced under `/api/v1/`. The plan does not provide a handler implementation or mention integrating with the existing chi router.

### MINOR: obj.Summaries[0] Access Without Bounds Check

**Plan code (`docs/plans/2026-02-18-inline-citations-plan.md:223`):**
```go
summary := obj.Summaries[0]
```

The `KnowledgeObject.Summaries` field (`internal/storage/types.go:13`) is `[]string` and can be empty. The actual `Compose()` method at `internal/service/service.go:371` correctly checks `if len(obj.Summaries) > 0` before access. The plan's reference table builder would panic on objects with no summaries.

---

## 2. Architectural Gaps (Missing Prerequisites)

### Prerequisite 1: LLM Provider Integration

**Blocked by:** `docs/plans/llm-provider-support.md` (not implemented)

The entire citation injection mechanism depends on an AI provider generating text with inline citations. Without it, there is no content to parse citations from. The current `Compose()` is a simple concatenator that formats object summaries into markdown. It does not generate new prose, so there is nothing for citations to reference within.

**Estimated prerequisite effort:** 5-6 hours (per the LLM provider plan)

### Prerequisite 2: Composition Engine (ADR-017)

**Blocked by:** ADR-017 implementation

ADR-017 already defines a comprehensive composition engine with template schema, graph-aware assembly, profile adaptation, and provenance preservation. It includes its own storage schema (`compositions` + `composition_sources` tables). The inline citations plan partially overlaps with ADR-017's provenance system but does not reference or build on it.

**Specific conflict:** ADR-017 defines provenance as a structured JSON blob per composition tracking which objects contributed to which sections. The inline citations plan adds a parallel but different provenance mechanism (inline `[ref:ID]` markers + reference table). These should be unified, not duplicated.

### Prerequisite 3: Object ID Selection in CLI

**Blocked by:** `--from` flag implementation

Neither `ObjectFilter` nor the CLI supports selecting objects by ID. This is a prerequisite for the `ctxt make brief --from o-abc123` command the plan proposes.

---

## 3. Design Issues

### Over-Engineering: Database Storage for Citations

The plan proposes storing citations in a `citation_refs` table with foreign key relationships. For an initial implementation, citations are derived data: they exist within the composition content and can be re-parsed on demand. Storing them separately creates a synchronization burden (what if the content is edited but citations are not updated?).

**Recommendation:** Start without citation persistence. Parse citations on-the-fly when needed. Add storage only if a concrete use case demands it (e.g., "show me all compositions that cite object X").

### Under-Specified: Fallback Behavior

The plan does not address what happens when the AI provider is unavailable. Given the project's "local-first" and "must work offline" constraints (ADR-017), the plan needs a fallback path. The current `Compose()` method works without AI. The plan replaces it with an AI-dependent path and provides no degradation strategy.

**Recommendation:** Citations should be additive. When no AI provider is configured, `Compose()` should still work (as it does today) but without inline citations. The plan should define this explicitly.

### Under-Specified: Citation Injection Reliability

The plan relies on prompting an LLM to insert `[ref:ID]` markers. LLMs are unreliable at following format instructions consistently. The plan does not address:

- What if the LLM outputs `[ref: o-abc123]` (extra space)?
- What if the LLM invents an ID not in the input set?
- What if the LLM omits citations entirely?
- What if the LLM uses a different format (e.g., `(o-abc123)` or `[1]`)?

**Recommendation:** Add a post-processing validation step. Define acceptable citation formats. Implement a repair pass that normalizes near-matches. Log warnings for hallucinated IDs. Consider a structured output approach (e.g., constrained generation via LMQL as described in the project's design.md) rather than free-form prompt-and-hope.

### Duplicated Provenance System

ADR-017 already defines provenance tracking at `docs/decisions/ADR-017-composition-engine.md:164-187`:

```json
{
  "sources": [
    {
      "object_id": "uuid",
      "section": "Context",
      "excerpt_hash": "sha256",
      "contribution": "provided API integration details"
    }
  ]
}
```

The inline citations plan introduces a second, parallel provenance mechanism. These serve different purposes (structured metadata vs inline text markers) but should be designed as complementary parts of one system, not independent features.

**Recommendation:** Position inline citations as the user-facing rendering of ADR-017's provenance metadata. The composition engine tracks provenance structurally; citations are how that provenance surfaces in the output text.

---

## 4. Sequencing Problems

### Inverted Dependency Order

The plan's phases assume capabilities exist in the wrong order:

| Phase | Depends On | Status |
|-------|-----------|--------|
| Phase 1: Citation Injection | LLM provider, Composition Engine | Neither exists |
| Phase 2: Citation Parser | Phase 1 output to parse | Blocked by Phase 1 |
| Phase 3: Reference Table | Parsed citations + object lookup | Buildable independently |
| Phase 4: Entity Integration | Phase 2 citations + entity system | Entity system exists |
| Phase 5: CLI/API | All phases + `--from` flag + API route | Partially blocked |

**Correct sequencing should be:**

1. Implement LLM provider support (`docs/plans/llm-provider-support.md`)
2. Implement `--from` flag and `ObjectFilter.IDs` field
3. Implement basic Composition Engine (ADR-017 Phase 1)
4. THEN implement citations as an enhancement to the composition engine

### Timeline is Unrealistic

The plan estimates 6 days total. Given the prerequisites:

| Work Item | Realistic Estimate |
|-----------|-------------------|
| LLM provider integration | 5-6 hours |
| `--from` flag + ObjectFilter.IDs | 2-4 hours |
| Basic Composition Engine (ADR-017 Phase 1) | 3-5 days |
| Citation injection (this plan Phase 1) | 2 days |
| Citation parser (Phase 2) | 1 day |
| Reference table (Phase 3) | 0.5 days |
| Entity integration (Phase 4) | 0.5 days |
| CLI/API integration (Phase 5) | 1 day |
| **Total with prerequisites** | **~8-10 days** |

---

## 5. What the Plan Gets Right

- **The `[ref:ID]` syntax is clean and readable.** It follows established patterns (academic citations, Wikipedia references) and is easy to parse with regex.

- **The reference table format is practical.** A markdown table at the end of compositions is low-friction and works in any rendering context.

- **The citation parser design is solid.** `ParseCitations()` and `ValidateCitations()` at lines 148-192 are well-structured, testable, and could be implemented as a standalone package today.

- **Edge cases are identified.** The table at lines 348-355 covers the important scenarios (deleted objects, deduplication, non-existent references).

- **The testing plan is reasonable.** Unit/integration/E2E tests at lines 359-380 cover the right layers.

---

## 6. Concrete Recommendations

### Immediate Actions

1. **Revise the plan** to explicitly list prerequisites and their status. Add a "Prerequisites" section above Phase 1.

2. **Split the plan** into two documents:
   - **Citation syntax and parser** (Phases 2-3): Can be implemented now as a standalone `internal/citation/` package with no external dependencies. Pure text processing.
   - **Citation injection and integration** (Phases 1, 4-5): Depends on LLM provider and composition engine. Should reference those plans as blockers.

3. **Unify with ADR-017** provenance system. The plan should explicitly state it extends ADR-017's provenance tracking, not replaces it.

4. **Add fallback behavior.** Define what `Compose()` returns when AI is unavailable. The current concatenator behavior should remain as the no-AI fallback, without citations.

5. **Fix the bounds check** on `obj.Summaries[0]` in the reference table builder. Use the pattern from the existing `Compose()` method.

6. **Namespace the API route** under `/api/v1/compositions/brief` to match existing conventions in `internal/server/http/server.go`.

### Implementation Order (Revised)

```
Phase 0a: Implement citation parser package (internal/citation/)     [0.5 days, no blockers]
Phase 0b: Implement --from flag + ObjectFilter.IDs                   [0.5 days, no blockers]
Phase 0c: Implement LLM provider support                             [1 day, external plan]
Phase 0d: Implement basic Composition Engine (ADR-017 Phase 1)       [3 days, depends on 0c]
Phase 1:  Citation injection into composition prompts                [1 day, depends on 0c+0d]
Phase 2:  Reference table generation                                 [0.5 days, depends on 0a]
Phase 3:  Entity integration                                        [0.5 days, depends on Phase 1]
Phase 4:  CLI flags (--no-citations, --export) + API route           [1 day, depends on Phase 1]
```

---

## Files Referenced

| File | Role in Review |
|------|---------------|
| `./docs/plans/2026-02-18-inline-citations-plan.md` | Plan under review |
| `./internal/service/service.go` | Service struct (no AI provider), current Compose() |
| `./internal/storage/storage.go` | StorageDriver interface (no CompositionStore) |
| `./internal/storage/types.go` | KnowledgeObject (no Composition type) |
| `./cmd/ctxt/cmd/make.go` | CLI make command (no --from, --export, --no-citations) |
| `./internal/server/http/server.go` | HTTP router (no composition routes) |
| `./internal/providers/factory.go` | Provider factory (media only, no LLM) |
| `./docs/plans/llm-provider-support.md` | LLM plan (NOT implemented) |
| `./docs/decisions/ADR-017-composition-engine.md` | Composition engine ADR (NOT implemented) |
| `./docs/stories/composition/US-0022-generate-brief-from-objects.md` | User story (references aiProvider that does not exist) |
| `./docs/stories/admin/US-0027-configure-ai-provider.md` | AI provider story (NOT implemented) |
