# Step Contracts Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add requires/produces/capabilities contracts to PipelineStep so pipelines are validated for composability and capability availability at registration time.

**Architecture:** Extend PipelineStep interface with `Contract() StepContract`. Each step embeds `BaseContract` to self-declare its inputs, outputs, and capability needs. Validation runs at build time in `buildPipeline()`: capability check first (prune or reject), then composability walk (accumulate state set, verify requires). Def overrides allow per-pipeline contract adjustments.

**Tech Stack:** Go, existing pipeline/providers packages, table-driven tests

---

### Task 1: Core Types — StepContract, BaseContract, Aliases

**Files:**
- Create: `internal/pipeline/contract.go`
- Create: `internal/pipeline/contract_test.go`

**Step 1: Write the failing tests**

```go
// internal/pipeline/contract_test.go
package pipeline

import "testing"

func TestCanonicalizeAlias(t *testing.T) {
	tests := []struct{ input, want string }{
		{"text", "RawContent"},
		{"content_type", "ContentType"},
		{"tags", "Tags"},
		{"metadata", "Metadata"},
		{"vector_indexed", "VectorIndexed"},
	}
	for _, tt := range tests {
		if got := Canonicalize(tt.input); got != tt.want {
			t.Errorf("Canonicalize(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestCanonicalizePassthrough(t *testing.T) {
	// Non-alias keys pass through unchanged.
	tests := []string{"RawContent", "Type", "Sections", "UnknownField"}
	for _, key := range tests {
		if got := Canonicalize(key); got != key {
			t.Errorf("Canonicalize(%q) = %q, want passthrough", key, got)
		}
	}
}

func TestCanonicalizeDotNotation(t *testing.T) {
	// Metadata sub-keys pass through (not aliased individually).
	key := "Metadata.ocr_confidence"
	if got := Canonicalize(key); got != key {
		t.Errorf("Canonicalize(%q) = %q, want passthrough", key, got)
	}
}

func TestBaseContract(t *testing.T) {
	c := NewBaseContract(StepContract{
		Requires:     []string{"RawContent"},
		Produces:     []string{"Tags"},
		Capabilities: []string{"llm"},
	})
	got := c.Contract()
	if len(got.Requires) != 1 || got.Requires[0] != "RawContent" {
		t.Errorf("Requires: %v", got.Requires)
	}
	if len(got.Produces) != 1 || got.Produces[0] != "Tags" {
		t.Errorf("Produces: %v", got.Produces)
	}
	if len(got.Capabilities) != 1 || got.Capabilities[0] != "llm" {
		t.Errorf("Capabilities: %v", got.Capabilities)
	}
}

func TestEmptyBaseContract(t *testing.T) {
	c := NewBaseContract(StepContract{})
	got := c.Contract()
	if got.Requires != nil || got.Produces != nil || got.Capabilities != nil {
		t.Errorf("empty contract should have nil slices: %+v", got)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/pipeline/ -run 'TestCanonicalize|TestBaseContract|TestEmptyBaseContract' -v`
Expected: FAIL — `Canonicalize` and `NewBaseContract` undefined

**Step 3: Write contract.go**

```go
// internal/pipeline/contract.go
package pipeline

import "strings"

// StepContract declares what a step requires and produces.
type StepContract struct {
	Requires     []string // KnowledgeObject fields the step reads
	Produces     []string // KnowledgeObject fields the step writes
	Capabilities []string // subsystem requirements: "ocr", "vision", "llm", "vector", "transcription", "diarization"
}

// BaseContract provides a default Contract() implementation for embedding in step structs.
type BaseContract struct {
	contract StepContract
}

// Contract returns the step's declared contract.
func (b BaseContract) Contract() StepContract { return b.contract }

// NewBaseContract creates a BaseContract with the given contract.
func NewBaseContract(c StepContract) BaseContract { return BaseContract{contract: c} }

// SeedState is the set of keys guaranteed to exist when a pipeline begins.
var SeedState = []string{"RawContent", "Source", "Pipeline"}

// KeyAliases maps short names to canonical KnowledgeObject field names.
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

// Canonicalize resolves a key alias to its canonical KnowledgeObject field name.
// Non-alias keys (including dot-notation like "Metadata.ocr_confidence") pass through unchanged.
func Canonicalize(key string) string {
	// Only alias top-level keys, not dot-notation sub-keys.
	if !strings.Contains(key, ".") {
		if canon, ok := KeyAliases[key]; ok {
			return canon
		}
	}
	return key
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/pipeline/ -run 'TestCanonicalize|TestBaseContract|TestEmptyBaseContract' -v`
Expected: PASS

**Step 5: Commit**

```
feat(pipeline): add StepContract, BaseContract, and key alias types
```

---

### Task 2: Update PipelineStep Interface

**Files:**
- Modify: `internal/pipeline/pipeline.go:9-13`
- Modify: `internal/pipeline/pipeline_test.go:10-15` (test helper `noopStep`)

**Step 1: Write the failing test**

Add `Contract()` to the test helper in `pipeline_test.go`:

