package pipeline

import (
	"context"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// PipelineStep is a single transformation that enriches a draft.
type PipelineStep interface {
	Name() string
	Contract() StepContract
	Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error)
}

// Pipeline is an ordered sequence of steps.
type Pipeline struct {
	PipelineName string
	Description  string
	Steps        []PipelineStep
}

// SelectorFunc is a function that selects a pipeline name for the given content.
type SelectorFunc func(content string) string

// Registry manages named pipelines.
type Registry interface {
	Register(name string, p *Pipeline) error
	Get(name string) (*Pipeline, error)
	List() []string
	SelectPipeline(content string) string
	SetSelectors(fn SelectorFunc)
	RegisterDetector(d Detector)
	Detect(in DetectInput) string
	Detectors() []Detector
}
