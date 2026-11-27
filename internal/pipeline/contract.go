package pipeline

import (
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blobref"
)

// StepContract declares what a pipeline step requires and produces.
type StepContract struct {
	Requires     []string // KnowledgeObject fields the step reads
	Produces     []string // KnowledgeObject fields the step writes
	Capabilities []string // subsystem requirements: "ocr", "vision", "llm", etc.
	// Version is the step's derivation version; 0 reads as 1. Bump it when
	// the step's output for the same input changes, so objects derived by
	// an older version can be found (see Signature).
	Version int
	// Resources names what the step consumes beyond its input: any of
	// ResourceLLM, ResourceEmbedding, ResourceVision, ResourceNetwork.
	// Unlike Capabilities, it never prunes a step.
	Resources []string
	// Binary marks a step that works on binary content (blobref.NeedsBlob):
	// it reads the bytes (OCR, a file reader) or the source, and emits
	// nothing but text. Every other step treats RawContent as text and
	// is skipped while the draft's content is binary, so no section,
	// node, text or metadata is ever cut from bytes.
	Binary bool
}

// SkipsBinary reports whether step must not run on draft: its content is
// binary and the step does not declare Binary.
func SkipsBinary(step PipelineStep, draft *storage.KnowledgeObject) bool {
	return draft != nil && !step.Contract().Binary && HoldsBinary(draft)
}

// ReadsMediaBytes reports whether step reads the captured media itself
// (OCR, vision, speech recognition, diarization, video, frames, document
// extraction): given text where the image, audio, video or document should
// be, it fails or invents output.
func ReadsMediaBytes(step PipelineStep) bool {
	c := step.Contract()
	if !c.Binary {
		return false
	}
	for _, cap := range c.Capabilities {
		switch cap {
		case "ocr", "vision", "transcription", "diarization", "video", "frames", "document":
			return true
		}
	}
	return false
}

// HoldsBinary reports whether draft's content is binary: held in
// RawContent, or externalized by an earlier step to a blob the draft
// still carries (BlobContent), where RawContent is only its ref.
func HoldsBinary(draft *storage.KnowledgeObject) bool {
	return blobref.NeedsBlob(ContentBytes(draft))
}

// ContentBytes is draft's content: RawContent, or, when an earlier step
// externalized it to a blob the draft still carries, those bytes rather
// than the ref.
func ContentBytes(draft *storage.KnowledgeObject) string {
	if key, ok := blobref.Key(draft.RawContent); ok {
		if content, held := draft.BlobContent[key]; held {
			return content
		}
	}
	return draft.RawContent
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
	"text":         "RawContent",
	"content_type": "ContentType",
	"type":         "Type",
	"subtype":      "Subtype",
	"sections":     "Sections",
	"tags":         "Tags",
	"mentions":     "Mentions",
	"metadata":     "Metadata",
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

// Resource values for StepContract.Resources.
const (
	ResourceLLM       = "llm"
	ResourceEmbedding = "embedding"
	ResourceVision    = "vision"
	ResourceNetwork   = "network"
)

// StepVersion is the step's derivation version: Contract().Version, with
// 0 read as 1.
func StepVersion(step PipelineStep) int {
	if v := step.Contract().Version; v > 0 {
		return v
	}
	return 1
}
