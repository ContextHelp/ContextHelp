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

// AnthropicProvider analyzes images using the Anthropic Messages API.
type AnthropicProvider struct {
	apiKey string
	model  string
	client *http.Client
}

// NewAnthropicProvider creates an Anthropic vision provider that authenticates with
// apiKey. An empty apiKey makes every request fail; the caller resolves the key.
func NewAnthropicProvider(apiKey, model string) *AnthropicProvider {
	if model == "" {
		model = "claude-3-5-sonnet-20241022"
	}
	return &AnthropicProvider{
		apiKey: apiKey,
		model:  model,
	}
}

// WithHTTPClient returns a copy of the provider that sends requests through c.
func (p *AnthropicProvider) WithHTTPClient(c *http.Client) *AnthropicProvider {
	cp := *p
	cp.client = c
	return &cp
}

func (p *AnthropicProvider) Name() string { return "anthropic" }

func (p *AnthropicProvider) Analyze(ctx context.Context, imageData []byte, contentType string) (*Result, error) {
	if p.apiKey == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY not set in the secrets store or environment")
	}

	b64 := base64.StdEncoding.EncodeToString(imageData)

	reqBody := map[string]interface{}{
		"model":      p.model,
		"max_tokens": 1024,
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": []map[string]interface{}{
					{
						"type": "image",
						"source": map[string]string{
							"type":       "base64",
							"media_type": contentType,
							"data":       b64,
						},
					},
					{"type": "text", "text": prompt},
				},
			},
		},
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("anthropic vision: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.anthropic.com/v1/messages", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("anthropic vision: create request: %w", err)
	}
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")

	resp, err := clientOr(p.client).Do(req)
	if err != nil {
		return nil, fmt.Errorf("anthropic vision: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("anthropic vision: HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("anthropic vision: decode response: %w", err)
	}
	if result.Error != nil {
		return nil, fmt.Errorf("anthropic vision: %s", result.Error.Message)
	}
	if len(result.Content) == 0 {
		return nil, fmt.Errorf("anthropic vision: no content returned")
	}

	description, labels := parseResponse(result.Content[0].Text)
	return &Result{
		Description: description,
		Labels:      labels,
		Confidence:  0.90,
	}, nil
}
