package steps

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// PDFExtractor extracts text from PDF documents using a DocumentProvider.
type PDFExtractor struct {
	pipeline.BaseContract
	provider providers.DocumentProvider
}

// PDFExtractorOption configures a PDFExtractor.
type PDFExtractorOption func(*PDFExtractor)

// WithDocumentProvider sets the DocumentProvider for PDF extraction.
func WithDocumentProvider(p providers.DocumentProvider) PDFExtractorOption {
	return func(e *PDFExtractor) { e.provider = p }
}

// NewPDFExtractor creates a PDFExtractor with optional configuration.
func NewPDFExtractor(opts ...PDFExtractorOption) *PDFExtractor {
	e := &PDFExtractor{
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

func (s *PDFExtractor) Name() string { return "pdf_extractor" }

func (s *PDFExtractor) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	result, err := s.provider.ExtractPDF(ctx, draft.Source)
	if err != nil {
		return nil, fmt.Errorf("pdf_extractor: %w", err)
	}

	draft.RawContent = result.FullText
	draft.Metadata["pdf_page_count"] = result.PageCount
	draft.Metadata["pdf_title"] = result.Title
	draft.Metadata["pdf_author"] = result.Author
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
