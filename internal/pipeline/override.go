package pipeline

import (
	"context"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// StepOverride allows per-pipeline adjustments to a step's contract.
type StepOverride struct {
	AddRequires  []string
	AddProduces  []string
	DropRequires []string
}

// ApplyOverride wraps a step with a modified contract.
func ApplyOverride(step PipelineStep, o StepOverride) PipelineStep {
	base := step.Contract()

	drop := make(map[string]bool, len(o.DropRequires))
	for _, k := range o.DropRequires {
		drop[k] = true
	}

	var requires []string
	for _, k := range base.Requires {
		if !drop[k] {
			requires = append(requires, k)
		}
	}
	requires = append(requires, o.AddRequires...)

	produces := append([]string(nil), base.Produces...)
	produces = append(produces, o.AddProduces...)

	return &overriddenStep{
		PipelineStep: step,
		overrideContract: StepContract{
			Requires:     requires,
			Produces:     produces,
			Capabilities: base.Capabilities,
		},
	}
}

type overriddenStep struct {
	PipelineStep
	overrideContract StepContract
}

func (o *overriddenStep) Contract() StepContract { return o.overrideContract }

func (o *overriddenStep) Name() string { return o.PipelineStep.Name() }

func (o *overriddenStep) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	return o.PipelineStep.Run(ctx, draft)
}
