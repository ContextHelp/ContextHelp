package steps

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
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
	if draft.Subtype != "office" {
		return draft, nil
	}

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

	// Emit canonical graph nodes when ID is set.
	if draft.ID == "" {
		return draft, nil
	}
	if draft.Graph == nil {
		draft.Graph = &pluginapi.ObjectGraph{}
	}
	rootID := pluginapi.NewNodeID(draft.ID, pluginapi.NodeTypeSummary, 0)
	if draft.Graph.FindNode(rootID) == nil {
		draft.Graph.Nodes = append(draft.Graph.Nodes, pluginapi.GraphNode{
			ID:       rootID,
			NodeType: pluginapi.NodeTypeSummary,
			Label:    "root",
			Content:  result.FullText,
			Order:    0,
		})
	}
	for i, page := range result.Pages {
		secID := pluginapi.NewNodeID(draft.ID, pluginapi.NodeTypeSection, i)
		draft.Graph.Nodes = append(draft.Graph.Nodes, pluginapi.GraphNode{
			ID:       secID,
			NodeType: pluginapi.NodeTypeSection,
			Label:    fmt.Sprintf("Page %d", page.Number),
			Content:  page.Content,
			Order:    i,
			Metadata: map[string]any{
				"type":        "page",
				"page_number": page.Number,
			},
		})
		draft.Graph.Edges = append(draft.Graph.Edges, pluginapi.GraphEdge{
			ID:       fmt.Sprintf("%s->%s", rootID, secID),
			FromID:   rootID,
			ToID:     secID,
			EdgeType: pluginapi.EdgeTypeContains,
		})
	}

	return draft, nil
}
