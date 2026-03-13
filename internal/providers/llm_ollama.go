package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// OllamaLLMProvider generates text using the Ollama /api/generate endpoint.
type OllamaLLMProvider struct {
	endpoint string
	model    string
	client   *http.Client
}

// NewOllamaLLMProvider creates an Ollama LLM provider with the given endpoint and model.
func NewOllamaLLMProvider(endpoint, model string) *OllamaLLMProvider {
	if endpoint == "" {
		endpoint = "http://localhost:11434"
	}
	if model == "" {
		model = "llama3"
	}
	return &OllamaLLMProvider{endpoint: endpoint, model: model, client: &http.Client{}}
}

func (p *OllamaLLMProvider) Name() string { return "ollama" }

func (p *OllamaLLMProvider) Generate(ctx context.Context, prompt string) (string, error) {
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
