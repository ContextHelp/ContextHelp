package steps

import (
	"context"
	"fmt"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/providers/vision"
	"github.com/ideacrafterslabs/ctxt/internal/representation"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type VisionAnalyzer struct {
	pipeline.BaseContract
	provider vision.Provider
}

func NewVisionAnalyzer(opts ...func(*VisionAnalyzer)) *VisionAnalyzer {
	v := &VisionAnalyzer{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Binary:       true,
			Requires:     []string{"RawContent", "ContentType"},
			Produces:     []string{"Sections", "Metadata", "Graph"},
			Resources:    []string{pipeline.ResourceVision},
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

func (s *VisionAnalyzer) ProviderUses(context.Context) []pipeline.ProviderUse {
	return visionUses(s.provider)
}

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
	// The caption representation derives from the stored original when the graph has one.
	if draft.ID == "" {
		return draft, nil
	}
	if draft.Graph == nil {
		draft.Graph = &storage.ObjectGraph{}
	}
	// Text that is empty has no representation; nothing stands in for it.
	if strings.TrimSpace(result.Description) == "" {
		return draft, nil
	}
	conf := representation.ConfidenceOf(result.Confidence)
	extra := map[string]any{}
	if len(result.Labels) > 0 {
		extra["labels"] = result.Labels
	}
	if _, err := representation.Emit(draft, representation.Spec{
		Kind:       representation.Caption,
		Label:      "Vision Analysis",
		Content:    result.Description,
		Provider:   s.provider.Name(),
		Model:      representation.ModelOf(s.provider),
		Confidence: conf,
		Extra:      extra,
	}); err != nil {
		return nil, fmt.Errorf("vision_analyzer: %w", err)
	}

	return draft, nil
}
