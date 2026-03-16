package pipeline

import (
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// PipelineStep is a single transformation that enriches a draft.
// The canonical definition lives in pkg/pluginapi; this alias keeps all
// internal packages working without change.
type PipelineStep = pluginapi.PipelineStep

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
	Upsert(name string, p *Pipeline)
	Get(name string) (*Pipeline, error)
	List() []string
	SelectPipeline(content string) string
	SetSelectors(fn SelectorFunc)
	RegisterDetector(d Detector)
	Detect(in DetectInput) string
	Detectors() []Detector
}
