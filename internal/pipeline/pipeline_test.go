package pipeline

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type noopStep struct {
	BaseContract
}

func (n *noopStep) Name() string { return "noop" }
func (n *noopStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	return draft, nil
}

func TestPipelineStepInterface(t *testing.T) {
	var s PipelineStep = &noopStep{}
	if s.Name() != "noop" {
		t.Errorf("Name: got %q", s.Name())
	}
	c := s.Contract()
	if c.Requires != nil || c.Produces != nil || c.Capabilities != nil {
		t.Errorf("noop contract should be empty: %+v", c)
	}
}

type mockRegistry struct {
	pipelines map[string]*Pipeline
}

func (m *mockRegistry) Register(name string, p *Pipeline) error {
	m.pipelines[name] = p
	return nil
}
func (m *mockRegistry) Get(name string) (*Pipeline, error) {
	p, ok := m.pipelines[name]
	if !ok {
		return nil, nil
	}
	return p, nil
}
func (m *mockRegistry) List() []string {
	var names []string
	for k := range m.pipelines {
		names = append(names, k)
	}
	return names
}
func (m *mockRegistry) SelectPipeline(content string) string {
	if len(content) < 500 {
		return "text.short"
	}
	return "text.long"
}
func (m *mockRegistry) Upsert(name string, p *Pipeline) { m.pipelines[name] = p }
func (m *mockRegistry) SetSelectors(_ SelectorFunc)     {}
func (m *mockRegistry) RegisterDetector(_ Detector)     {}
func (m *mockRegistry) Detect(in DetectInput) string    { return m.SelectPipeline(in.Source) }
func (m *mockRegistry) Detectors() []Detector           { return nil }

func TestRegistryInterface(t *testing.T) {
	var r Registry = &mockRegistry{pipelines: make(map[string]*Pipeline)}
	if r == nil {
		t.Fatal("mock should satisfy Registry")
	}
}
