# Pipeline Stories US-0003 to US-0008 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement 16 new ingestion pipelines covering image OCR, audio transcription, video processing, document parsing, feed subscriptions, and batch import.

**Architecture:** Each pipeline is an ordered sequence of `PipelineStep` implementations registered in the `pipeline.Registry`. External tools (OCR, transcription, ffmpeg) are accessed through pluggable provider interfaces with stub defaults. The worker pool (`internal/jobs/worker.go`) already handles async execution -- we just register new pipelines and steps.

**Tech Stack:** Go, SQLite (via existing storage layer), cobra (CLI), `net/http` (existing API server), provider interfaces for external tools.

---

## Conventions

All code lives under `github.com/ideacrafterslabs/ctxt`. The module path is `github.com/ideacrafterslabs/ctxt`.

**Step pattern** (copy from `internal/pipeline/steps/noop.go`):
```go
type MyStep struct{ /* config */ }
func NewMyStep(/* opts */) *MyStep { return &MyStep{} }
func (s *MyStep) Name() string { return "my_step" }
func (s *MyStep) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
    // mutate draft, return it
    return draft, nil
}
```

**Test pattern** (copy from `internal/pipeline/steps/typedetect_test.go`):
```go
func TestMyStep(t *testing.T) {
    step := NewMyStep()
    draft := &storage.KnowledgeObject{RawContent: "test"}
    got, err := step.Run(context.Background(), draft)
    if err != nil { t.Fatalf("run: %v", err) }
    // assert got.Field == expected
}
```

**Run all tests:** `go test ./...`
**Run step tests:** `go test ./internal/pipeline/steps/...`
**Run provider tests:** `go test ./internal/providers/...`

---

## Agent 1: Media Pipelines (US-0003 Image + US-0004 Audio)

Agent 1 creates shared infrastructure used by all agents, then implements image and audio pipelines.

### Task 1.1: Extend Section with Metadata

**Files:**
- Modify: `internal/storage/types.go:33-38`
- Test: `internal/pipeline/steps/sectioner_test.go` (existing tests must still pass)

**Step 1: Add Metadata field to Section**

In `internal/storage/types.go`, change the `Section` struct:

```go
type Section struct {
	Title    string         `json:"title"`
	Content  string         `json:"content"`
	Order    int            `json:"order"`
	Metadata map[string]any `json:"metadata,omitempty"`
}
```

**Step 2: Run existing tests to verify no breakage**

Run: `go test ./internal/pipeline/steps/... -v`
Expected: All existing Sectioner/Tagger tests PASS (Metadata is omitempty, so zero value is nil which serializes fine).

**Step 3: Commit**

```
feat(storage): add Metadata field to Section type

Enables pipeline steps to attach arbitrary metadata (timestamps,
speaker labels, page numbers, confidence scores) to individual
sections within a KnowledgeObject.
```

---

### Task 1.2: Provider Interfaces

**Files:**
- Create: `internal/providers/ocr.go`
- Create: `internal/providers/transcription.go`
- Create: `internal/providers/vision.go`
- Create: `internal/providers/providers_test.go`

**Step 1: Write OCR provider interface and stub**

Create `internal/providers/ocr.go`:

```go
package providers

import "context"

// OCRResult holds the output of an OCR extraction.
type OCRResult struct {
	Text       string  // Extracted text
	Confidence float64 // 0.0 to 1.0
}

// OCRProvider extracts text from images.
type OCRProvider interface {
	Name() string
	Extract(ctx context.Context, imageData []byte, contentType string) (*OCRResult, error)
}

// StubOCRProvider returns placeholder text for testing and development.
type StubOCRProvider struct{}

func NewStubOCRProvider() *StubOCRProvider { return &StubOCRProvider{} }

func (p *StubOCRProvider) Name() string { return "stub" }

func (p *StubOCRProvider) Extract(_ context.Context, imageData []byte, contentType string) (*OCRResult, error) {
	return &OCRResult{
		Text:       "[OCR text extracted from image]",
		Confidence: 0.95,
	}, nil
}
```

**Step 2: Write transcription provider interface and stub**

Create `internal/providers/transcription.go`:

```go
package providers

import (
	"context"
	"time"
)

// TranscribeOptions controls transcription behavior.
type TranscribeOptions struct {
	Language string // ISO 639-1 code (e.g., "en", "fr")
	Format   string // MIME type of the audio
}

// TranscriptSegment is a timestamped piece of transcript.
type TranscriptSegment struct {
	StartTime time.Duration
	EndTime   time.Duration
	Text      string
	Speaker   string // populated after diarization
}

// TranscriptResult holds the output of a transcription.
type TranscriptResult struct {
	FullText         string
	Segments         []TranscriptSegment
	DetectedLanguage string
	Confidence       float64
}

// TranscriptionProvider converts audio to text.
type TranscriptionProvider interface {
	Name() string
	Transcribe(ctx context.Context, audioPath string, opts TranscribeOptions) (*TranscriptResult, error)
}

// StubTranscriptionProvider returns placeholder transcript.
type StubTranscriptionProvider struct{}

func NewStubTranscriptionProvider() *StubTranscriptionProvider {
	return &StubTranscriptionProvider{}
}

func (p *StubTranscriptionProvider) Name() string { return "stub" }

func (p *StubTranscriptionProvider) Transcribe(_ context.Context, audioPath string, opts TranscribeOptions) (*TranscriptResult, error) {
	return &TranscriptResult{
		FullText: "[Transcribed audio content]",
		Segments: []TranscriptSegment{
			{StartTime: 0, EndTime: 10 * time.Second, Text: "[Transcribed audio content]"},
		},
		DetectedLanguage: "en",
		Confidence:       0.94,
	}, nil
}

// DiarizedResult holds speaker-attributed segments.
type DiarizedResult struct {
	SpeakerCount  int
	SpeakerLabels []string
	Segments      []TranscriptSegment
}

// DiarizationProvider attributes transcript segments to speakers.
type DiarizationProvider interface {
	Name() string
	Diarize(ctx context.Context, audioPath string, segments []TranscriptSegment) (*DiarizedResult, error)
}

// StubDiarizationProvider returns segments unchanged (no speaker attribution).
type StubDiarizationProvider struct{}

func NewStubDiarizationProvider() *StubDiarizationProvider {
	return &StubDiarizationProvider{}
}

func (p *StubDiarizationProvider) Name() string { return "stub" }

func (p *StubDiarizationProvider) Diarize(_ context.Context, _ string, segments []TranscriptSegment) (*DiarizedResult, error) {
	return &DiarizedResult{
		SpeakerCount:  1,
		SpeakerLabels: []string{"Speaker_1"},
		Segments:      segments,
	}, nil
}
```

**Step 3: Write vision provider interface and stub**

Create `internal/providers/vision.go`:

