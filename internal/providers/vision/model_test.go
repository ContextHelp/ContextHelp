package vision

import "testing"

func TestVisionProvidersReportModel(t *testing.T) {
	if got := NewOllamaProvider("", "qwen2.5vl:7b").Model(); got != "qwen2.5vl:7b" {
		t.Errorf("ollama model %q", got)
	}
	for name, p := range map[string]interface{ Model() string }{
		"anthropic":  NewAnthropicProvider("k", ""),
		"gemini":     NewGeminiProvider("k", ""),
		"openai":     NewOpenAIProvider("k", ""),
		"openrouter": NewOpenRouterProvider("k", ""),
	} {
		if p.Model() == "" {
			t.Errorf("%s: empty default model", name)
		}
	}
}
