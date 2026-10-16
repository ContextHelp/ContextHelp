package steps

import (
	"context"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ContentClasses assigns content classes to the draft (ADR-080 §3): it
// evaluates the class rules against the draft's type, source, pipeline,
// tags, profile and structured metadata, and writes the classes of every
// matching rule to draft.Classes, keeping those already there. Built-in
// pipelines run it after structured_metadata on protected and public
// instances (builtins.InjectClassRulesStep).
type ContentClasses struct {
	pipeline.BaseContract
	rules []sharing.ClassRule
}

// NewContentClasses returns the step for rules; with none it leaves
// every draft as it is.
func NewContentClasses(rules []sharing.ClassRule) *ContentClasses {
	return &ContentClasses{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"RawContent"},
			Produces: []string{"Classes"},
		}),
		rules: rules,
	}
}

func (s *ContentClasses) Name() string { return "content_classes" }

// Run applies the rules to draft.
func (s *ContentClasses) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	draft.Classes = sharing.ApplyClassRules(s.rules, draft)
	return draft, nil
}
