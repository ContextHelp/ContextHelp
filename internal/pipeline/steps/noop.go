package steps

import (
	"context"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type Noop struct{}

func NewNoop() *Noop { return &Noop{} }

func (n *Noop) Name() string { return "noop" }

func (n *Noop) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	return draft, nil
}
