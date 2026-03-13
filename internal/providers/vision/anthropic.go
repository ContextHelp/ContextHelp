package vision

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

// AnthropicProvider analyzes images using the Anthropic Messages API.
type AnthropicProvider struct {
	apiKey string
	model  string
}

func NewAnthropicProvider(model string) *AnthropicProvider {
	if model == "" {
		model = "claude-3-5-sonnet-20241022"
	}
	return &AnthropicProvider{
		apiKey: os.Getenv("ANTHROPIC_API_KEY"),
		model:  model,
	}
}

func (p *AnthropicProvider) Name() string { return "anthropic" }

func (p *AnthropicProvider) Analyze(ctx context.Context, imageData []byte, contentType string) (*Result, error) {
	if p.apiKey == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY not set")
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

	resp, err := http.DefaultClient.Do(req)
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
