package steps

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
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
	// OCR produces: one artifact node (image ref) + one section node (extracted text).
	if draft.ID == "" {
		return draft, nil
	}
	if draft.Graph == nil {
		draft.Graph = &pluginapi.ObjectGraph{}
	}
	artifactID := pluginapi.NewNodeID(draft.ID, pluginapi.NodeTypeArtifact, 0)
	if draft.Graph.FindNode(artifactID) == nil {
		draft.Graph.Nodes = append(draft.Graph.Nodes, pluginapi.GraphNode{
			ID:       artifactID,
			NodeType: pluginapi.NodeTypeArtifact,
			Label:    "source-image",
			Content:  draft.ContentType,
			Order:    0,
		})
	}
	secOrdinal := 0
	for _, n := range draft.Graph.Nodes {
		if n.NodeType == pluginapi.NodeTypeSection {
			secOrdinal++
		}
	}
	secID := pluginapi.NewNodeID(draft.ID, pluginapi.NodeTypeSection, secOrdinal)
	draft.Graph.Nodes = append(draft.Graph.Nodes, pluginapi.GraphNode{
		ID:       secID,
		NodeType: pluginapi.NodeTypeSection,
		Label:    "OCR Text",
		Content:  result.Text,
		Order:    0,
		Metadata: map[string]any{
			"ocr_confidence": result.Confidence,
		},
	})
	draft.Graph.Edges = append(draft.Graph.Edges, pluginapi.GraphEdge{
		ID:       fmt.Sprintf("%s->%s", artifactID, secID),
		FromID:   artifactID,
		ToID:     secID,
		EdgeType: pluginapi.EdgeTypeDerivedFrom,
	})

	return draft, nil
}