```go
// In pipeline_test.go, update noopStep:
type noopStep struct {
	BaseContract
}

// Update TestPipelineStepInterface to check Contract():
func TestPipelineStepInterface(t *testing.T) {
	var s PipelineStep = &noopStep{}
	if s.Name() != "noop" {
		t.Errorf("Name: got %q", s.Name())
	}
	c := s.Contract()
	if c.Requires != nil || c.Produces != nil || c.Capabilities != nil {
		t.Errorf("noop contract should be empty: %+v", c)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/pipeline/ -run TestPipelineStepInterface -v`
Expected: FAIL — `PipelineStep` has no `Contract()` method

**Step 3: Add Contract() to the interface**

In `internal/pipeline/pipeline.go`, update the interface:

```go
// PipelineStep is a single transformation that enriches a draft.
type PipelineStep interface {
	Name() string
	Contract() StepContract
	Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/pipeline/ -run TestPipelineStepInterface -v`
Expected: PASS

Note: This will cause compile errors in other packages. That's expected — we fix them in Tasks 3-5.

**Step 5: Commit**

```
feat(pipeline): add Contract() to PipelineStep interface
```

---

### Task 3: Add Contracts to All Built-in Steps (batch 1: simple steps)

**Files:**
- Modify: `internal/pipeline/steps/noop.go`
- Modify: `internal/pipeline/steps/typedetect.go`
- Modify: `internal/pipeline/steps/textcleaner.go`
- Modify: `internal/pipeline/steps/sectioner.go`
- Modify: `internal/pipeline/steps/tagger.go`
- Modify: `internal/pipeline/steps/filereader.go`
- Modify: `internal/pipeline/steps/formatdetector.go`
- Create: `internal/pipeline/steps/contract_test.go`

**Step 1: Write the failing tests**

```go
// internal/pipeline/steps/contract_test.go
package steps

import "testing"

func TestNoopContract(t *testing.T) {
	s := NewNoop()
	c := s.Contract()
	if c.Requires != nil || c.Produces != nil || c.Capabilities != nil {
		t.Errorf("noop: expected empty contract, got %+v", c)
	}
}

func TestTypeDetectorContract(t *testing.T) {
	s := NewTypeDetector()
	c := s.Contract()
	assertRequires(t, "typedetect", c, []string{"RawContent"})
	assertProduces(t, "typedetect", c, []string{"Type", "Subtype"})
	assertCapabilities(t, "typedetect", c, nil)
}

func TestTextCleanerContract(t *testing.T) {
	s := NewTextCleaner()
	c := s.Contract()
	assertRequires(t, "text_cleaner", c, []string{"RawContent"})
	assertProduces(t, "text_cleaner", c, []string{"RawContent"})
	assertCapabilities(t, "text_cleaner", c, nil)
}

func TestSectionerContract(t *testing.T) {
	s := NewSectioner()
	c := s.Contract()
	assertRequires(t, "sectioner", c, []string{"RawContent"})
	assertProduces(t, "sectioner", c, []string{"Sections"})
	assertCapabilities(t, "sectioner", c, nil)
}

func TestTaggerContract(t *testing.T) {
	s := NewTagger()
	c := s.Contract()
	assertRequires(t, "tagger", c, []string{"RawContent"})
	assertProduces(t, "tagger", c, []string{"Tags"})
	assertCapabilities(t, "tagger", c, nil)
}

func TestFileReaderContract(t *testing.T) {
	s := NewFileReader()
	c := s.Contract()
	assertRequires(t, "file_reader", c, []string{"Source"})
	assertProduces(t, "file_reader", c, []string{"RawContent", "Metadata"})
	assertCapabilities(t, "file_reader", c, []string{"io"})
}

func TestFormatDetectorContract(t *testing.T) {
	s := NewFormatDetector()
	c := s.Contract()
	assertRequires(t, "format_detector", c, []string{"RawContent"})
	assertProduces(t, "format_detector", c, []string{"ContentType", "Type", "Subtype", "Metadata"})
	assertCapabilities(t, "format_detector", c, nil)
}

// --- helpers ---

func assertRequires(t *testing.T, name string, c pipeline.StepContract, want []string) {
	t.Helper()
	if !equalSorted(c.Requires, want) {
		t.Errorf("%s Requires: got %v, want %v", name, c.Requires, want)
	}
}

func assertProduces(t *testing.T, name string, c pipeline.StepContract, want []string) {
	t.Helper()
	if !equalSorted(c.Produces, want) {
		t.Errorf("%s Produces: got %v, want %v", name, c.Produces, want)
	}
}

func assertCapabilities(t *testing.T, name string, c pipeline.StepContract, want []string) {
	t.Helper()
	if !equalSorted(c.Capabilities, want) {
		t.Errorf("%s Capabilities: got %v, want %v", name, c.Capabilities, want)
	}
}

func equalSorted(a, b []string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	if len(a) != len(b) {
		return false
	}
	am := make(map[string]bool, len(a))
	for _, s := range a {
		am[s] = true
	}
	for _, s := range b {
		if !am[s] {
			return false
		}
	}
	return true
}
```

