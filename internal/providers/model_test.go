package providers

import "testing"

// Providers that know their model report it, so derived text can record it.
func TestLLMProvidersReportModel(t *testing.T) {
	for name, p := range map[string]interface{ Model() string }{
		"ollama":    NewOllamaLLMProvider("", "qwen3:4b"),
		"openai":    NewOpenAILLMProvider("k", "gpt-x"),
		"anthropic": NewAnthropicLLMProvider("k", "claude-x"),
	} {
		if p.Model() == "" {
			t.Errorf("%s: empty model", name)
		}
	}
	if got := NewOllamaLLMProvider("", "qwen3:4b").Model(); got != "qwen3:4b" {
		t.Errorf("model %q", got)
	}
}
