# Workflow Step Contract Design (requires/produces/capabilities)

Date: 2026-02-18

## Problem

Pipeline steps declare only `Name()` and `Run()`. No declared contracts means:
- No static validation of pipeline correctness at registration time
- Steps fail at runtime when they read fields that preceding steps haven't populated
- No way to feature-flag steps based on available subsystems (LLM, OCR, etc.)
- Pipeline composition errors discovered only when processing real data

## Decisions

| Decision | Choice |
|----------|--------|
| Contract location | On PipelineStep interface + Def overrides |
| Key vocabulary | Struct field names with short aliases |
| Unavailable capability behavior | Configurable: warn+skip (default) or error+reject (strict) |
| Validation timing | Static only (at registration/build time) |
| Approach | Extended PipelineStep interface with StepContract struct |

## Core Types

### StepContract

```go
// pipeline/contract.go

type StepContract struct {
    Requires     []string // KnowledgeObject fields the step reads
    Produces     []string // KnowledgeObject fields the step writes
    Capabilities []string // subsystem requirements: "ocr", "vision", "llm", "vector", "transcription", "diarization"
}
```

### Updated PipelineStep Interface

```go
type PipelineStep interface {
    Name() string
    Contract() StepContract
    Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error)
}
```

### BaseContract (Boilerplate Reduction)

```go
type BaseContract struct {
    contract StepContract
}

func (b BaseContract) Contract() StepContract { return b.contract }
func NewBaseContract(c StepContract) BaseContract { return BaseContract{contract: c} }
```

### SeedState

Fields guaranteed to exist when a pipeline begins:

```go
var SeedState = []string{"RawContent", "Source", "Pipeline"}
```

### Key Aliases

```go
var KeyAliases = map[string]string{
    "text":           "RawContent",
    "content_type":   "ContentType",
    "type":           "Type",
    "subtype":        "Subtype",
    "sections":       "Sections",
    "tags":           "Tags",
    "mentions":       "Mentions",
    "embeddings":     "Embeddings",
    "metadata":       "Metadata",
    "vector_indexed": "VectorIndexed",
}
```

## Step Contract Declarations

| Step | Requires | Produces | Capabilities |
|------|----------|----------|-------------|
| `typedetector` | `RawContent` | `Type`, `Subtype` | — |
| `filereader` | `Source` | `RawContent`, `ContentType` | `io` |
| `formatdetector` | `RawContent`, `ContentType` | `ContentType` | — |
| `textcleaner` | `RawContent` | `RawContent` | — |
| `sectioner` | `RawContent` | `Sections` | — |
| `tagger` | `RawContent` | `Tags` | — |
| `ocr_extractor` | `RawContent`, `ContentType` | `RawContent`, `Sections`, `Metadata.ocr_confidence`, `Metadata.ocr_provider` | `ocr` |
| `vision_analyzer` | `RawContent`, `ContentType` | `Sections`, `Metadata` | `vision` |
| `audio_transcriber` | `Source`, `ContentType` | `RawContent`, `Metadata` | `transcription` |
| `speaker_diarizer` | `RawContent`, `Metadata` | `Metadata`, `Sections` | `diarization` |
| `timestamp_aligner` | `Metadata` | `Sections` | — |
| `embedding` | `RawContent` | `Embeddings`, `VectorIndexed` | — |
| `noop` | — | — | — |

Each step embeds `BaseContract` in its struct and passes its contract via `NewBaseContract()`.

## Static Validation

### Composability Check

Walk the step list, accumulate available state starting from `SeedState`. Each step's `Requires` must be a subset of the available state. `Metadata.*` sub-keys pass if `Metadata` is available.

```go
func ValidateComposability(steps []PipelineStep) error
```

### Capability Check

Verify each step's `Capabilities` against the runtime `CapabilitySet`. Returns indices of unsatisfied steps.

```go
type CapabilitySet map[string]bool

func ValidateCapabilities(steps []PipelineStep, caps CapabilitySet) ([]int, error)
```

### Validation Order in buildPipeline

1. Resolve all steps (existing logic)
2. Build CapabilitySet from providers.Factory
3. Capability check → if strict, error; if non-strict, prune unsatisfied steps + warn
4. Apply Def overrides to remaining steps
5. Composability check on final step list
6. Register pipeline

Capability pruning before composability check prevents false failures (e.g., removing an OCR step shouldn't break downstream composability if the pipeline still works without it).

## Def Overrides

```go
type StepOverride struct {
    AddRequires  []string
    AddProduces  []string
    DropRequires []string
}
```

Applied via `overriddenStep` wrapper that delegates `Name()` and `Run()` to the original step but returns the merged contract from `Contract()`.

The existing `Def.Providers` field is retained for backward compatibility; canonical capabilities come from step contracts.

## CapabilitySet Construction

Derived from `providers.Factory`. Each non-stub provider maps to a capability. `io` is always available.

```go
func capabilitiesFromFactory(f *providers.Factory) pipeline.CapabilitySet
```

Stub detection uses type assertion against `*providers.Stub*Provider` types.

## Strict Mode

```go
type BuildOpts struct {
    Strict bool
}
```

Passed from application config. Default `false` (warn+skip). When `true`, `buildPipeline` returns error for any missing capability.

## Testing Strategy

### Unit Tests

- `TestCanonicalize` — alias resolution, passthrough, dot notation
- `TestValidateComposability_Valid` — happy path
- `TestValidateComposability_MissingRequires` — descriptive error
- `TestValidateComposability_MetadataPrefix` — `Metadata.*` passes when `Metadata` available
- `TestValidateCapabilities_AllPresent` — no unsatisfied
- `TestValidateCapabilities_MissingCap` — returns correct indices
- Per-step `TestXxxContract` — ensures declared contract matches actual field access

### Integration Tests

- `TestAllPipelinesValidateComposability` — all built-in Defs pass
- `TestStrictModeRejectsIncapable` — strict + no OCR → image.ocr registration fails
- `TestNonStrictSkipsIncapableSteps` — non-strict + no OCR → image.ocr registers with fewer steps
- `TestDefOverridesApply` — override modifies step contract correctly

## Files Changed

| File | Change |
|------|--------|
| `internal/pipeline/pipeline.go` | Add `Contract()` to interface |
| `internal/pipeline/contract.go` | **New**: StepContract, BaseContract, SeedState, KeyAliases, Canonicalize |
| `internal/pipeline/validate.go` | **New**: ValidateComposability, ValidateCapabilities, CapabilitySet |
| `internal/pipeline/steps/*.go` | All 13 steps: embed BaseContract, add contract declaration |
| `internal/pipeline/builtins/builtins.go` | Def gains Overrides field, buildPipeline gains validation + BuildOpts |
| `internal/pipeline/builtins/capabilities.go` | **New**: capabilitiesFromFactory |
| `internal/pipeline/contract_test.go` | **New**: unit tests for contract/validation |
| `internal/pipeline/builtins/builtins_test.go` | Add composability + capability integration tests |
