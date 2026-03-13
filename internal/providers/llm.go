package providers

import "context"

// LLMProvider generates text from a prompt.
type LLMProvider interface {
	Generate(ctx context.Context, prompt string) (string, error)
	Name() string
}

// NewStubLLMProvider returns a stub that returns a canned response.
func NewStubLLMProvider() LLMProvider { return &stubLLMProvider{} }

type stubLLMProvider struct{}

func (s *stubLLMProvider) Name() string { return "stub" }
func (s *stubLLMProvider) Generate(_ context.Context, prompt string) (string, error) {
	end := len(prompt)
	if end > 50 {
		end = 50
	}
	return "[stub response for: " + prompt[:end] + "...]", nil
}