Note: The test file needs `import "github.com/ideacrafterslabs/ctxt/internal/pipeline"` for `pipeline.StepContract`. Since the steps package already imports the pipeline package, use the full type path in the helper signatures.

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/pipeline/steps/ -run 'TestNoopContract|TestTypeDetectorContract|TestTextCleanerContract|TestSectionerContract|TestTaggerContract|TestFileReaderContract|TestFormatDetectorContract' -v`
Expected: FAIL — steps don't implement `Contract()` yet

**Step 3: Add BaseContract to each step**

For each file, embed `pipeline.BaseContract` and update the constructor. Example for `noop.go`:

```go
// noop.go
type Noop struct {
	pipeline.BaseContract
}

func NewNoop() *Noop {
	return &Noop{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{}),
	}
}
```

For `typedetect.go`:

```go
type TypeDetector struct {
	pipeline.BaseContract
}

func NewTypeDetector() *TypeDetector {
	return &TypeDetector{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"RawContent"},
			Produces: []string{"Type", "Subtype"},
		}),
	}
}
```

For `textcleaner.go`:

```go
type TextCleaner struct {
	pipeline.BaseContract
}

func NewTextCleaner() *TextCleaner {
	return &TextCleaner{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"RawContent"},
			Produces: []string{"RawContent"},
		}),
	}
}
```

For `sectioner.go`:

```go
type Sectioner struct {
	pipeline.BaseContract
}

func NewSectioner() *Sectioner {
	return &Sectioner{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"RawContent"},
			Produces: []string{"Sections"},
		}),
	}
}
```

For `tagger.go`:

```go
type Tagger struct {
	pipeline.BaseContract
	maxTags int
}

func NewTagger() *Tagger {
	return &Tagger{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"RawContent"},
			Produces: []string{"Tags"},
		}),
		maxTags: 10,
	}
}
```

For `filereader.go`:

```go
type FileReader struct {
	pipeline.BaseContract
	maxFileSize int64
}

func NewFileReader(opts ...FileReaderOption) *FileReader {
	fr := &FileReader{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires:     []string{"Source"},
			Produces:     []string{"RawContent", "Metadata"},
			Capabilities: []string{"io"},
		}),
		maxFileSize: 50 * 1024 * 1024,
	}
	for _, opt := range opts {
		opt(fr)
	}
	return fr
}
```

For `formatdetector.go`:

```go
type FormatDetector struct {
	pipeline.BaseContract
}

func NewFormatDetector() *FormatDetector {
	return &FormatDetector{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"RawContent"},
			Produces: []string{"ContentType", "Type", "Subtype", "Metadata"},
		}),
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/pipeline/steps/ -run 'TestNoopContract|TestTypeDetectorContract|TestTextCleanerContract|TestSectionerContract|TestTaggerContract|TestFileReaderContract|TestFormatDetectorContract' -v`
Expected: PASS

**Step 5: Run existing step tests to verify no regressions**

Run: `go test ./internal/pipeline/steps/ -v`
Expected: All existing tests still pass

**Step 6: Commit**

```
feat(steps): add contracts to simple pipeline steps
```

---

### Task 4: Add Contracts to Provider-Dependent Steps (batch 2)

**Files:**
- Modify: `internal/pipeline/steps/ocr_extractor.go`
- Modify: `internal/pipeline/steps/vision_analyzer.go`
- Modify: `internal/pipeline/steps/audio_transcriber.go`
- Modify: `internal/pipeline/steps/speaker_diarizer.go`
- Modify: `internal/pipeline/steps/timestamp_aligner.go`
- Modify: `internal/pipeline/steps/embedding.go`
- Modify: `internal/pipeline/steps/contract_test.go` (add tests)

**Step 1: Add tests to contract_test.go**

```go
func TestOCRExtractorContract(t *testing.T) {
	s := NewOCRExtractor()
	c := s.Contract()
	assertRequires(t, "ocr_extractor", c, []string{"RawContent", "ContentType"})
	assertProduces(t, "ocr_extractor", c, []string{"RawContent", "Sections", "Metadata"})
	assertCapabilities(t, "ocr_extractor", c, []string{"ocr"})
}

func TestVisionAnalyzerContract(t *testing.T) {
	s := NewVisionAnalyzer()
	c := s.Contract()
	assertRequires(t, "vision_analyzer", c, []string{"RawContent", "ContentType"})
	assertProduces(t, "vision_analyzer", c, []string{"Sections", "Metadata"})
	assertCapabilities(t, "vision_analyzer", c, []string{"vision"})
}

func TestAudioTranscriberContract(t *testing.T) {
	s := NewAudioTranscriber()
	c := s.Contract()
	assertRequires(t, "audio_transcriber", c, []string{"Source", "ContentType"})
	assertProduces(t, "audio_transcriber", c, []string{"RawContent", "Metadata"})
	assertCapabilities(t, "audio_transcriber", c, []string{"transcription"})
}

