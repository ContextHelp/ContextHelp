package pageshape

import (
	"context"
	"errors"
	"testing"

	lateralpageshape "github.com/ideacrafterslabs/ctxt/internal/lateral/pageshape"
)

type stubHeuristic struct{ labels []lateralpageshape.Label }

func (s *stubHeuristic) Classify(_ lateralpageshape.Input) []lateralpageshape.Label {
	return s.labels
}

type stubLLM struct {
	labels []lateralpageshape.Label
	err    error
	called int
}

func (s *stubLLM) Classify(_ context.Context, _ lateralpageshape.Input) ([]lateralpageshape.Label, error) {
	s.called++
	return s.labels, s.err
}

func TestClassify_PrefersHeuristic(t *testing.T) {
	h := &stubHeuristic{labels: []lateralpageshape.Label{lateralpageshape.LabelAboutPage}}
	llm := &stubLLM{labels: []lateralpageshape.Label{lateralpageshape.LabelBlog}}
	c := New(h, llm)
	got, err := c.Classify(context.Background(), "https://x.example/about")
	if err != nil {
		t.Fatalf("Classify err = %v", err)
	}
	if got != string(lateralpageshape.LabelAboutPage) {
		t.Errorf("got = %q; want %q", got, lateralpageshape.LabelAboutPage)
	}
	if llm.called != 0 {
		t.Errorf("LLM called %d times; want 0 (heuristic short-circuit)", llm.called)
	}
}

func TestClassify_FallsBackToLLM(t *testing.T) {
	h := &stubHeuristic{labels: nil}
	llm := &stubLLM{labels: []lateralpageshape.Label{lateralpageshape.LabelResearchPaper}}
	c := New(h, llm)
	got, err := c.Classify(context.Background(), "https://arxiv.org/abs/1234.5678")
	if err != nil {
		t.Fatalf("Classify err = %v", err)
	}
	if got != string(lateralpageshape.LabelResearchPaper) {
		t.Errorf("got = %q", got)
	}
	if llm.called != 1 {
		t.Errorf("LLM called %d; want 1", llm.called)
	}
}

func TestClassify_BothEmptyReturnsEmpty(t *testing.T) {
	c := New(&stubHeuristic{}, &stubLLM{})
	got, err := c.Classify(context.Background(), "https://x.example/")
	if err != nil {
		t.Fatalf("Classify err = %v", err)
	}
	if got != "" {
		t.Errorf("got = %q; want empty", got)
	}
}

func TestClassify_NilLayersReturnEmpty(t *testing.T) {
	c := New(nil, nil)
	got, err := c.Classify(context.Background(), "https://x.example/")
	if err != nil {
		t.Fatalf("Classify err = %v", err)
	}
	if got != "" {
		t.Errorf("got = %q; want empty", got)
	}
}

func TestClassify_LLMErrorPropagates(t *testing.T) {
	want := errors.New("llm boom")
	c := New(&stubHeuristic{}, &stubLLM{err: want})
	_, err := c.Classify(context.Background(), "https://x.example/")
	if !errors.Is(err, want) {
		t.Errorf("err = %v; want wraps %v", err, want)
	}
}

func TestClassify_FirstLabelWinsOnMultiHit(t *testing.T) {
	h := &stubHeuristic{labels: []lateralpageshape.Label{
		lateralpageshape.LabelProductPage, lateralpageshape.LabelEcommerceStore,
	}}
	c := New(h, nil)
	got, err := c.Classify(context.Background(), "https://x.example/products/foo")
	if err != nil {
		t.Fatalf("Classify err = %v", err)
	}
	if got != string(lateralpageshape.LabelProductPage) {
		t.Errorf("got = %q; want %q (first wins)", got, lateralpageshape.LabelProductPage)
	}
}

// Real heuristic integration check — confirms the adapter's
// HeuristicLayer alias matches the actual *pageshape.Heuristic shape.
func TestClassify_RealHeuristicIntegration(t *testing.T) {
	c := New(lateralpageshape.NewHeuristic(), nil)
	got, err := c.Classify(context.Background(), "https://x.example/about")
	if err != nil {
		t.Fatalf("Classify err = %v", err)
	}
	if got != string(lateralpageshape.LabelAboutPage) {
		t.Errorf("got = %q; want AboutPage", got)
	}
}
