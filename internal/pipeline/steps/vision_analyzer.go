package steps

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type VisionAnalyzer struct {
	pipeline.BaseContract
	provider providers.VisionProvider
}

func NewVisionAnalyzer(opts ...func(*VisionAnalyzer)) *VisionAnalyzer {
	v := &VisionAnalyzer{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires:     []string{"RawContent", "ContentType"},
			Produces:     []string{"Sections", "Metadata"},
			Capabilities: []string{"vision"},
		}),
		provider: providers.NewStubVisionProvider(),
	}
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