```go
package providers

import "context"

// VisionResult holds the output of a vision analysis.
type VisionResult struct {
	Description string  // Scene description
	Labels      []string // Detected labels/objects
	Confidence  float64
}

// VisionProvider analyzes image content beyond OCR.
type VisionProvider interface {
	Name() string
	Analyze(ctx context.Context, imageData []byte, contentType string) (*VisionResult, error)
}

// StubVisionProvider returns placeholder analysis.
type StubVisionProvider struct{}

func NewStubVisionProvider() *StubVisionProvider { return &StubVisionProvider{} }

func (p *StubVisionProvider) Name() string { return "stub" }

func (p *StubVisionProvider) Analyze(_ context.Context, _ []byte, _ string) (*VisionResult, error) {
	return &VisionResult{
		Description: "[Vision analysis of image content]",
		Labels:      []string{"image"},
		Confidence:  0.90,
	}, nil
}
```

**Step 4: Write tests for all stubs**

Create `internal/providers/providers_test.go`:

```go
package providers

import (
	"context"
	"testing"
	"time"
)

func TestStubOCR(t *testing.T) {
	p := NewStubOCRProvider()
	if p.Name() != "stub" {
		t.Errorf("Name: got %q", p.Name())
	}
	result, err := p.Extract(context.Background(), []byte("fake"), "image/png")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if result.Confidence < 0.5 {
		t.Errorf("Confidence too low: %f", result.Confidence)
	}
	if result.Text == "" {
		t.Error("Text is empty")
	}
}

func TestStubTranscription(t *testing.T) {
	p := NewStubTranscriptionProvider()
	if p.Name() != "stub" {
		t.Errorf("Name: got %q", p.Name())
	}
	result, err := p.Transcribe(context.Background(), "/fake.mp3", TranscribeOptions{Language: "en"})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if result.FullText == "" {
		t.Error("FullText is empty")
	}
	if len(result.Segments) == 0 {
		t.Error("no segments")
	}
}

func TestStubDiarization(t *testing.T) {
	p := NewStubDiarizationProvider()
	segments := []TranscriptSegment{
		{StartTime: 0, EndTime: 5 * time.Second, Text: "hello"},
	}
	result, err := p.Diarize(context.Background(), "/fake.mp3", segments)
	if err != nil {
		t.Fatalf("Diarize: %v", err)
	}
	if result.SpeakerCount != 1 {
		t.Errorf("SpeakerCount: got %d", result.SpeakerCount)
	}
	if len(result.Segments) != 1 {
		t.Errorf("Segments: got %d", len(result.Segments))
	}
}

func TestStubVision(t *testing.T) {
	p := NewStubVisionProvider()
	result, err := p.Analyze(context.Background(), []byte("fake"), "image/png")
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if result.Description == "" {
		t.Error("Description is empty")
	}
}
```

**Step 5: Run tests**

Run: `go test ./internal/providers/... -v`
Expected: All PASS.

**Step 6: Commit**

```
feat(providers): add pluggable provider interfaces for OCR, transcription, vision

Defines OCRProvider, TranscriptionProvider, DiarizationProvider, and
VisionProvider interfaces with stub implementations for development
and testing. Real providers (Tesseract, Whisper, etc.) plug in later.
```

---

### Task 1.3: Shared Pipeline Steps (FileReader, FormatDetector, TextCleaner, EmbeddingGenerator)

**Files:**
- Create: `internal/pipeline/steps/filereader.go`
- Create: `internal/pipeline/steps/filereader_test.go`
- Create: `internal/pipeline/steps/formatdetector.go`
- Create: `internal/pipeline/steps/formatdetector_test.go`
- Create: `internal/pipeline/steps/textcleaner.go`
- Create: `internal/pipeline/steps/textcleaner_test.go`
- Create: `internal/pipeline/steps/embedding.go`
- Create: `internal/pipeline/steps/embedding_test.go`

**Step 1: Write FileReader step and test**

`internal/pipeline/steps/filereader.go`:
```go
package steps

import (
	"context"
	"fmt"
	"os"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type FileReader struct {
	maxFileSize int64 // bytes; 0 means no limit
}

type FileReaderOption func(*FileReader)

func WithMaxFileSize(max int64) FileReaderOption {
	return func(fr *FileReader) { fr.maxFileSize = max }
}

func NewFileReader(opts ...FileReaderOption) *FileReader {
	fr := &FileReader{maxFileSize: 50 * 1024 * 1024} // default 50MB
	for _, opt := range opts {
		opt(fr)
	}
	return fr
}

func (s *FileReader) Name() string { return "file_reader" }

func (s *FileReader) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	path := draft.Source
	if path == "" {
		return nil, fmt.Errorf("file_reader: no source path")
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("file_reader: %w", err)
	}

	if s.maxFileSize > 0 && info.Size() > s.maxFileSize {
		return nil, fmt.Errorf("file_reader: file size %d exceeds limit %d", info.Size(), s.maxFileSize)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("file_reader: %w", err)
	}

	draft.RawContent = string(data)
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Metadata["file_size_bytes"] = info.Size()
	draft.Metadata["file_name"] = info.Name()

	return draft, nil
}
```

Test `internal/pipeline/steps/filereader_test.go`:
```go
package steps

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestFileReaderReadsFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.txt")
	os.WriteFile(path, []byte("hello world"), 0644)

	step := NewFileReader()
	draft := &storage.KnowledgeObject{Source: path}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.RawContent != "hello world" {
		t.Errorf("RawContent: got %q", got.RawContent)
	}
	if got.Metadata["file_size_bytes"] != int64(11) {
		t.Errorf("file_size_bytes: got %v", got.Metadata["file_size_bytes"])
	}
}

func TestFileReaderRejectsOversized(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "big.bin")
	os.WriteFile(path, make([]byte, 100), 0644)

	step := NewFileReader(WithMaxFileSize(50))
	draft := &storage.KnowledgeObject{Source: path}
	_, err := step.Run(context.Background(), draft)
	if err == nil {
		t.Fatal("expected error for oversized file")
	}
}

func TestFileReaderNoSource(t *testing.T) {
	step := NewFileReader()
	draft := &storage.KnowledgeObject{}
	_, err := step.Run(context.Background(), draft)
	if err == nil {
		t.Fatal("expected error for missing source")
	}
}

func TestFileReaderMissingFile(t *testing.T) {
	step := NewFileReader()
	draft := &storage.KnowledgeObject{Source: "/nonexistent/file.txt"}
	_, err := step.Run(context.Background(), draft)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
```

**Step 2: Write FormatDetector step and test**

