package vision

import "net/http"

// OpenAIProvider analyzes images using the OpenAI chat completions API.
type OpenAIProvider struct{ chatProvider }

// NewOpenAIProvider creates an OpenAI vision provider that authenticates with
// apiKey. An empty apiKey makes every request fail; the caller resolves the key.
func NewOpenAIProvider(apiKey, model string) *OpenAIProvider {
	if model == "" {
		model = "gpt-4o"
	}
	return &OpenAIProvider{chatProvider{
		api: chatCompletionsAPI{
			name:    "openai",
			url:     "https://api.openai.com/v1/chat/completions",
			keyName: "OPENAI_API_KEY",
			label:   "openai vision",
		},
		apiKey: apiKey,
		model:  model,
	}}
}

// WithHTTPClient returns a copy of the provider that sends requests through c.
func (p *OpenAIProvider) WithHTTPClient(c *http.Client) *OpenAIProvider {
	cp := *p
	cp.client = c
	return &cp
}
