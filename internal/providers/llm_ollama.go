package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/ideacrafterslabs/ctxt/internal/providers/vision"
)

// OllamaLLMProvider generates text using the Ollama /api/generate endpoint.
type OllamaLLMProvider struct {
	endpoint string
	model    string
	client   *http.Client
}

// ErrModelNotConfigured reports a local backend with no model to run.
// There is no silent default model: it comes from config.
var ErrModelNotConfigured = vision.ErrModelNotConfigured

// NewOllamaLLMProvider creates an Ollama LLM provider with the given
// endpoint and model. An empty model makes Generate fail with
// ErrModelNotConfigured.
func NewOllamaLLMProvider(endpoint, model string) *OllamaLLMProvider {
	if endpoint == "" {
		endpoint = defaultOllamaEndpoint
	}
	return &OllamaLLMProvider{endpoint: endpoint, model: model, client: &http.Client{}}
}

// WithHTTPClient returns a copy that sends requests through c.
func (p *OllamaLLMProvider) WithHTTPClient(c *http.Client) *OllamaLLMProvider {
	cp := *p
	cp.client = c
	return &cp
}

func (p *OllamaLLMProvider) Name() string { return backendOllama }

// Model is the model the provider calls; provenance on derived text records it.
func (p *OllamaLLMProvider) Model() string { return p.model }

func (p *OllamaLLMProvider) Generate(ctx context.Context, prompt string) (string, error) {
	if p.model == "" {
		return "", fmt.Errorf("ollama llm: %w (set providers.llm.model)", ErrModelNotConfigured)
	}
	reqBody := map[string]interface{}{
		"model":  p.model,
		"prompt": prompt,
		"stream": false,
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("ollama llm: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint+"/api/generate", bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama generate: %w", err)
	}
	defer resp.Body.Close()
	var result struct {
		Response string `json:"response"`
		Error    string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if result.Error != "" {
		return "", fmt.Errorf("ollama error: %s", result.Error)
	}
	return result.Response, nil
}