`internal/pipeline/steps/formatdetector.go`:
```go
package steps

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type FormatDetector struct{}

func NewFormatDetector() *FormatDetector { return &FormatDetector{} }

func (s *FormatDetector) Name() string { return "format_detector" }

func (s *FormatDetector) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	// Try MIME detection from content bytes first.
	if draft.RawContent != "" {
		mime := http.DetectContentType([]byte(draft.RawContent))
		draft.ContentType = mime
	}

	// Refine from file extension if available.
	if draft.Source != "" {
		ext := strings.ToLower(filepath.Ext(draft.Source))
		draft.Metadata["file_extension"] = ext

		switch ext {
		// Images
		case ".png":
			draft.ContentType = "image/png"
			draft.Type = "image"
		case ".jpg", ".jpeg":
			draft.ContentType = "image/jpeg"
			draft.Type = "image"
		case ".webp":
			draft.ContentType = "image/webp"
			draft.Type = "image"
		case ".tiff", ".tif":
			draft.ContentType = "image/tiff"
			draft.Type = "image"
		case ".bmp":
			draft.ContentType = "image/bmp"
			draft.Type = "image"
		case ".gif":
			draft.ContentType = "image/gif"
			draft.Type = "image"

		// Audio
		case ".mp3":
			draft.ContentType = "audio/mpeg"
			draft.Type = "audio"
		case ".wav":
			draft.ContentType = "audio/wav"
			draft.Type = "audio"
		case ".ogg":
			draft.ContentType = "audio/ogg"
			draft.Type = "audio"
		case ".flac":
			draft.ContentType = "audio/flac"
			draft.Type = "audio"
		case ".m4a":
			draft.ContentType = "audio/mp4"
			draft.Type = "audio"
		case ".webm":
			// Could be audio or video; default to audio if no video track detected.
			draft.ContentType = "audio/webm"
			draft.Type = "audio"

		// Video
		case ".mp4":
			draft.ContentType = "video/mp4"
			draft.Type = "video"
		case ".mov":
			draft.ContentType = "video/quicktime"
			draft.Type = "video"
		case ".avi":
			draft.ContentType = "video/x-msvideo"
			draft.Type = "video"
		case ".mkv":
			draft.ContentType = "video/x-matroska"
			draft.Type = "video"

		// Documents
		case ".pdf":
			draft.ContentType = "application/pdf"
			draft.Type = "document"
			draft.Subtype = "pdf"
		case ".md", ".markdown":
			draft.ContentType = "text/markdown"
			draft.Type = "document"
			draft.Subtype = "markdown"
		case ".go", ".py", ".js", ".ts", ".rs", ".java", ".rb", ".cpp", ".c", ".cs":
			draft.ContentType = "text/plain"
			draft.Type = "document"
			draft.Subtype = "code"
		case ".docx":
			draft.ContentType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
			draft.Type = "document"
			draft.Subtype = "office"
		case ".epub":
			draft.ContentType = "application/epub+zip"
			draft.Type = "document"
			draft.Subtype = "epub"
		case ".html", ".htm":
			draft.ContentType = "text/html"
			draft.Type = "document"
			draft.Subtype = "html"

		default:
			return nil, fmt.Errorf("format_detector: unsupported format %q", ext)
		}
	}

	return draft, nil
}
```

Test `internal/pipeline/steps/formatdetector_test.go`:
```go
package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestFormatDetectorImage(t *testing.T) {
	step := NewFormatDetector()
	for _, ext := range []string{".png", ".jpg", ".jpeg", ".webp", ".tiff", ".bmp", ".gif"} {
		draft := &storage.KnowledgeObject{Source: "/tmp/test" + ext}
		got, err := step.Run(context.Background(), draft)
		if err != nil {
			t.Fatalf("%s: %v", ext, err)
		}
		if got.Type != "image" {
			t.Errorf("%s: Type got %q, want image", ext, got.Type)
		}
	}
}

func TestFormatDetectorAudio(t *testing.T) {
	step := NewFormatDetector()
	for _, ext := range []string{".mp3", ".wav", ".ogg", ".flac", ".m4a"} {
		draft := &storage.KnowledgeObject{Source: "/tmp/test" + ext}
		got, err := step.Run(context.Background(), draft)
		if err != nil {
			t.Fatalf("%s: %v", ext, err)
		}
		if got.Type != "audio" {
			t.Errorf("%s: Type got %q, want audio", ext, got.Type)
		}
	}
}

func TestFormatDetectorVideo(t *testing.T) {
	step := NewFormatDetector()
	for _, ext := range []string{".mp4", ".mov", ".avi", ".mkv"} {
		draft := &storage.KnowledgeObject{Source: "/tmp/test" + ext}
		got, err := step.Run(context.Background(), draft)
		if err != nil {
			t.Fatalf("%s: %v", ext, err)
		}
		if got.Type != "video" {
			t.Errorf("%s: Type got %q, want video", ext, got.Type)
		}
	}
}

func TestFormatDetectorDocument(t *testing.T) {
	step := NewFormatDetector()
	tests := map[string]string{".pdf": "pdf", ".md": "markdown", ".go": "code", ".docx": "office"}
	for ext, subtype := range tests {
		draft := &storage.KnowledgeObject{Source: "/tmp/test" + ext}
		got, err := step.Run(context.Background(), draft)
		if err != nil {
			t.Fatalf("%s: %v", ext, err)
		}
		if got.Type != "document" {
			t.Errorf("%s: Type got %q, want document", ext, got.Type)
		}
		if got.Subtype != subtype {
			t.Errorf("%s: Subtype got %q, want %q", ext, got.Subtype, subtype)
		}
	}
}

func TestFormatDetectorUnsupported(t *testing.T) {
	step := NewFormatDetector()
	draft := &storage.KnowledgeObject{Source: "/tmp/test.xyz"}
	_, err := step.Run(context.Background(), draft)
	if err == nil {
		t.Fatal("expected error for unsupported format")
	}
}
```

**Step 3: Write TextCleaner step and test**

`internal/pipeline/steps/textcleaner.go`:
```go
package steps

import (
	"context"
	"regexp"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type TextCleaner struct{}

func NewTextCleaner() *TextCleaner { return &TextCleaner{} }

func (s *TextCleaner) Name() string { return "text_cleaner" }

var multiSpace = regexp.MustCompile(`[ \t]+`)
var multiNewline = regexp.MustCompile(`\n{3,}`)

func (s *TextCleaner) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	text := draft.RawContent
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = multiSpace.ReplaceAllString(text, " ")
	text = multiNewline.ReplaceAllString(text, "\n\n")
	text = strings.TrimSpace(text)
	draft.RawContent = text
	return draft, nil
}
```

Test `internal/pipeline/steps/textcleaner_test.go`:
```go
package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestTextCleanerNormalizesSpaces(t *testing.T) {
	step := NewTextCleaner()
	draft := &storage.KnowledgeObject{RawContent: "hello    world\t\ttabs"}
	got, _ := step.Run(context.Background(), draft)
	if got.RawContent != "hello world tabs" {
		t.Errorf("got %q", got.RawContent)
	}
}

func TestTextCleanerNormalizesNewlines(t *testing.T) {
	step := NewTextCleaner()
	draft := &storage.KnowledgeObject{RawContent: "a\n\n\n\nb"}
	got, _ := step.Run(context.Background(), draft)
	if got.RawContent != "a\n\nb" {
		t.Errorf("got %q", got.RawContent)
	}
}

func TestTextCleanerTrimsWhitespace(t *testing.T) {
	step := NewTextCleaner()
	draft := &storage.KnowledgeObject{RawContent: "  hello  "}
	got, _ := step.Run(context.Background(), draft)
	if got.RawContent != "hello" {
		t.Errorf("got %q", got.RawContent)
	}
}

func TestTextCleanerCRLF(t *testing.T) {
	step := NewTextCleaner()
	draft := &storage.KnowledgeObject{RawContent: "line1\r\nline2"}
	got, _ := step.Run(context.Background(), draft)
	if got.RawContent != "line1\nline2" {
		t.Errorf("got %q", got.RawContent)
	}
}
```

