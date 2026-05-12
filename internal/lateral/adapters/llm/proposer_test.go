package llm

import (
	"context"
	"errors"
	"strings"
	"testing"

	kitllm "hop.top/kit/go/ai/llm"
)

// stubCompleter records the last Request it received and returns a
// pre-canned Response. Sufficient for adapter-shape tests; production
// uses real kit/ai/llm.Client adapters (anthropic/openai/etc.) wired
// through the Completer interface.
type stubCompleter struct {
	last kitllm.Request
	resp kitllm.Response
	err  error
}

func (s *stubCompleter) Complete(_ context.Context, req kitllm.Request) (kitllm.Response, error) {
	s.last = req
	return s.resp, s.err
}

func TestProposer_PropagatesPromptAndParsesResponse(t *testing.T) {
	stub := &stubCompleter{
		resp: kitllm.Response{Content: "/about\n/team\n# comment\n/blog"},
	}
	p := NewProposer(stub, Options{Model: "test-model"})
	got, err := p.Propose(context.Background(), "blog.acme.io", "Blog")
	if err != nil {
		t.Fatalf("Propose err = %v", err)
	}
	want := []string{"/about", "/team", "/blog"}
	if !strSlicesEqual(got, want) {
		t.Errorf("paths = %v; want %v", got, want)
	}
	// Prompt must contain both inputs (jit.BuildPrompt contract is
	// covered in the jit package; this just confirms the adapter
	// passed the inputs through).
	prompt := stub.last.Messages[0].Content
	if !strings.Contains(prompt, "blog.acme.io") || !strings.Contains(prompt, "Blog") {
		t.Errorf("prompt missing inputs:\n%s", prompt)
	}
	if stub.last.Model != "test-model" {
		t.Errorf("Model = %q; want test-model", stub.last.Model)
	}
}

func TestProposer_AppliesDefaults(t *testing.T) {
	stub := &stubCompleter{resp: kitllm.Response{Content: "/x"}}
	p := NewProposer(stub, Options{})
	if _, err := p.Propose(context.Background(), "x.example", ""); err != nil {
		t.Fatalf("Propose err = %v", err)
	}
	if stub.last.Model != defaultModel {
		t.Errorf("Model = %q; want %q", stub.last.Model, defaultModel)
	}
	if stub.last.Temperature != defaultTemperature {
		t.Errorf("Temperature = %v; want %v", stub.last.Temperature, defaultTemperature)
	}
	if stub.last.MaxTokens != defaultMaxTokens {
		t.Errorf("MaxTokens = %v; want %v", stub.last.MaxTokens, defaultMaxTokens)
	}
}

func TestProposer_LLMErrorPropagates(t *testing.T) {
	want := errors.New("model exploded")
	stub := &stubCompleter{err: want}
	p := NewProposer(stub, Options{})
	_, err := p.Propose(context.Background(), "x.example", "")
	if !errors.Is(err, want) {
		t.Fatalf("err = %v; want wraps %v", err, want)
	}
}

func TestProposer_EmptyResponseIsNotAnError(t *testing.T) {
	stub := &stubCompleter{resp: kitllm.Response{Content: ""}}
	p := NewProposer(stub, Options{})
	got, err := p.Propose(context.Background(), "x.example", "")
	if err != nil {
		t.Fatalf("Propose err = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("paths = %v; want empty", got)
	}
}

func TestNewProposer_PanicsOnNilLLM(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on nil llm")
		}
	}()
	_ = NewProposer(nil, Options{})
}

// PageTypeClassifier tests.
func TestPageTypeClassifier_ReturnsFirstNonEmptyLine(t *testing.T) {
	stub := &stubCompleter{resp: kitllm.Response{Content: "\n  BlogPost\nignored"}}
	c := NewPageTypeClassifier(stub, Options{})
	got, err := c.Classify(context.Background(), "https://x.example/post/1")
	if err != nil {
		t.Fatalf("Classify err = %v", err)
	}
	if got != "BlogPost" {
		t.Errorf("got = %q; want BlogPost", got)
	}
	prompt := stub.last.Messages[0].Content
	if !strings.Contains(prompt, "https://x.example/post/1") {
		t.Errorf("prompt missing URL:\n%s", prompt)
	}
}

func TestPageTypeClassifier_EmptyResponseReturnsEmptyString(t *testing.T) {
	stub := &stubCompleter{resp: kitllm.Response{Content: "   \n\n"}}
	c := NewPageTypeClassifier(stub, Options{})
	got, err := c.Classify(context.Background(), "https://x.example/")
	if err != nil {
		t.Fatalf("Classify err = %v", err)
	}
	if got != "" {
		t.Errorf("got = %q; want empty", got)
	}
}

func TestPageTypeClassifier_ErrorPropagates(t *testing.T) {
	stub := &stubCompleter{err: errors.New("boom")}
	c := NewPageTypeClassifier(stub, Options{})
	_, err := c.Classify(context.Background(), "https://x.example/")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestNewPageTypeClassifier_PanicsOnNilLLM(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on nil llm")
		}
	}()
	_ = NewPageTypeClassifier(nil, Options{})
}

func strSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
