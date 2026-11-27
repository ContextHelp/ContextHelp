package steps

import (
	"context"
	"fmt"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/representation"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// FrameOCR runs OCR on sampled video keyframes.
type FrameOCR struct {
	pipeline.BaseContract
	provider providers.OCRProvider
}

// FrameOCROption configures a FrameOCR step.
type FrameOCROption func(*FrameOCR)

// WithFrameOCRProvider sets the OCRProvider used for frame OCR.
func WithFrameOCRProvider(p providers.OCRProvider) FrameOCROption {
	return func(f *FrameOCR) { f.provider = p }
}

// NewFrameOCR creates a FrameOCR with optional configuration.
func NewFrameOCR(opts ...FrameOCROption) *FrameOCR {
	f := &FrameOCR{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Binary:       true,
			Requires:     []string{},
			Produces:     []string{"Metadata", "Graph"},
			Capabilities: []string{"ocr"},
		}),
		provider: providers.NewStubOCRProvider(),
	}
	for _, opt := range opts {
		opt(f)
	}
	return f
}

func (s *FrameOCR) Name() string { return "frame_ocr" }

func (s *FrameOCR) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	paths, _ := draft.Metadata["frame_paths"].([]string)
	if len(paths) == 0 {
		// No frames to process.
		return draft, nil
	}

	// frame_ms, when a frame step set it, is each frame's timestamp.
	frameMS, _ := draft.Metadata["frame_ms"].([]int64)
	var texts []string
	for i, path := range paths {
		result, err := s.provider.Extract(ctx, []byte(path), "image/png")
		if err != nil {
			return nil, fmt.Errorf("frame_ocr: frame %s: %w", path, err)
		}
		if result.Text == "" {
			continue
		}
		texts = append(texts, result.Text)
		if draft.ID == "" {
			continue
		}
		anchor := representation.Anchor{}.WithFrameIndex(i)
		if i < len(frameMS) {
			anchor = anchor.WithFrame(i, max(frameMS[i], 0))
		}
		emitLenient(draft, representation.Spec{
			Kind:       representation.FrameOCR,
			Label:      fmt.Sprintf("Frame %d OCR", i+1),
			Content:    result.Text,
			Anchor:     anchor,
			Provider:   s.provider.Name(),
			Model:      representation.ModelOf(s.provider),
			Confidence: representation.ConfidenceOf(result.Confidence),
		})
	}

	draft.Metadata["frame_ocr_text"] = strings.Join(texts, "\n")

	return draft, nil
}