**Step 4: Write EmbeddingGenerator step and test**

`internal/pipeline/steps/embedding.go`:
```go
package steps

import (
	"context"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type EmbeddingGenerator struct{}

func NewEmbeddingGenerator() *EmbeddingGenerator { return &EmbeddingGenerator{} }

func (s *EmbeddingGenerator) Name() string { return "embedding_generator" }

func (s *EmbeddingGenerator) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	// Stub: mark as ready for vector indexing. Real implementation
	// will call an embedding provider (OpenAI, local model, etc.).
	draft.VectorIndexed = true
	return draft, nil
}
```

Test `internal/pipeline/steps/embedding_test.go`:
```go
package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestEmbeddingGeneratorSetsFlag(t *testing.T) {
	step := NewEmbeddingGenerator()
	draft := &storage.KnowledgeObject{RawContent: "some text"}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !got.VectorIndexed {
		t.Error("VectorIndexed should be true")
	}
}
```

**Step 5: Run all tests**

Run: `go test ./internal/pipeline/steps/... -v`
Expected: All PASS.

**Step 6: Commit**

```
feat(steps): add FileReader, FormatDetector, TextCleaner, EmbeddingGenerator

Shared steps used across image, audio, video, and document pipelines.
FileReader validates size limits, FormatDetector routes by MIME type,
TextCleaner normalizes whitespace, EmbeddingGenerator stubs vector indexing.
```

---

### Task 1.4: Image Pipeline Steps (OCRExtractor, VisionAnalyzer)

**Files:**
- Create: `internal/pipeline/steps/ocr_extractor.go`
- Create: `internal/pipeline/steps/ocr_extractor_test.go`
- Create: `internal/pipeline/steps/vision_analyzer.go`
- Create: `internal/pipeline/steps/vision_analyzer_test.go`

**Step 1: Write OCRExtractor step and test**

`internal/pipeline/steps/ocr_extractor.go`:
```go
package steps

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type OCRExtractor struct {
	provider            providers.OCRProvider
	confidenceThreshold float64
}

type OCRExtractorOption func(*OCRExtractor)

func WithOCRProvider(p providers.OCRProvider) OCRExtractorOption {
	return func(o *OCRExtractor) { o.provider = p }
}

func WithOCRConfidenceThreshold(t float64) OCRExtractorOption {
	return func(o *OCRExtractor) { o.confidenceThreshold = t }
}

func NewOCRExtractor(opts ...OCRExtractorOption) *OCRExtractor {
	o := &OCRExtractor{
		provider:            providers.NewStubOCRProvider(),
		confidenceThreshold: 0.60,
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

func (s *OCRExtractor) Name() string { return "ocr_extractor" }

func (s *OCRExtractor) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	result, err := s.provider.Extract(ctx, []byte(draft.RawContent), draft.ContentType)
	if err != nil {
		return nil, fmt.Errorf("ocr_extractor: %w", err)
	}

	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	draft.Metadata["ocr_confidence"] = result.Confidence
	draft.Metadata["ocr_provider"] = s.provider.Name()

	if result.Confidence < s.confidenceThreshold {
		draft.Metadata["ocr_low_confidence"] = true
		draft.Tags = append(draft.Tags, storage.Tag{Label: "needs-review", Source: "ocr"})
	}

	draft.Sections = append(draft.Sections, storage.Section{
		Title:   "OCR Text",
		Content: result.Text,
		Order:   len(draft.Sections),
	})

	// Set RawContent to extracted text for downstream steps.
	draft.RawContent = result.Text

	return draft, nil
}
```

Test `internal/pipeline/steps/ocr_extractor_test.go`:
```go
package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestOCRExtractorSetsText(t *testing.T) {
	step := NewOCRExtractor()
	draft := &storage.KnowledgeObject{
		RawContent:  "fake image bytes",
		ContentType: "image/png",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(got.Sections) == 0 {
		t.Fatal("no sections created")
	}
	if got.Sections[0].Title != "OCR Text" {
		t.Errorf("section title: %q", got.Sections[0].Title)
	}
	if got.Metadata["ocr_provider"] != "stub" {
		t.Errorf("ocr_provider: %v", got.Metadata["ocr_provider"])
	}
}

func TestOCRExtractorLowConfidence(t *testing.T) {
	lowConf := &lowConfOCR{}
	step := NewOCRExtractor(WithOCRProvider(lowConf), WithOCRConfidenceThreshold(0.80))
	draft := &storage.KnowledgeObject{
		RawContent:  "fake",
		ContentType: "image/png",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Metadata["ocr_low_confidence"] != true {
		t.Error("expected ocr_low_confidence = true")
	}
	hasReview := false
	for _, tag := range got.Tags {
		if tag.Label == "needs-review" {
			hasReview = true
		}
	}
	if !hasReview {
		t.Error("expected needs-review tag")
	}
}

type lowConfOCR struct{}

func (p *lowConfOCR) Name() string { return "low" }
func (p *lowConfOCR) Extract(_ context.Context, _ []byte, _ string) (*providers.OCRResult, error) {
	return &providers.OCRResult{Text: "blurry text", Confidence: 0.30}, nil
}
```

**Step 2: Write VisionAnalyzer step and test**

`internal/pipeline/steps/vision_analyzer.go`:
```go
package steps

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type VisionAnalyzer struct {
	provider providers.VisionProvider
}

func NewVisionAnalyzer(opts ...func(*VisionAnalyzer)) *VisionAnalyzer {
	v := &VisionAnalyzer{provider: providers.NewStubVisionProvider()}
	for _, opt := range opts {
		opt(v)
	}
	return v
}

func WithVisionProvider(p providers.VisionProvider) func(*VisionAnalyzer) {
	return func(v *VisionAnalyzer) { v.provider = p }
}

func (s *VisionAnalyzer) Name() string { return "vision_analyzer" }

func (s *VisionAnalyzer) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	result, err := s.provider.Analyze(ctx, []byte(draft.RawContent), draft.ContentType)
	if err != nil {
		return nil, fmt.Errorf("vision_analyzer: %w", err)
	}

	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Metadata["vision_description"] = result.Description
	draft.Metadata["vision_labels"] = result.Labels
	draft.Metadata["vision_confidence"] = result.Confidence
	draft.Metadata["vision_provider"] = s.provider.Name()

	draft.Sections = append(draft.Sections, storage.Section{
		Title:   "Vision Analysis",
		Content: result.Description,
		Order:   len(draft.Sections),
	})

	return draft, nil
}
```

