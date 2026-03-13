package autosuggest

import (
	"context"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// AutoSuggestStep is a pipeline.PipelineStep that suggests tags and mentions.
type AutoSuggestStep struct {
	pipeline.BaseContract
	cfg AutoSuggestConfig
	llm LLMProvider
}

// NewAutoSuggestStep creates an AutoSuggestStep with the given config and LLM provider.
func NewAutoSuggestStep(cfg AutoSuggestConfig, llm LLMProvider) *AutoSuggestStep {
	return &AutoSuggestStep{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires:     []string{"RawContent"},
			Produces:     []string{"Tags", "Mentions", "Metadata"},
			Capabilities: []string{"llm"},
		}),
		cfg: cfg,
		llm: llm,
	}
}

func (s *AutoSuggestStep) Name() string { return "autosuggest" }

func (s *AutoSuggestStep) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if !s.cfg.Enabled {
		return draft, nil
	}

	tags, mentions, err := SuggestTagsAndMentions(ctx, draft, s.cfg, s.llm)
	if err != nil {
		// Soft failure: return unchanged draft.
		return draft, nil
	}
	if len(tags) == 0 && len(mentions) == 0 {
		return draft, nil
	}

	switch s.cfg.Mode {
	case "generate":
		_ = ApplyGenerate(draft, tags, mentions)
	default: // "select"
		_ = ApplySelect(draft, tags, mentions)
	}

	return draft, nil
}
