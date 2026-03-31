package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)

// OpenAILLMProvider generates text using the OpenAI chat completions API.
type OpenAILLMProvider struct {
	apiKey string
	model  string
	client *http.Client
}

// NewOpenAILLMProvider creates an OpenAI LLM provider. API key is read from
// the OPENAI_API_KEY environment variable.
func NewOpenAILLMProvider(model string) *OpenAILLMProvider {
	if model == "" {
		model = "gpt-4o-mini"
	}
	return &OpenAILLMProvider{
		apiKey: os.Getenv("OPENAI_API_KEY"),
		model:  model,
		client: &http.Client{},
	}
}

// WithHTTPClient returns a copy of the provider using the given HTTP client.
// Used in tests to inject a VCR-backed client.
func (p *OpenAILLMProvider) WithHTTPClient(c *http.Client) *OpenAILLMProvider {
	copy := *p
	copy.client = c
	return &copy
}

func (p *OpenAILLMProvider) Name() string { return "openai" }

func (p *OpenAILLMProvider) Generate(ctx context.Context, prompt string) (string, error) {
	if p.apiKey == "" {
		return "", fmt.Errorf("OPENAI_API_KEY not set")
	}
	reqBody := map[string]interface{}{
		"model": p.model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("openai llm: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.openai.com/v1/chat/completions", bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("openai request: %w", err)
	}
	defer resp.Body.Close()
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if result.Error != nil {
		return "", fmt.Errorf("openai error: %s", result.Error.Message)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("no choices returned")
	}
	return result.Choices[0].Message.Content, nil
}