Test `internal/pipeline/steps/vision_analyzer_test.go`:
```go
package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestVisionAnalyzerAddsSection(t *testing.T) {
	step := NewVisionAnalyzer()
	draft := &storage.KnowledgeObject{
		RawContent:  "fake image",
		ContentType: "image/png",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	found := false
	for _, sec := range got.Sections {
		if sec.Title == "Vision Analysis" {
			found = true
		}
	}
	if !found {
		t.Error("expected Vision Analysis section")
	}
	if got.Metadata["vision_provider"] != "stub" {
		t.Errorf("vision_provider: %v", got.Metadata["vision_provider"])
	}
}
```

**Step 3: Run tests**

Run: `go test ./internal/pipeline/steps/... -v`
Expected: All PASS.

**Step 4: Commit**

```
feat(steps): add OCRExtractor and VisionAnalyzer pipeline steps

OCRExtractor calls a pluggable OCR provider, stores extracted text as
a section, flags low-confidence results. VisionAnalyzer calls a vision
provider for scene description beyond raw text extraction.
```

---

### Task 1.5: Audio Pipeline Steps (AudioTranscriber, SpeakerDiarizer, TimestampAligner)

**Files:**
- Create: `internal/pipeline/steps/audio_transcriber.go`
- Create: `internal/pipeline/steps/audio_transcriber_test.go`
- Create: `internal/pipeline/steps/speaker_diarizer.go`
- Create: `internal/pipeline/steps/speaker_diarizer_test.go`
- Create: `internal/pipeline/steps/timestamp_aligner.go`
- Create: `internal/pipeline/steps/timestamp_aligner_test.go`

**Step 1: Write AudioTranscriber step**

`internal/pipeline/steps/audio_transcriber.go`:
```go
package steps

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type AudioTranscriber struct {
	provider providers.TranscriptionProvider
}

func NewAudioTranscriber(opts ...func(*AudioTranscriber)) *AudioTranscriber {
	a := &AudioTranscriber{provider: providers.NewStubTranscriptionProvider()}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

func WithTranscriptionProvider(p providers.TranscriptionProvider) func(*AudioTranscriber) {
	return func(a *AudioTranscriber) { a.provider = p }
}

func (s *AudioTranscriber) Name() string { return "audio_transcriber" }

func (s *AudioTranscriber) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	lang, _ := draft.Metadata["language_hint"].(string)

	result, err := s.provider.Transcribe(ctx, draft.Source, providers.TranscribeOptions{
		Language: lang,
		Format:   draft.ContentType,
	})
	if err != nil {
		return nil, fmt.Errorf("audio_transcriber: %w", err)
	}

	draft.RawContent = result.FullText
	draft.Metadata["transcription_provider"] = s.provider.Name()
	draft.Metadata["transcription_language"] = result.DetectedLanguage
	draft.Metadata["transcription_confidence"] = result.Confidence
	draft.Metadata["_raw_segments"] = result.Segments

	return draft, nil
}
```

**Step 2: Write SpeakerDiarizer step**

`internal/pipeline/steps/speaker_diarizer.go`:
```go
package steps

import (
	"context"

	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type SpeakerDiarizer struct {
	provider providers.DiarizationProvider
	enabled  bool
}

func NewSpeakerDiarizer(enabled bool, opts ...func(*SpeakerDiarizer)) *SpeakerDiarizer {
	s := &SpeakerDiarizer{
		provider: providers.NewStubDiarizationProvider(),
		enabled:  enabled,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func WithDiarizationProvider(p providers.DiarizationProvider) func(*SpeakerDiarizer) {
	return func(s *SpeakerDiarizer) { s.provider = p }
}

func (s *SpeakerDiarizer) Name() string { return "speaker_diarizer" }

func (s *SpeakerDiarizer) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if !s.enabled {
		return draft, nil
	}

	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	segments, _ := draft.Metadata["_raw_segments"].([]providers.TranscriptSegment)

	result, err := s.provider.Diarize(ctx, draft.Source, segments)
	if err != nil {
		// Diarization failure is non-fatal.
		draft.Metadata["diarization_error"] = err.Error()
		return draft, nil
	}

	draft.Metadata["speaker_count"] = result.SpeakerCount
	draft.Metadata["speakers"] = result.SpeakerLabels
	draft.Metadata["_raw_segments"] = result.Segments

	return draft, nil
}
```

**Step 3: Write TimestampAligner step**

`internal/pipeline/steps/timestamp_aligner.go`:
```go
package steps

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type TimestampAligner struct{}

func NewTimestampAligner() *TimestampAligner { return &TimestampAligner{} }

func (s *TimestampAligner) Name() string { return "timestamp_aligner" }

func (s *TimestampAligner) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		return draft, nil
	}

	segments, _ := draft.Metadata["_raw_segments"].([]providers.TranscriptSegment)
	if len(segments) == 0 {
		return draft, nil
	}

	for i, seg := range segments {
		section := storage.Section{
			Title:   fmt.Sprintf("Segment %d", i+1),
			Content: seg.Text,
			Order:   i,
			Metadata: map[string]any{
				"start_time": seg.StartTime.String(),
				"end_time":   seg.EndTime.String(),
			},
		}
		if seg.Speaker != "" {
			section.Metadata["speaker"] = seg.Speaker
		}
		draft.Sections = append(draft.Sections, section)
	}

	// Clean up internal metadata.
	delete(draft.Metadata, "_raw_segments")

	return draft, nil
}
```

**Step 4: Write tests for all three**

Tests go in corresponding `*_test.go` files. Each tests: the step Name(), basic happy path, and edge cases (disabled diarizer, empty segments, etc.).

`internal/pipeline/steps/audio_transcriber_test.go`:
```go
package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestAudioTranscriberSetsTranscript(t *testing.T) {
	step := NewAudioTranscriber()
	draft := &storage.KnowledgeObject{
		Source:      "/tmp/test.mp3",
		ContentType: "audio/mpeg",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.RawContent == "" {
		t.Error("RawContent is empty after transcription")
	}
	if got.Metadata["transcription_provider"] != "stub" {
		t.Errorf("provider: %v", got.Metadata["transcription_provider"])
	}
}
```

`internal/pipeline/steps/speaker_diarizer_test.go`:
```go
package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestSpeakerDiarizerDisabled(t *testing.T) {
	step := NewSpeakerDiarizer(false)
	draft := &storage.KnowledgeObject{RawContent: "test"}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.RawContent != "test" {
		t.Error("draft should be unchanged when disabled")
	}
}

func TestSpeakerDiarizerEnabled(t *testing.T) {
	step := NewSpeakerDiarizer(true)
	draft := &storage.KnowledgeObject{
		Source: "/tmp/test.mp3",
		Metadata: map[string]any{
			"_raw_segments": []providers.TranscriptSegment{
				{Text: "hello"},
			},
		},
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Metadata["speaker_count"] != 1 {
		t.Errorf("speaker_count: %v", got.Metadata["speaker_count"])
	}
}
```

