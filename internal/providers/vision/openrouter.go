package vision

import "net/http"

// OpenRouterProvider analyzes images using the OpenRouter API (OpenAI-compatible).
type OpenRouterProvider struct{ chatProvider }

// NewOpenRouterProvider creates an OpenRouter vision provider that authenticates with
// apiKey. An empty apiKey makes every request fail; the caller resolves the key.
func NewOpenRouterProvider(apiKey, model string) *OpenRouterProvider {
	if model == "" {
		model = "google/gemini-2.0-flash-lite"
	}
	return &OpenRouterProvider{chatProvider{
		api: chatCompletionsAPI{
			name:    "openrouter",
			url:     "https://openrouter.ai/api/v1/chat/completions",
			keyName: "OPENROUTER_API_KEY",
			label:   "openrouter vision",
		},
		apiKey: apiKey,
		model:  model,
	}}
}

// WithHTTPClient returns a copy of the provider that sends requests through c.
func (p *OpenRouterProvider) WithHTTPClient(c *http.Client) *OpenRouterProvider {
	cp := *p
	cp.client = c
	return &cp
}
