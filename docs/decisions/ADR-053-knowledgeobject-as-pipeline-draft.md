# ADR-053 – Use KnowledgeObject Directly as Pipeline Draft Type

> **Status:** Accepted
> **Date:** 2026-02-18
> **Author:** @jadb
> **Applies to:** dPKMS, ctxt
> **Supersedes:** None
> **Superseded by:** N/A

---

## Context

ADR-004 defines the pipeline step interface as:

```go
type PipelineStep interface {
    Name() string
    Run(ctx context.Context, in *BookmarkDraft) (*BookmarkDraft, error)
}
```

The `*BookmarkDraft` type is referenced but never defined. Meanwhile, the storage layer defines a complete `KnowledgeObject` schema (successor to "bookmarks") with all fields: `id`, `type`, `subtype`, `raw_content`, `metadata`, `summaries`, `sections`, `tags`, `mentions`, `decisions`, `tasks`, `embeddings`, `pipeline`, `source`, timestamps, and index tracking flags.

Pipeline steps progressively enrich this object: step 1 might set `raw_content` and `type`, step 2 adds `summaries`, step 3 extracts `tags` and `mentions`, step 4 generates `embeddings`, etc.

**The question:** Should `BookmarkDraft` be a separate type from `KnowledgeObject`, and if so, what fields and behaviors should it have?

---

## Decision

**`BookmarkDraft` is a type alias for `KnowledgeObject`.** Pipeline steps receive and return `*KnowledgeObject` directly. There is no separate draft type, builder type, or intermediate representation.

### Definition

```go
// Draft is a KnowledgeObject being progressively enriched by pipeline steps.
// Pipeline steps populate fields incrementally. Unfilled fields remain at their
// Go zero values (nil slices, empty strings, zero time).
type Draft = KnowledgeObject
```

### Pipeline Step Interface (Updated)

```go
type PipelineStep interface {
    Name() string
    Run(ctx context.Context, draft *KnowledgeObject) (*KnowledgeObject, error)
}
```

### How Steps Use It

```go
// Step 1: Type detection — sets type and subtype
func (s *TypeDetector) Run(ctx context.Context, draft *KnowledgeObject) (*KnowledgeObject, error) {
    draft.Type = detectType(draft.RawContent)
    draft.Subtype = detectSubtype(draft.RawContent, draft.Type)
    return draft, nil
}

// Step 3: Tag extraction — adds tags (earlier steps already set summaries)
func (s *TagExtractor) Run(ctx context.Context, draft *KnowledgeObject) (*KnowledgeObject, error) {
    draft.Tags = extractTags(draft.Summaries, draft.Sections)
    return draft, nil
}
```

### When It Becomes Persistent

Only after all pipeline steps complete successfully does the ingestion manager commit the object to storage. This preserves ADR-004's constraint: "steps should not write to storage directly; only the ingestion manager commits the final bookmark."

```go
// Ingestion manager (not a pipeline step)
func (m *IngestManager) Run(ctx context.Context, job *Job) error {
    draft := &KnowledgeObject{
        ID:         uuid.New().String(),
        RawContent: job.Payload,
        Pipeline:   job.Pipeline,
        Source:     job.Source,
        CreatedAt:  time.Now(),
    }

    for _, step := range pipeline.Steps() {
        var err error
        draft, err = step.Run(ctx, draft)
        if err != nil {
            return fmt.Errorf("step %s failed: %w", step.Name(), err)
        }
    }

    draft.UpdatedAt = time.Now()
    return m.store.CreateObject(ctx, draft)
}
```

---

## Rationale

### Alternatives Considered

#### 1. Separate Mutable Builder Type (Rejected)

A `BookmarkDraft` struct with builder methods (`SetSummary`, `AddTag`, `AddMention`) that converts to `KnowledgeObject` at the end.