`internal/pipeline/steps/timestamp_aligner_test.go`:
```go
package steps

import (
	"context"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestTimestampAlignerCreatesSections(t *testing.T) {
	step := NewTimestampAligner()
	draft := &storage.KnowledgeObject{
		Metadata: map[string]any{
			"_raw_segments": []providers.TranscriptSegment{
				{StartTime: 0, EndTime: 5 * time.Second, Text: "hello", Speaker: "S1"},
				{StartTime: 5 * time.Second, EndTime: 10 * time.Second, Text: "world"},
			},
		},
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(got.Sections) != 2 {
		t.Fatalf("sections: got %d, want 2", len(got.Sections))
	}
	if got.Sections[0].Metadata["speaker"] != "S1" {
		t.Errorf("speaker: %v", got.Sections[0].Metadata["speaker"])
	}
	if _, ok := got.Metadata["_raw_segments"]; ok {
		t.Error("_raw_segments should be cleaned up")
	}
}

func TestTimestampAlignerNoSegments(t *testing.T) {
	step := NewTimestampAligner()
	draft := &storage.KnowledgeObject{Metadata: map[string]any{}}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(got.Sections) != 0 {
		t.Errorf("sections: got %d, want 0", len(got.Sections))
	}
}
```

**Step 5: Run tests**

Run: `go test ./internal/pipeline/steps/... -v`
Expected: All PASS.

**Step 6: Commit**

```
feat(steps): add AudioTranscriber, SpeakerDiarizer, TimestampAligner

AudioTranscriber calls a pluggable transcription provider and stores
timestamped segments. SpeakerDiarizer optionally attributes segments
to speakers (non-fatal on failure). TimestampAligner converts segments
into Sections with time and speaker metadata.
```

---

### Task 1.6: Register Image and Audio Pipelines + Update SelectPipeline

**Files:**
- Modify: `internal/pipeline/registry.go`
- Modify: `internal/pipeline/pipeline_test.go` (add registry tests)

**Step 1: Update SelectPipeline and register new pipelines**

In `internal/pipeline/registry.go`, update `SelectPipeline` to accept a type parameter and register the new pipelines in `DefaultRegistry`:

```go
func (r *registry) SelectPipeline(content string) string {
	// Check if content looks like a file path with a known extension.
	lower := strings.ToLower(content)

	// Image formats
	for _, ext := range []string{".png", ".jpg", ".jpeg", ".webp", ".tiff", ".tif", ".bmp", ".gif"} {
		if strings.HasSuffix(lower, ext) {
			return "image.ocr"
		}
	}

	// Audio formats
	for _, ext := range []string{".mp3", ".wav", ".ogg", ".flac", ".m4a"} {
		if strings.HasSuffix(lower, ext) {
			return "audio.transcribe"
		}
	}

	// Video formats
	for _, ext := range []string{".mp4", ".mov", ".avi", ".mkv", ".webm"} {
		if strings.HasSuffix(lower, ext) {
			return "video.full"
		}
	}

	// Document formats
	if strings.HasSuffix(lower, ".pdf") {
		return "doc.pdf"
	}
	for _, ext := range []string{".md", ".markdown"} {
		if strings.HasSuffix(lower, ext) {
			return "doc.markdown"
		}
	}
	for _, ext := range []string{".go", ".py", ".js", ".ts", ".rs", ".java", ".rb", ".cpp", ".c", ".cs"} {
		if strings.HasSuffix(lower, ext) {
			return "doc.code"
		}
	}
	for _, ext := range []string{".docx", ".doc", ".odt", ".rtf", ".epub"} {
		if strings.HasSuffix(lower, ext) {
			return "doc.office"
		}
	}

	// Default: text pipelines by length
	if len(content) < 500 {
		return "text.short"
	}
	return "text.long"
}
```

Register new pipelines in `DefaultRegistry()`:

```go
// Image pipelines
r.Register("image.ocr", &Pipeline{
    PipelineName: "image.ocr",
    Description:  "Image OCR extraction pipeline",
    Steps: []PipelineStep{
        steps.NewFileReader(),
        steps.NewFormatDetector(),
        steps.NewOCRExtractor(),
        steps.NewTextCleaner(),
        steps.NewTagger(),
        steps.NewEmbeddingGenerator(),
    },
})

r.Register("image.analysis", &Pipeline{
    PipelineName: "image.analysis",
    Description:  "Image vision analysis pipeline",
    Steps: []PipelineStep{
        steps.NewFileReader(),
        steps.NewFormatDetector(),
        steps.NewOCRExtractor(),
        steps.NewVisionAnalyzer(),
        steps.NewTextCleaner(),
        steps.NewTagger(),
        steps.NewEmbeddingGenerator(),
    },
})

// Audio pipeline
r.Register("audio.transcribe", &Pipeline{
    PipelineName: "audio.transcribe",
    Description:  "Audio transcription pipeline",
    Steps: []PipelineStep{
        steps.NewFileReader(),
        steps.NewFormatDetector(),
        steps.NewAudioTranscriber(),
        steps.NewSpeakerDiarizer(false), // diarization off by default
        steps.NewTimestampAligner(),
        steps.NewSectioner(),
        steps.NewTagger(),
        steps.NewEmbeddingGenerator(),
    },
})
```

**Step 2: Run all tests**

Run: `go test ./... -count=1`
Expected: All PASS.

**Step 3: Commit**

```
feat(pipeline): register image.ocr, image.analysis, audio.transcribe pipelines

Updates SelectPipeline to route by file extension for media types.
Registers 3 new pipelines in DefaultRegistry with full step chains.
```

---

## Agent 2: Complex Pipelines (US-0005 Video + US-0006 Document)

Agent 2 depends on Agent 1's shared steps (FileReader, FormatDetector, EmbeddingGenerator) and providers.

### Task 2.1: Video Provider Interface

**Files:**
- Create: `internal/providers/video.go`
- Create: `internal/providers/video_test.go`

Create `internal/providers/video.go` with `VideoProvider` interface (ExtractAudio, SampleFrames, ProbeMetadata) + `StubVideoProvider`. Same pattern as Agent 1's providers.

The stub returns placeholder metadata (duration, resolution, fps, codec).

**Commit:** `feat(providers): add VideoProvider interface with stub implementation`

---

### Task 2.2: Document Provider Interface

**Files:**
- Create: `internal/providers/document.go`
- Create: `internal/providers/document_test.go`

Create `internal/providers/document.go` with `DocumentProvider` interface (ExtractPDF, ExtractOffice, ParseMarkdown) + `StubDocumentProvider`.

The stub returns placeholder text extraction results with page/section metadata.

**Commit:** `feat(providers): add DocumentProvider interface with stub implementation`

---

### Task 2.3: Video Pipeline Steps

**Files:**
- Create: `internal/pipeline/steps/audio_extractor.go` + test
- Create: `internal/pipeline/steps/frame_sampler.go` + test
- Create: `internal/pipeline/steps/scene_detector.go` + test
- Create: `internal/pipeline/steps/frame_ocr.go` + test
- Create: `internal/pipeline/steps/timeline_assembler.go` + test

Each step follows the same pattern: accepts a provider via functional options, defaults to stub, mutates draft. Tests verify metadata is set correctly, sections are created, etc.