func TestSpeakerDiarizerContract(t *testing.T) {
	s := NewSpeakerDiarizer(false)
	c := s.Contract()
	assertRequires(t, "speaker_diarizer", c, []string{"RawContent", "Metadata"})
	assertProduces(t, "speaker_diarizer", c, []string{"Metadata", "Sections"})
	assertCapabilities(t, "speaker_diarizer", c, []string{"diarization"})
}

func TestTimestampAlignerContract(t *testing.T) {
	s := NewTimestampAligner()
	c := s.Contract()
	assertRequires(t, "timestamp_aligner", c, []string{"Metadata"})
	assertProduces(t, "timestamp_aligner", c, []string{"Sections"})
	assertCapabilities(t, "timestamp_aligner", c, nil)
}

func TestEmbeddingGeneratorContract(t *testing.T) {
	s := NewEmbeddingGenerator()
	c := s.Contract()
	assertRequires(t, "embedding_generator", c, []string{"RawContent"})
	assertProduces(t, "embedding_generator", c, []string{"Embeddings", "VectorIndexed"})
	assertCapabilities(t, "embedding_generator", c, nil)
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/pipeline/steps/ -run 'TestOCRExtractorContract|TestVisionAnalyzerContract|TestAudioTranscriberContract|TestSpeakerDiarizerContract|TestTimestampAlignerContract|TestEmbeddingGeneratorContract' -v`
Expected: FAIL

**Step 3: Add BaseContract to each step**

For `ocr_extractor.go`:

```go
type OCRExtractor struct {
	pipeline.BaseContract
	provider            providers.OCRProvider
	confidenceThreshold float64
}

func NewOCRExtractor(opts ...OCRExtractorOption) *OCRExtractor {
	o := &OCRExtractor{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires:     []string{"RawContent", "ContentType"},
			Produces:     []string{"RawContent", "Sections", "Metadata"},
			Capabilities: []string{"ocr"},
		}),
		provider:            providers.NewStubOCRProvider(),
		confidenceThreshold: 0.60,
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}
```

For `vision_analyzer.go`:

```go
type VisionAnalyzer struct {
	pipeline.BaseContract
	provider providers.VisionProvider
}

func NewVisionAnalyzer(opts ...func(*VisionAnalyzer)) *VisionAnalyzer {
	v := &VisionAnalyzer{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires:     []string{"RawContent", "ContentType"},
			Produces:     []string{"Sections", "Metadata"},
			Capabilities: []string{"vision"},
		}),
		provider: providers.NewStubVisionProvider(),
	}
	for _, opt := range opts {
		opt(v)
	}
	return v
}
```

For `audio_transcriber.go`:

```go
type AudioTranscriber struct {
	pipeline.BaseContract
	provider providers.TranscriptionProvider
}

func NewAudioTranscriber(opts ...func(*AudioTranscriber)) *AudioTranscriber {
	a := &AudioTranscriber{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires:     []string{"Source", "ContentType"},
			Produces:     []string{"RawContent", "Metadata"},
			Capabilities: []string{"transcription"},
		}),
		provider: providers.NewStubTranscriptionProvider(),
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}
```

For `speaker_diarizer.go`:

```go
type SpeakerDiarizer struct {
	pipeline.BaseContract
	provider providers.DiarizationProvider
	enabled  bool
}

func NewSpeakerDiarizer(enabled bool, opts ...func(*SpeakerDiarizer)) *SpeakerDiarizer {
	s := &SpeakerDiarizer{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires:     []string{"RawContent", "Metadata"},
			Produces:     []string{"Metadata", "Sections"},
			Capabilities: []string{"diarization"},
		}),
		provider: providers.NewStubDiarizationProvider(),
		enabled:  enabled,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}
```

For `timestamp_aligner.go`:

```go
type TimestampAligner struct {
	pipeline.BaseContract
}

func NewTimestampAligner() *TimestampAligner {
	return &TimestampAligner{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"Metadata"},
			Produces: []string{"Sections"},
		}),
	}
}
```

For `embedding.go`:

```go
type EmbeddingGenerator struct {
	pipeline.BaseContract
}

func NewEmbeddingGenerator() *EmbeddingGenerator {
	return &EmbeddingGenerator{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"RawContent"},
			Produces: []string{"Embeddings", "VectorIndexed"},
		}),
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/pipeline/steps/ -run 'TestOCRExtractorContract|TestVisionAnalyzerContract|TestAudioTranscriberContract|TestSpeakerDiarizerContract|TestTimestampAlignerContract|TestEmbeddingGeneratorContract' -v`
Expected: PASS

**Step 5: Run all step tests**

Run: `go test ./internal/pipeline/steps/ -v`
Expected: All pass

**Step 6: Commit**

```
feat(steps): add contracts to provider-dependent pipeline steps
```

---

### Task 5: Fix External PipelineStep Implementors

**Files:**
- Modify: `internal/steps/executor.go:311-322`
- Modify: `internal/jobs/worker_test.go:191-197`

**Step 1: Add BaseContract to ExternalStep**

In `internal/steps/executor.go`:

```go
type ExternalStep struct {
	pipeline.BaseContract
	name    string
	path    string
	config  map[string]any
	sandbox *storage.SandboxConfig
}
```

Update wherever `ExternalStep` is constructed to include `BaseContract: pipeline.NewBaseContract(pipeline.StepContract{})`.

**Step 2: Add Contract() to test helpers**

In `internal/jobs/worker_test.go`, update `mentionStep`:

```go
type mentionStep struct {
	pipeline.BaseContract
}

