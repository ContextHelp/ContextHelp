package pipeline

import (
	"fmt"
	"sort"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline/steps"
)

type registry struct {
	pipelines map[string]*Pipeline
}

// NewRegistry creates an empty pipeline registry.
func NewRegistry() Registry {
	return &registry{pipelines: make(map[string]*Pipeline)}
}

func (r *registry) Register(name string, p *Pipeline) error {
	if _, exists := r.pipelines[name]; exists {
		return fmt.Errorf("pipeline %q already registered", name)
	}
	r.pipelines[name] = p
	return nil
}

func (r *registry) Get(name string) (*Pipeline, error) {
	p, ok := r.pipelines[name]
	if !ok {
		return nil, fmt.Errorf("pipeline %q not found", name)
	}
	return p, nil
}

func (r *registry) List() []string {
	names := make([]string, 0, len(r.pipelines))
	for k := range r.pipelines {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

func (r *registry) SelectPipeline(content string) string {
	if len(content) < 500 {
		return "text.short"
	}
	return "text.long"
}

// DefaultRegistry returns a registry pre-loaded with built-in pipelines.
func DefaultRegistry() Registry {
	r := NewRegistry()

	r.Register("text.short", &Pipeline{
		PipelineName: "text.short",
		Description:  "Short text pipeline (< 500 chars)",
		Steps: []PipelineStep{
			steps.NewTypeDetector(),
			steps.NewTagger(),
		},
	})

	r.Register("text.long", &Pipeline{
		PipelineName: "text.long",
		Description:  "Long text pipeline (>= 500 chars)",
		Steps: []PipelineStep{
			steps.NewTypeDetector(),
			steps.NewSectioner(),
			steps.NewTagger(),
		},
	})

	return r
}
