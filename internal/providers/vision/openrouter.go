package vision

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// OpenRouterProvider analyzes images using the OpenRouter API (OpenAI-compatible).
type OpenRouterProvider struct {
	apiKey string
	model  string
	client *http.Client
}

// NewOpenRouterProvider creates an OpenRouter vision provider that authenticates with
// apiKey. An empty apiKey makes every request fail; the caller resolves the key.
func NewOpenRouterProvider(apiKey, model string) *OpenRouterProvider {
	if model == "" {
		model = "google/gemini-2.0-flash-lite"
	}
	return &OpenRouterProvider{
		apiKey: apiKey,
		model:  model,
	}
}

// WithHTTPClient returns a copy of the provider that sends requests through c.
func (p *OpenRouterProvider) WithHTTPClient(c *http.Client) *OpenRouterProvider {
	cp := *p
	cp.client = c
	return &cp
}

func (p *OpenRouterProvider) Name() string { return "openrouter" }

func (p *OpenRouterProvider) Analyze(ctx context.Context, imageData []byte, contentType string) (*Result, error) {
	if p.apiKey == "" {
		return nil, fmt.Errorf("OPENROUTER_API_KEY not set in the secrets store or environment")
	}

	b64 := base64.StdEncoding.EncodeToString(imageData)
	dataURL := "data:" + contentType + ";base64," + b64

	reqBody := map[string]interface{}{
		"model": p.model,
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": []map[string]interface{}{
					{"type": "text", "text": prompt},
					{"type": "image_url", "image_url": map[string]string{"url": dataURL}},
				},
			},
		},
		"max_tokens": 1024,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("openrouter vision: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://openrouter.ai/api/v1/chat/completions", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("openrouter vision: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := clientOr(p.client).Do(req)
	if err != nil {
		return nil, fmt.Errorf("openrouter vision: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openrouter vision: HTTP %d: %s", resp.StatusCode, string(body))
	}

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
		return nil, fmt.Errorf("openrouter vision: decode response: %w", err)
	}
	if result.Error != nil {
		return nil, fmt.Errorf("openrouter vision: %s", result.Error.Message)
	}
	if len(result.Choices) == 0 {
		return nil, fmt.Errorf("openrouter vision: no choices returned")
	}

	description, labels := parseResponse(result.Choices[0].Message.Content)
	return &Result{
		Description: description,
		Labels:      labels,
		Confidence:  0.90,
	}, nil
}
