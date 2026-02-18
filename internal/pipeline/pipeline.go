package pipeline

import (
	"context"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// PipelineStep is a single transformation that enriches a draft.
type PipelineStep interface {
	Name() string
	Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error)
}

// Pipeline is an ordered sequence of steps.
type Pipeline struct {
	PipelineName string
	Description  string
	Steps        []PipelineStep
}

// Registry manages named pipelines.
type Registry interface {
	Register(name string, p *Pipeline) error
	Get(name string) (*Pipeline, error)
	List() []string
	SelectPipeline(content string) string
}