**Rejected because:**
- Adds a translation/mapping layer that must be maintained as KnowledgeObject evolves
- When new fields are added to KnowledgeObject (including plugin fields via `Plugins map[string]any`), the builder must be updated too — maintenance burden with no safety gain
- Pipeline steps are sequential and cooperative — step N trusts steps 1..N-1 did their job. A builder's "enforce field population order" benefit doesn't apply to cooperative sequential steps
- Plugin-defined fields (`plugins.<name>`) are arbitrary JSON — a builder can't anticipate them at compile time
- Adds conversion overhead (draft→object) at the end of every pipeline run

#### 2. Generic `map[string]any` (Rejected)

Pipeline passes a flexible map between steps.

**Rejected because:**
- Zero type safety — typos in field names become runtime bugs
- Not idiomatic Go
- No IDE autocompletion or refactoring support
- Performance overhead from interface boxing/unboxing

### Benefits of Chosen Approach

- **Zero maintenance burden** — as KnowledgeObject evolves, the draft type stays in sync automatically
- **No mapping layer** — what steps build is exactly what gets stored
- **Go zero values are meaningful** — `nil` slices mean "not yet extracted", empty strings mean "not yet set", `time.Time{}` means "not yet timestamped"
- **Plugin fields work naturally** — `draft.Plugins["rss_feed"] = map[string]any{...}` needs no builder support
- **Simple, idiomatic Go** — no ceremony, no abstraction for abstraction's sake
- **Aligns with YAGNI** — if compile-time draft validation is ever needed, a builder can be added later without breaking the interface (still `*KnowledgeObject` in, `*KnowledgeObject` out)

---

## Consequences

### Positive

- Pipeline step interface is simple and concrete
- No type proliferation — one struct for objects throughout the system
- Steps can read any previously-set field directly
- Testing is straightforward — construct a `KnowledgeObject`, run a step, assert fields

### Negative

- No compile-time enforcement of "field X must be set before step Y reads it"
- A step could accidentally overwrite a field set by a previous step
- Mitigated by: step ordering in pipeline definitions, integration tests, step isolation principle from ADR-004

### Neutral

- The `Draft` type alias exists purely for documentation — call sites can use either name
- `architecture.md`'s `PipelineStep` interface using `Execute(ctx, input any) (output any, error)` is superseded by ADR-004's concrete `Run(ctx, *KnowledgeObject)` form

---

## Implementation Notes

### KnowledgeObject Struct (from schema)

```go
type KnowledgeObject struct {
    ID                 string            `json:"id"`
    Type               string            `json:"type"`
    Subtype            string            `json:"subtype,omitempty"`
    RawContent         string            `json:"raw_content"`
    ContentType        string            `json:"content_type,omitempty"`
    Metadata           map[string]any    `json:"metadata,omitempty"`
    Summaries          []string          `json:"summaries,omitempty"`
    Sections           []Section         `json:"sections,omitempty"`
    Tags               []Tag             `json:"tags,omitempty"`
    Mentions           []string          `json:"mentions,omitempty"`       // Denormalized cache (ADR-049)
    Decisions          []Decision        `json:"decisions,omitempty"`
    Tasks              []Task            `json:"tasks,omitempty"`
    Embeddings         []float32         `json:"embeddings,omitempty"`
    Pipeline           string            `json:"pipeline,omitempty"`
    Source             string            `json:"source,omitempty"`
    RegistryInfluences []string          `json:"registry_influences,omitempty"`
    Plugins            map[string]any    `json:"plugins,omitempty"`
    CreatedAt          time.Time         `json:"created_at"`
    UpdatedAt          time.Time         `json:"updated_at"`
    FTSIndexed         bool              `json:"fts_indexed"`
    VectorIndexed      bool              `json:"vector_indexed"`
}

type Draft = KnowledgeObject
```

---

## References

- **ADR-004** — Step-Based Pipeline Architecture (defines `PipelineStep` interface with `*BookmarkDraft`)
- **design.md:336-409** — Knowledge Object schema (all fields)
- **architecture.md:739-746** — `PipelineStep` interface (generic form)
- **cross-package-contracts.md:82-89** — `PipelineDefinition` interface
- **dpkms/storage.md:329-370** — ObjectStore interface

---
