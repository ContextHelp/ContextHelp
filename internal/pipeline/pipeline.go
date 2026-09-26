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

// SourceSelectorFunc routes by where content came from: URL patterns match a
// URL, extension rules match a file path. It reports ok=false when the source
// names nothing its rules know (a capture label, an unknown extension), so
// the content decides instead.
type SourceSelectorFunc func(source string) (pipelineName string, ok bool)

// ContentSelectorFunc routes by the content itself: length, markdown
// structure, payload shape. It always picks a pipeline.
type ContentSelectorFunc func(content string) string

// Selectors are the registry's fallback rules, split by the signal each one
// reads. Source rules never see the content; content rules never see the
// source.
type Selectors struct {
	Source  SourceSelectorFunc
	Content ContentSelectorFunc
}

// Registry manages named pipelines.
type Registry interface {
	Register(name string, p *Pipeline) error
	Upsert(name string, p *Pipeline)
	Get(name string) (*Pipeline, error)
	List() []string
	SelectPipeline(source, content string) string
	SetSelectors(sel Selectors)
	RegisterDetector(d Detector)
	Detect(in DetectInput) string
	Detectors() []Detector
}
