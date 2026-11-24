// Package representation is the convention for media-derived text in an
// object graph (ADR-076 §1-2): the closed set of representation kinds,
// the provenance metadata keys, a writer that every media step emits
// through, a reader that decodes the keys, and the order in which the
// index projection reads representation nodes.
//
// Producers (OCR, captioning, transcription, frame steps, summarizers,
// sidecar import) call Emit and never build representation nodes by hand,
// so node ordinals come from one allocator and the keys stay uniform.
package representation

import "github.com/ideacrafterslabs/ctxt/internal/storage"

// Kind is a representation kind: what the text is derived from and how.
type Kind string

// The closed set of representation kinds (ADR-076 §1). A new kind needs an
// amendment to the ADR. Non-speech sound has no kind.
const (
	Caption       Kind = "caption"
	OCR           Kind = "ocr"
	Transcript    Kind = "transcript"
	FrameCaption  Kind = "frame_caption"
	FrameOCR      Kind = "frame_ocr"
	Summary       Kind = "summary"
	ExtractedText Kind = "extracted_text"
)

var kinds = []Kind{Caption, OCR, Transcript, FrameCaption, FrameOCR, Summary, ExtractedText}

// Kinds returns the closed set, in declaration order.
func Kinds() []Kind { return append([]Kind(nil), kinds...) }

// Valid reports whether k is in the closed set.
func (k Kind) Valid() bool {
	for _, v := range kinds {
		if k == v {
			return true
		}
	}
	return false
}

// NodeType is the graph node type that holds kind k: summaries are summary
// nodes, every other representation is a section node.
func (k Kind) NodeType() string {
	if k == Summary {
		return storage.NodeTypeSummary
	}
	return storage.NodeTypeSection
}

func (k Kind) isFrame() bool { return k == FrameCaption || k == FrameOCR }

// Provenance metadata keys on representation nodes (ADR-076 §2). The
// existing keys (start_ms, end_ms, speaker, page_number) are reused.
const (
	KeyRepresentation = "representation"
	KeyStartMS        = "start_ms"
	KeyEndMS          = "end_ms"
	KeySpeaker        = "speaker"
	KeyPageNumber     = "page_number"
	KeyFrameIndex     = "frame_index"
	KeyFrameMS        = "frame_ms"
	KeyImageIndex     = "image_index"
	KeyProvider       = "provider"
	KeyModel          = "model"
	KeyConfidence     = "confidence"
	KeyOrigin         = "origin"
)

// OriginSidecar marks text imported from a subtitle or transcript sidecar.
const OriginSidecar = "sidecar"

var reserved = map[string]bool{
	KeyRepresentation: true, KeyStartMS: true, KeyEndMS: true, KeySpeaker: true,
	KeyPageNumber: true, KeyFrameIndex: true, KeyFrameMS: true, KeyImageIndex: true,
	KeyProvider: true, KeyModel: true, KeyConfidence: true, KeyOrigin: true,
}
