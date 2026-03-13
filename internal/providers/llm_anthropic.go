package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)

// AnthropicLLMProvider generates text using the Anthropic Messages API.
type AnthropicLLMProvider struct {
	apiKey string
	model  string
	client *http.Client
}

// NewAnthropicLLMProvider creates an Anthropic LLM provider. API key is read from
// the ANTHROPIC_API_KEY environment variable.
func NewAnthropicLLMProvider(model string) *AnthropicLLMProvider {
	if model == "" {
		model = "claude-haiku-4-5-20251001"
	}
	return &AnthropicLLMProvider{
		apiKey: os.Getenv("ANTHROPIC_API_KEY"),
		model:  model,
		client: &http.Client{},
	}
}

func (p *AnthropicLLMProvider) Name() string { return "anthropic" }

func (p *AnthropicLLMProvider) Generate(ctx context.Context, prompt string) (string, error) {
	if p.apiKey == "" {
		return "", fmt.Errorf("ANTHROPIC_API_KEY not set")
	}
	reqBody := map[string]interface{}{
		"model":      p.model,
		"max_tokens": 1024,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("anthropic llm: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.anthropic.com/v1/messages", bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("anthropic request: %w", err)
	}
	defer resp.Body.Close()
	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if result.Error != nil {
		return "", fmt.Errorf("anthropic error: %s", result.Error.Message)
	}
	if len(result.Content) == 0 {
		return "", fmt.Errorf("no content returned")
	}
	return result.Content[0].Text, nil
}