func (s *mentionStep) Name() string { return "test-mention" }
func (s *mentionStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	draft.Mentions = []string{"@test.entity"}
	return draft, nil
}
```

Also update any place `mentionStep` is instantiated to initialize it: `&mentionStep{BaseContract: pipeline.NewBaseContract(pipeline.StepContract{})}` — or simpler, just `&mentionStep{}` since the zero value of BaseContract returns an empty StepContract.

**Step 3: Verify compilation**

Run: `go build ./...`
Expected: No compile errors

**Step 4: Run all tests**

Run: `go test ./internal/... -v -count=1`
Expected: All pass

**Step 5: Commit**

```
fix: add Contract() to ExternalStep and test helpers
```

---

### Task 6: Validation Functions — ValidateComposability

**Files:**
- Create: `internal/pipeline/validate.go`
- Modify: `internal/pipeline/contract_test.go` (add validation tests)

**Step 1: Write the failing tests**

Add to `internal/pipeline/contract_test.go`:

```go
func TestValidateComposability_Valid(t *testing.T) {
	steps := []PipelineStep{
		&stubStep{name: "a", contract: StepContract{Requires: []string{"RawContent"}, Produces: []string{"Type"}}},
		&stubStep{name: "b", contract: StepContract{Requires: []string{"Type"}, Produces: []string{"Tags"}}},
	}
	if err := ValidateComposability(steps); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateComposability_SeedState(t *testing.T) {
	// Steps that only require seed-state fields should pass with no prior steps.
	steps := []PipelineStep{
		&stubStep{name: "a", contract: StepContract{Requires: []string{"RawContent", "Source", "Pipeline"}, Produces: []string{"Type"}}},
	}
	if err := ValidateComposability(steps); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateComposability_MissingRequires(t *testing.T) {
	steps := []PipelineStep{
		&stubStep{name: "a", contract: StepContract{Requires: []string{"RawContent"}, Produces: []string{"Type"}}},
		&stubStep{name: "b", contract: StepContract{Requires: []string{"Sections"}, Produces: []string{"Tags"}}},
	}
	err := ValidateComposability(steps)
	if err == nil {
		t.Fatal("expected error for missing Sections requirement")
	}
	if !strings.Contains(err.Error(), "Sections") {
		t.Errorf("error should mention missing key: %v", err)
	}
	if !strings.Contains(err.Error(), "step 1") {
		t.Errorf("error should mention step index: %v", err)
	}
}

func TestValidateComposability_MetadataPrefix(t *testing.T) {
	// If a step produces "Metadata", a downstream step requiring "Metadata.ocr_confidence" should pass.
	steps := []PipelineStep{
		&stubStep{name: "a", contract: StepContract{Requires: []string{"RawContent"}, Produces: []string{"Metadata"}}},
		&stubStep{name: "b", contract: StepContract{Requires: []string{"Metadata.ocr_confidence"}, Produces: []string{"Tags"}}},
	}
	if err := ValidateComposability(steps); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateComposability_EmptyContracts(t *testing.T) {
	// Steps with empty contracts (like noop) should always pass.
	steps := []PipelineStep{
		&stubStep{name: "a", contract: StepContract{}},
		&stubStep{name: "b", contract: StepContract{}},
	}
	if err := ValidateComposability(steps); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateComposability_AliasResolution(t *testing.T) {
	// "text" alias should resolve to "RawContent" (available in seed state).
	steps := []PipelineStep{
		&stubStep{name: "a", contract: StepContract{Requires: []string{"text"}, Produces: []string{"tags"}}},
	}
	if err := ValidateComposability(steps); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- test helper ---

type stubStep struct {
	BaseContract
	name     string
	contract StepContract
}

func (s *stubStep) Name() string             { return s.name }
func (s *stubStep) Contract() StepContract    { return s.contract }
func (s *stubStep) Run(_ context.Context, d *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	return d, nil
}
```

Add `"strings"` and `"context"` and `"github.com/ideacrafterslabs/ctxt/internal/storage"` to the imports.

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/pipeline/ -run TestValidateComposability -v`
Expected: FAIL — `ValidateComposability` undefined

**Step 3: Write validate.go**

```go
// internal/pipeline/validate.go
package pipeline

import (
	"fmt"
	"sort"
	"strings"
)

// CapabilitySet represents what the runtime environment can provide.
type CapabilitySet map[string]bool

// ValidateComposability checks that each step's Requires are satisfied
// by the SeedState plus all preceding steps' Produces.
func ValidateComposability(steps []PipelineStep) error {
	available := make(map[string]bool, len(SeedState))
	for _, k := range SeedState {
		available[k] = true
	}

	for i, step := range steps {
		c := step.Contract()
		for _, req := range c.Requires {
			canon := Canonicalize(req)
			if !available[canon] && !available[fieldPrefix(canon)] {
				return fmt.Errorf("step %d (%s): requires %q but not produced by preceding steps (available: %v)",
					i, step.Name(), req, sortedKeys(available))
			}
		}
		for _, prod := range c.Produces {
			available[Canonicalize(prod)] = true
		}
	}
	return nil
}

// ValidateCapabilities checks all steps' required capabilities against available ones.
// Returns indices of steps with unsatisfied capabilities.
func ValidateCapabilities(steps []PipelineStep, caps CapabilitySet) []int {
	var unsatisfied []int
	for i, step := range steps {
		for _, cap := range step.Contract().Capabilities {
			if !caps[cap] {
				unsatisfied = append(unsatisfied, i)
				break
			}
		}
	}
	return unsatisfied
}

// fieldPrefix returns the top-level field name from a dot-notation key.
// e.g., "Metadata.ocr_confidence" → "Metadata"
func fieldPrefix(key string) string {
	if idx := strings.IndexByte(key, '.'); idx >= 0 {
		return key[:idx]
	}
	return ""
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// RemoveIndices returns a new slice with elements at the given indices removed.
func RemoveIndices(steps []PipelineStep, indices []int) []PipelineStep {
	remove := make(map[int]bool, len(indices))
	for _, idx := range indices {
		remove[idx] = true
	}
	result := make([]PipelineStep, 0, len(steps)-len(indices))
	for i, s := range steps {
		if !remove[i] {
			result = append(result, s)
		}
	}
	return result
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/pipeline/ -run TestValidateComposability -v`
Expected: PASS

**Step 5: Commit**

```
feat(pipeline): add ValidateComposability and ValidateCapabilities
```

---

### Task 7: Validation Functions — ValidateCapabilities

**Files:**
- Modify: `internal/pipeline/contract_test.go` (add capability tests)

**Step 1: Write the failing tests**

Add to `internal/pipeline/contract_test.go`:

```go
func TestValidateCapabilities_AllPresent(t *testing.T) {
	steps := []PipelineStep{
		&stubStep{name: "a", contract: StepContract{Capabilities: []string{"ocr"}}},
		&stubStep{name: "b", contract: StepContract{Capabilities: []string{"vision"}}},
	}
	caps := CapabilitySet{"ocr": true, "vision": true}
	unsatisfied := ValidateCapabilities(steps, caps)
	if len(unsatisfied) != 0 {
		t.Errorf("expected no unsatisfied, got %v", unsatisfied)
	}
}

func TestValidateCapabilities_MissingCap(t *testing.T) {
	steps := []PipelineStep{
		&stubStep{name: "a", contract: StepContract{Capabilities: []string{"ocr"}}},
		&stubStep{name: "b", contract: StepContract{}},
		&stubStep{name: "c", contract: StepContract{Capabilities: []string{"vision"}}},
	}
	caps := CapabilitySet{"ocr": true}
	unsatisfied := ValidateCapabilities(steps, caps)
	if len(unsatisfied) != 1 || unsatisfied[0] != 2 {
		t.Errorf("expected [2], got %v", unsatisfied)
	}
}

func TestValidateCapabilities_NoCaps(t *testing.T) {
	steps := []PipelineStep{
		&stubStep{name: "a", contract: StepContract{}},
	}
	unsatisfied := ValidateCapabilities(steps, CapabilitySet{})
	if len(unsatisfied) != 0 {
		t.Errorf("expected no unsatisfied, got %v", unsatisfied)
	}
}

func TestRemoveIndices(t *testing.T) {
	a := &stubStep{name: "a"}
	b := &stubStep{name: "b"}
	c := &stubStep{name: "c"}
	steps := []PipelineStep{a, b, c}
	result := RemoveIndices(steps, []int{1})
	if len(result) != 2 || result[0].Name() != "a" || result[1].Name() != "c" {
		t.Errorf("unexpected result: %v", result)
	}
}
```

**Step 2: Run tests to verify they pass (already implemented in Task 6)**

Run: `go test ./internal/pipeline/ -run 'TestValidateCapabilities|TestRemoveIndices' -v`
Expected: PASS (code was already written in Task 6)

**Step 3: Commit**

```
test(pipeline): add capability validation and RemoveIndices tests
```

---

### Task 8: Def Overrides — overriddenStep Wrapper

**Files:**
- Create: `internal/pipeline/override.go`
- Modify: `internal/pipeline/contract_test.go` (add override tests)

**Step 1: Write the failing tests**

Add to `internal/pipeline/contract_test.go`:

```go
func TestApplyOverride_AddRequires(t *testing.T) {
	base := &stubStep{name: "a", contract: StepContract{
		Requires: []string{"RawContent"},
		Produces: []string{"Tags"},
	}}
	wrapped := ApplyOverride(base, StepOverride{AddRequires: []string{"ContentType"}})
	c := wrapped.Contract()
	if !equalSorted(c.Requires, []string{"RawContent", "ContentType"}) {
		t.Errorf("Requires: %v", c.Requires)
	}
	if wrapped.Name() != "a" {
		t.Errorf("Name: %q", wrapped.Name())
	}
}

func TestApplyOverride_DropRequires(t *testing.T) {
	base := &stubStep{name: "a", contract: StepContract{
		Requires: []string{"RawContent", "ContentType"},
		Produces: []string{"Tags"},
	}}
	wrapped := ApplyOverride(base, StepOverride{DropRequires: []string{"ContentType"}})
	c := wrapped.Contract()
	if !equalSorted(c.Requires, []string{"RawContent"}) {
		t.Errorf("Requires: %v", c.Requires)
	}
}

func TestApplyOverride_AddProduces(t *testing.T) {
	base := &stubStep{name: "a", contract: StepContract{
		Requires: []string{"RawContent"},
		Produces: []string{"Tags"},
	}}
	wrapped := ApplyOverride(base, StepOverride{AddProduces: []string{"Metadata"}})
	c := wrapped.Contract()
	if !equalSorted(c.Produces, []string{"Tags", "Metadata"}) {
		t.Errorf("Produces: %v", c.Produces)
	}
}

func TestApplyOverride_PreservesCapabilities(t *testing.T) {
	base := &stubStep{name: "a", contract: StepContract{
		Capabilities: []string{"ocr"},
	}}
	wrapped := ApplyOverride(base, StepOverride{AddRequires: []string{"Source"}})
	c := wrapped.Contract()
	if !equalSorted(c.Capabilities, []string{"ocr"}) {
		t.Errorf("Capabilities: %v", c.Capabilities)
	}
}

func TestApplyOverride_Empty(t *testing.T) {
	base := &stubStep{name: "a", contract: StepContract{
		Requires: []string{"RawContent"},
		Produces: []string{"Tags"},
	}}
	wrapped := ApplyOverride(base, StepOverride{})
	c := wrapped.Contract()
	if !equalSorted(c.Requires, []string{"RawContent"}) || !equalSorted(c.Produces, []string{"Tags"}) {
		t.Errorf("empty override should not change contract: %+v", c)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/pipeline/ -run TestApplyOverride -v`
Expected: FAIL — `ApplyOverride` and `StepOverride` undefined

**Step 3: Write override.go**

```go
// internal/pipeline/override.go
package pipeline

import (
	"context"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// StepOverride allows per-pipeline adjustments to a step's contract.
type StepOverride struct {
	AddRequires  []string
	AddProduces  []string
	DropRequires []string
}

// ApplyOverride wraps a step with a modified contract.
func ApplyOverride(step PipelineStep, o StepOverride) PipelineStep {
	base := step.Contract()

	drop := make(map[string]bool, len(o.DropRequires))
	for _, k := range o.DropRequires {
		drop[k] = true
	}

	var requires []string
	for _, k := range base.Requires {
		if !drop[k] {
			requires = append(requires, k)
		}
	}
	requires = append(requires, o.AddRequires...)

	produces := append([]string(nil), base.Produces...)
	produces = append(produces, o.AddProduces...)

	return &overriddenStep{
		PipelineStep: step,
		overrideContract: StepContract{
			Requires:     requires,
			Produces:     produces,
			Capabilities: base.Capabilities,
		},
	}
}

type overriddenStep struct {
	PipelineStep
	overrideContract StepContract
}

func (o *overriddenStep) Contract() StepContract { return o.overrideContract }

func (o *overriddenStep) Name() string { return o.PipelineStep.Name() }

func (o *overriddenStep) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	return o.PipelineStep.Run(ctx, draft)
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/pipeline/ -run TestApplyOverride -v`
Expected: PASS

**Step 5: Commit**

```
feat(pipeline): add StepOverride and ApplyOverride for per-pipeline contract adjustments
```

---

### Task 9: CapabilitySet from Factory

**Files:**
- Create: `internal/pipeline/builtins/capabilities.go`
- Create: `internal/pipeline/builtins/capabilities_test.go`

**Step 1: Write the failing test**

```go
// internal/pipeline/builtins/capabilities_test.go
package builtins

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
)

func TestCapabilitiesFromFactory_NilFactory(t *testing.T) {
	caps := CapabilitiesFromFactory(nil)
	if len(caps) != 0 {
		t.Errorf("nil factory should return empty caps, got %v", caps)
	}
}

func TestCapabilitiesFromFactory_StubsOnly(t *testing.T) {
	// With stub backend, no capabilities except io.
	f := providers.NewFactory(config.ProvidersConfig{
		OCR:           config.ProviderBackendConfig{Backend: "stub"},
		Vision:        config.ProviderBackendConfig{Backend: "stub"},
		Transcription: config.ProviderBackendConfig{Backend: "stub"},
		Diarization:   config.ProviderBackendConfig{Backend: "stub"},
	})
	caps := CapabilitiesFromFactory(f)
	if !caps["io"] {
		t.Error("io capability should always be present")
	}
	for _, name := range []string{"ocr", "vision", "transcription", "diarization"} {
		if caps[name] {
			t.Errorf("%s should not be available with stub backend", name)
		}
	}
}

func TestCapabilitiesFromFactory_IOAlwaysPresent(t *testing.T) {
	f := providers.NewFactory(config.ProvidersConfig{})
	caps := CapabilitiesFromFactory(f)
	if !caps["io"] {
		t.Error("io capability should always be present")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/pipeline/builtins/ -run TestCapabilitiesFromFactory -v`
Expected: FAIL — `CapabilitiesFromFactory` undefined

**Step 3: Write capabilities.go**

```go
// internal/pipeline/builtins/capabilities.go
package builtins

import (
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
)

// CapabilitiesFromFactory builds a CapabilitySet by checking which providers
// resolve to real (non-stub) implementations.
func CapabilitiesFromFactory(f *providers.Factory) pipeline.CapabilitySet {
	caps := make(pipeline.CapabilitySet)
	if f == nil {
		return caps
	}

	// io is always available (filesystem access).
	caps["io"] = true

	if _, isStub := f.OCR().(*providers.StubOCRProvider); !isStub {
		caps["ocr"] = true
	}
	if _, isStub := f.Vision().(*providers.StubVisionProvider); !isStub {
		caps["vision"] = true
	}
	if _, isStub := f.Transcription().(*providers.StubTranscriptionProvider); !isStub {
		caps["transcription"] = true
	}
	if _, isStub := f.Diarization().(*providers.StubDiarizationProvider); !isStub {
		caps["diarization"] = true
	}

	return caps
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/pipeline/builtins/ -run TestCapabilitiesFromFactory -v`
Expected: PASS (note: the "stubs only" test depends on what the "auto" backend resolves to — if Ollama Vision is not a stub type assertion, adjust the test to use explicit "stub" config)

**Step 5: Commit**

```
feat(builtins): add CapabilitiesFromFactory for runtime capability detection
```

---

### Task 10: Integrate Validation into buildPipeline

**Files:**
- Modify: `internal/pipeline/builtins/builtins.go`
- Modify: `internal/pipeline/builtins/builtins_test.go`

**Step 1: Write the failing tests**

Add to `internal/pipeline/builtins/builtins_test.go`:

```go
func TestAllPipelinesValidateComposability(t *testing.T) {
	r := Registry()
	for _, name := range r.List() {
		p, err := r.Get(name)
		if err != nil {
			t.Errorf("Get(%q): %v", name, err)
			continue
		}
		if err := pipeline.ValidateComposability(p.Steps); err != nil {
			t.Errorf("pipeline %q composability: %v", name, err)
		}
	}
}
```

**Step 2: Run test**

Run: `go test ./internal/pipeline/builtins/ -run TestAllPipelinesValidateComposability -v`
Expected: PASS — all built-in pipelines should already be composable (we declared contracts to match reality). If any fail, it means a contract declaration is wrong — fix the contract, not the test.

**Step 3: Add Overrides field to Def and BuildOpts to buildPipeline**

Update `internal/pipeline/builtins/builtins.go`:

Add to `Def` struct:
```go
Overrides map[string]pipeline.StepOverride // per-step contract overrides, keyed by step name
```

Update `buildPipeline` signature:
```go
func buildPipeline(name string, d Def, f *providers.Factory, strict bool) (*pipeline.Pipeline, error) {
```

Update `buildPipeline` body to:
1. After resolving steps, call `CapabilitiesFromFactory(f)`
2. Call `pipeline.ValidateCapabilities(steps, caps)`
3. If strict and unsatisfied > 0, return error
4. If not strict and unsatisfied > 0, prune + log
5. Apply overrides from `d.Overrides`
6. Call `pipeline.ValidateComposability(steps)`

Update `buildRegistry` to pass `strict: false` (default).

Add `ConfiguredRegistryStrict(f *providers.Factory) pipeline.Registry` for strict mode.

**Step 4: Write the strict-mode test**

```go
func TestStrictModeRejectsIncapable(t *testing.T) {
	// With stub providers and strict mode, pipelines requiring capabilities should fail.
	// We test this by manually calling buildPipeline with strict=true.
	d := Defs()["image.ocr"]
	_, err := buildPipeline("image.ocr", d, nil, true)
	if err == nil {
		t.Error("expected error in strict mode with nil factory for image.ocr")
	}
}
```

**Step 5: Run tests**

Run: `go test ./internal/pipeline/builtins/ -v`
Expected: All pass

**Step 6: Commit**

```
feat(builtins): integrate contract validation into buildPipeline
```

---

### Task 11: Final Integration — Full Test Suite

**Files:** (none new — verification only)

**Step 1: Run full test suite**

Run: `go test ./... -count=1`
Expected: All pass

**Step 2: Run with race detector**

Run: `go test ./... -race -count=1`
Expected: No races

**Step 3: Verify build**

Run: `go build ./...`
Expected: Clean build

**Step 4: Commit (if any fixups needed)**

```
fix: address integration test issues from contract validation
```
