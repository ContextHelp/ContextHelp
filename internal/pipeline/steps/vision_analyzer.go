package steps

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/providers/vision"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

type VisionAnalyzer struct {
	pipeline.BaseContract
	provider vision.Provider
}

func NewVisionAnalyzer(opts ...func(*VisionAnalyzer)) *VisionAnalyzer {
	v := &VisionAnalyzer{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires:     []string{"RawContent", "ContentType"},
			Produces:     []string{"Sections", "Metadata"},
			Capabilities: []string{"vision"},
		}),
		provider: vision.NewStubProvider(),
	}
	for _, opt := range opts {
		opt(v)
	}
	return v
}

func WithVisionProvider(p vision.Provider) func(*VisionAnalyzer) {
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

	// Emit canonical graph nodes when ID is set.
	// Vision analysis produces: one artifact node (image ref) + one summary node (description).
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
	summaryID := pluginapi.NewNodeID(draft.ID, pluginapi.NodeTypeSummary, 0)
	if draft.Graph.FindNode(summaryID) == nil {
		draft.Graph.Nodes = append(draft.Graph.Nodes, pluginapi.GraphNode{
			ID:       summaryID,
			NodeType: pluginapi.NodeTypeSummary,
			Label:    "vision-description",
			Content:  result.Description,
			Order:    0,
			Metadata: map[string]any{
				"labels":     result.Labels,
				"confidence": result.Confidence,
			},
		})
		draft.Graph.Edges = append(draft.Graph.Edges, pluginapi.GraphEdge{
			ID:       fmt.Sprintf("%s->%s", artifactID, summaryID),
			FromID:   artifactID,
			ToID:     summaryID,
			EdgeType: pluginapi.EdgeTypeDerivedFrom,
		})
	}

	return draft, nil
}
