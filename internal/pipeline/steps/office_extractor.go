package steps

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// OfficeExtractor extracts text from Office documents using a DocumentProvider.
type OfficeExtractor struct {
	pipeline.BaseContract
	provider providers.DocumentProvider
}

// OfficeExtractorOption configures an OfficeExtractor.
type OfficeExtractorOption func(*OfficeExtractor)

// WithOfficeDocumentProvider sets the DocumentProvider for Office extraction.
func WithOfficeDocumentProvider(p providers.DocumentProvider) OfficeExtractorOption {
	return func(e *OfficeExtractor) { e.provider = p }
}

// NewOfficeExtractor creates an OfficeExtractor with optional configuration.
func NewOfficeExtractor(opts ...OfficeExtractorOption) *OfficeExtractor {
	e := &OfficeExtractor{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{},
			Produces: []string{"RawContent", "Sections"},
		}),
		provider: providers.NewStubDocumentProvider(),
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

func (s *OfficeExtractor) Name() string { return "office_extractor" }

func (s *OfficeExtractor) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	result, err := s.provider.ExtractOffice(ctx, draft.Source)
	if err != nil {
		// Fall back to RawContent if provider fails.
		draft.Metadata["office_extraction_error"] = err.Error()
		return draft, nil
	}

	draft.RawContent = result.FullText
	draft.Metadata["office_page_count"] = result.PageCount
	draft.Metadata["office_title"] = result.Title
	draft.Metadata["document_provider"] = s.provider.Name()

	for _, page := range result.Pages {
		draft.Sections = append(draft.Sections, storage.Section{
			Title:   fmt.Sprintf("Page %d", page.Number),
			Content: page.Content,
			Order:   len(draft.Sections),
			Metadata: map[string]any{
				"type":        "page",
				"page_number": page.Number,
			},
		})
	}

	return draft, nil
}
