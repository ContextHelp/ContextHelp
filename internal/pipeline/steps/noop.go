package steps

import (
	"context"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type Noop struct {
	pipeline.BaseContract
}

func NewNoop() *Noop {
	return &Noop{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{}),
	}
}

func (n *Noop) Name() string { return "noop" }

func (n *Noop) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	return draft, nil
}