Key behaviors:
- `AudioExtractor`: stores extracted audio path in `draft.Metadata["audio_path"]`
- `FrameSampler`: stores keyframe count in `draft.Metadata["frame_count"]`
- `SceneDetector`: stores scene boundaries in `draft.Metadata["scenes"]`
- `FrameOCR`: runs OCR on each keyframe, appends text to `draft.Metadata["frame_ocr_text"]`
- `TimelineAssembler`: merges transcript + scenes + OCR into unified Sections with timestamps

**Commit:** `feat(steps): add video pipeline steps (AudioExtractor through TimelineAssembler)`

---

### Task 2.4: Document Pipeline Steps

**Files:**
- Create: `internal/pipeline/steps/pdf_extractor.go` + test
- Create: `internal/pipeline/steps/markdown_parser.go` + test
- Create: `internal/pipeline/steps/heading_splitter.go` + test
- Create: `internal/pipeline/steps/code_block_extractor.go` + test
- Create: `internal/pipeline/steps/language_detector.go` + test
- Create: `internal/pipeline/steps/ast_parser.go` + test
- Create: `internal/pipeline/steps/function_extractor.go` + test
- Create: `internal/pipeline/steps/comment_extractor.go` + test
- Create: `internal/pipeline/steps/image_extractor.go` + test
- Create: `internal/pipeline/steps/table_extractor.go` + test
- Create: `internal/pipeline/steps/office_extractor.go` + test
- Create: `internal/pipeline/steps/page_splitter.go` + test

Key behaviors:
- `PDFExtractor`: calls DocumentProvider, stores text per page in Sections, sets page count in Metadata
- `MarkdownParser`: parses Markdown into heading tree, stores headings as Sections
- `HeadingSplitter`: splits content on heading boundaries (configurable depth 1-6)
- `CodeBlockExtractor`: extracts fenced code blocks with language annotation
- `LanguageDetector`: detects programming language by extension + heuristics, sets `draft.Metadata["language"]`
- `ASTParser`: stub that stores source as structured metadata (real AST parsing is provider-pluggable)
- `FunctionExtractor`: extracts function signatures from code (Go: regex-based, others: line-based fallback)
- `CommentExtractor`: extracts TODO/FIXME/HACK annotations as Sections
- `ImageExtractor`: identifies embedded images, creates metadata entries (actual child object creation deferred)
- `TableExtractor`: identifies tables, converts to Markdown table format within Sections
- `OfficeExtractor`: calls DocumentProvider for DOCX/EPUB extraction
- `PageSplitter`: splits content into page-based Sections

**Commit:** `feat(steps): add document pipeline steps (PDF through Office extraction)`

---

### Task 2.5: Register Video and Document Pipelines

**Files:**
- Modify: `internal/pipeline/registry.go`

Register 6 new pipelines in `DefaultRegistry()`:
- `video.full` (11 steps)
- `video.audio_only` (7 steps)
- `doc.pdf`, `doc.markdown`, `doc.code`, `doc.office`

SelectPipeline already updated by Agent 1 to route these extensions.

**Commit:** `feat(pipeline): register video.full, video.audio_only, doc.pdf/markdown/code/office pipelines`

---

## Agent 3: Subscription/Import Pipelines (US-0007 Feed + US-0008 Batch)

Agent 3 is independent of media steps. Adds new storage tables and CLI commands.

### Task 3.1: Feed and Batch Storage Types

**Files:**
- Modify: `internal/storage/types.go` (add Feed, FeedItem, Batch types + filters)

Add:
```go
type Feed struct {
	ID            string    `json:"id"`
	URL           string    `json:"url"`
	Title         string    `json:"title"`
	Description   string    `json:"description"`
	SiteURL       string    `json:"site_url"`
	Format        string    `json:"format"`          // rss2.0, atom1.0, json1.1
	Status        string    `json:"status"`          // active, paused, error, suspended, gone
	SyncInterval  string    `json:"sync_interval"`
	ETag          string    `json:"etag"`
	LastModified  string    `json:"last_modified"`
	LastSync      *time.Time `json:"last_sync,omitempty"`
	ErrorCount    int       `json:"error_count"`
	LastError     string    `json:"last_error"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type FeedItem struct {
	ID         string    `json:"id"`
	FeedID     string    `json:"feed_id"`
	GUID       string    `json:"guid"`
	Link       string    `json:"link"`
	ObjectID   string    `json:"object_id"`
	IngestedAt time.Time `json:"ingested_at"`
}

type FeedFilter struct {
	Status string
	Limit  int
	Offset int
}

