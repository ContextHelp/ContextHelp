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

type OCRExtractor struct {
	pipeline.BaseContract
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
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Binary:       true,
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

	// Emit canonical graph nodes when ID is set.
	// The ocr representation derives from the stored original when the graph has one.
	if draft.ID == "" {
		return draft, nil
	}
	if draft.Graph == nil {
		draft.Graph = &storage.ObjectGraph{}
	}
	// Text that is empty has no representation; nothing stands in for it.
	if strings.TrimSpace(result.Text) == "" {
		return draft, nil
	}
	if _, err := representation.Emit(draft, representation.Spec{
		Kind:       representation.OCR,
		Label:      "OCR Text",
		Content:    result.Text,
		Provider:   s.provider.Name(),
		Model:      representation.ModelOf(s.provider),
		Confidence: representation.ConfidenceOf(result.Confidence),
	}); err != nil {
		return nil, fmt.Errorf("ocr_extractor: %w", err)
	}

	return draft, nil
}