type Batch struct {
	ID          string         `json:"id"`
	Format      string         `json:"format"`
	TotalRecords int           `json:"total_records"`
	Completed   int            `json:"completed"`
	Failed      int            `json:"failed"`
	Status      string         `json:"status"` // processing, completed, partial, dry_run_complete
	Errors      []BatchError   `json:"errors,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

type BatchError struct {
	Line  int    `json:"line"`
	Error string `json:"error"`
}

type BatchFilter struct {
	Status string
	Limit  int
	Offset int
}

type ImportRecord struct {
	Content  string         `json:"content"`
	Type     string         `json:"type,omitempty"`
	Tags     []string       `json:"tags,omitempty"`
	Source   string         `json:"source,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}
```

Add to `StorageDriver` interface (check `internal/storage/storage.go`):
```go
Feeds() FeedStore
FeedItems() FeedItemStore
Batches() BatchStore
```

Add store interfaces:
```go
type FeedStore interface {
	Create(ctx context.Context, feed *Feed) error
	Get(ctx context.Context, id string) (*Feed, error)
	GetByURL(ctx context.Context, url string) (*Feed, error)
	List(ctx context.Context, filter FeedFilter) ([]*Feed, error)
	Update(ctx context.Context, feed *Feed) error
	Delete(ctx context.Context, id string) error
}

type FeedItemStore interface {
	Create(ctx context.Context, item *FeedItem) error
	ExistsByGUID(ctx context.Context, feedID, guid string) (bool, error)
}

type BatchStore interface {
	Create(ctx context.Context, batch *Batch) error
	Get(ctx context.Context, id string) (*Batch, error)
	Update(ctx context.Context, batch *Batch) error
}
```

**Commit:** `feat(storage): add Feed, FeedItem, Batch types and store interfaces`

---

### Task 3.2: Feed and Batch SQLite Storage

**Files:**
- Create: `internal/storage/sqlite/migrations/002_feeds_and_batches.sql`
- Create: `internal/storage/sqlite/feeds.go`
- Create: `internal/storage/sqlite/feeds_test.go`
- Create: `internal/storage/sqlite/feed_items.go`
- Create: `internal/storage/sqlite/feed_items_test.go`
- Create: `internal/storage/sqlite/batches.go`
- Create: `internal/storage/sqlite/batches_test.go`
- Modify: `internal/storage/sqlite/driver.go` (add new stores to driver)

Migration SQL:
```sql
CREATE TABLE IF NOT EXISTS feeds (
    id TEXT PRIMARY KEY,
    url TEXT NOT NULL UNIQUE,
    title TEXT DEFAULT '',
    description TEXT DEFAULT '',
    site_url TEXT DEFAULT '',
    format TEXT DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active',
    sync_interval TEXT NOT NULL DEFAULT '1h',
    etag TEXT DEFAULT '',
    last_modified TEXT DEFAULT '',
    last_sync TEXT,
    error_count INTEGER NOT NULL DEFAULT 0,
    last_error TEXT DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS feed_items (
    id TEXT PRIMARY KEY,
    feed_id TEXT NOT NULL REFERENCES feeds(id),
    guid TEXT NOT NULL,
    link TEXT DEFAULT '',
    object_id TEXT DEFAULT '',
    ingested_at TEXT NOT NULL,
    UNIQUE(feed_id, guid)
);

CREATE TABLE IF NOT EXISTS batches (
    id TEXT PRIMARY KEY,
    format TEXT NOT NULL,
    total_records INTEGER NOT NULL DEFAULT 0,
    completed INTEGER NOT NULL DEFAULT 0,
    failed INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'processing',
    errors JSON DEFAULT '[]',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_feeds_status ON feeds(status);
CREATE INDEX IF NOT EXISTS idx_feed_items_feed_id ON feed_items(feed_id);
CREATE INDEX IF NOT EXISTS idx_feed_items_guid ON feed_items(feed_id, guid);
CREATE INDEX IF NOT EXISTS idx_batches_status ON batches(status);
```

Each store implementation follows the existing pattern in `internal/storage/sqlite/jobs.go`.

**Commit:** `feat(storage): add SQLite storage for feeds, feed_items, batches`

---

### Task 3.3: Feed Pipeline Steps

**Files:**
- Create: `internal/pipeline/steps/feed_fetcher.go` + test
- Create: `internal/pipeline/steps/feed_parser.go` + test
- Create: `internal/pipeline/steps/item_deduplicator.go` + test
- Create: `internal/pipeline/steps/item_enqueuer.go` + test

Key behaviors:
- `FeedFetcher`: sends conditional GET (ETag/If-Modified-Since), handles 304/410/429 status codes
- `FeedParser`: detects RSS/Atom/JSON Feed format, extracts items (GUID, title, link, content, published)
- `ItemDeduplicator`: checks each item's GUID against FeedItemStore, filters out already-seen items
- `ItemEnqueuer`: creates one ingestion job per new item via JobStore (url.article for links, text.long for inline)

**Commit:** `feat(steps): add feed pipeline steps (FeedFetcher through ItemEnqueuer)`

---

### Task 3.4: Batch Pipeline Steps

**Files:**
- Create: `internal/pipeline/steps/jsonl_parser.go` + test
- Create: `internal/pipeline/steps/csv_parser.go` + test
- Create: `internal/pipeline/steps/column_mapper.go` + test
- Create: `internal/pipeline/steps/opml_parser.go` + test
- Create: `internal/pipeline/steps/directory_scanner.go` + test
- Create: `internal/pipeline/steps/record_validator.go` + test
- Create: `internal/pipeline/steps/batch_enqueuer.go` + test

Key behaviors:
- `JSONLParser`: reads JSONL line-by-line into `[]ImportRecord`
- `CSVParser`: reads CSV/TSV with configurable delimiter, stores rows as `[]map[string]string`
- `ColumnMapper`: maps CSV columns to ImportRecord fields using configurable mapping
- `OPMLParser`: parses OPML XML, extracts feed URLs
- `DirectoryScanner`: recursively scans directory for Markdown files, creates ImportRecords
- `RecordValidator`: validates each record (content required, size limit), splits into valid/errors
- `BatchEnqueuer`: fans out valid records into individual ingestion jobs with concurrency limit

**Commit:** `feat(steps): add batch pipeline steps (JSONLParser through BatchEnqueuer)`

---

### Task 3.5: Register Feed and Batch Pipelines

**Files:**
- Modify: `internal/pipeline/registry.go`

Register 6 pipelines in `DefaultRegistry()`:
- `feed.sync`
- `batch.jsonl`, `batch.csv`, `batch.tsv`, `batch.opml`, `batch.directory`

**Commit:** `feat(pipeline): register feed.sync and batch.* pipelines`

---

### Task 3.6: Feed CLI Commands

**Files:**
- Create: `cmd/ctxt/cmd/feed.go`
- Create: `cmd/ctxt/cmd/feed_test.go`

Subcommands: `ctxt feed add <url>`, `ctxt feed list`, `ctxt feed sync [--url <url>]`, `ctxt feed remove <url>`.

Each sends HTTP requests to the dpkms server:
- `POST /api/v1/feeds` (add)
- `GET /api/v1/feeds` (list)
- `POST /api/v1/feeds/{id}/sync` (sync)
- `DELETE /api/v1/feeds/{id}` (remove)

Follow the pattern in `cmd/ctxt/cmd/analyze.go` for server URL resolution and HTTP calls.

**Commit:** `feat(cli): add ctxt feed add/list/sync/remove commands`

---

### Task 3.7: Import CLI Commands

**Files:**
- Create: `cmd/ctxt/cmd/import.go`
- Create: `cmd/ctxt/cmd/import_test.go`

Subcommands: `ctxt import --file <path> [--format <fmt>] [--dry-run]`, `ctxt import --dir <path>`, `ctxt import status <batch_id>`.

Flags: `--file`, `--dir`, `--format` (jsonl/csv/tsv/opml/markdown), `--dry-run`, `--map-content`, `--map-type`, `--map-tags`, `--map-source`.

Sends multipart upload to `POST /api/v1/import` or queries `GET /api/v1/import/{batch_id}`.

**Commit:** `feat(cli): add ctxt import and ctxt import status commands`

---

### Task 3.8: Feed and Batch HTTP Handlers

**Files:**
- Create: `internal/server/http/handlers_feeds.go`
- Create: `internal/server/http/handlers_feeds_test.go`
- Create: `internal/server/http/handlers_import.go`
- Create: `internal/server/http/handlers_import_test.go`
- Modify: `internal/server/http/server.go` (add routes)

Feed routes:
- `POST /api/v1/feeds` -> create feed subscription
- `GET /api/v1/feeds` -> list feeds
- `POST /api/v1/feeds/{id}/sync` -> trigger sync
- `DELETE /api/v1/feeds/{id}` -> remove feed

Import routes:
- `POST /api/v1/import` -> upload file for batch import
- `GET /api/v1/import/{id}` -> get batch status

**Commit:** `feat(api): add feed and import HTTP handlers`

---

## Final Integration

After all 3 agents complete, run the full test suite:

```bash
go test ./... -count=1 -race
```

Verify all 16 pipelines are registered:

```bash
go test -run TestRegistryListsAll ./internal/pipeline/... -v
```

The registry should list: `audio.transcribe`, `batch.csv`, `batch.directory`, `batch.jsonl`, `batch.opml`, `batch.tsv`, `doc.code`, `doc.markdown`, `doc.office`, `doc.pdf`, `feed.sync`, `image.analysis`, `image.ocr`, `text.long`, `text.short`, `video.audio_only`, `video.full`.
