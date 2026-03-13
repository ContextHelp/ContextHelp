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

// OpenAIProvider analyzes images using the OpenAI chat completions API.
type OpenAIProvider struct {
	apiKey string
	model  string
}

func NewOpenAIProvider(model string) *OpenAIProvider {
	if model == "" {
		model = "gpt-4o"
	}
	return &OpenAIProvider{
		apiKey: os.Getenv("OPENAI_API_KEY"),
		model:  model,
	}
}

func (p *OpenAIProvider) Name() string { return "openai" }

func (p *OpenAIProvider) Analyze(ctx context.Context, imageData []byte, contentType string) (*Result, error) {
	if p.apiKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY not set")
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
		return nil, fmt.Errorf("openai vision: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.openai.com/v1/chat/completions", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("openai vision: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai vision: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai vision: HTTP %d: %s", resp.StatusCode, string(body))
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
		return nil, fmt.Errorf("openai vision: decode response: %w", err)
	}
	if result.Error != nil {
		return nil, fmt.Errorf("openai vision: %s", result.Error.Message)
	}
	if len(result.Choices) == 0 {
		return nil, fmt.Errorf("openai vision: no choices returned")
	}

	description, labels := parseResponse(result.Choices[0].Message.Content)
	return &Result{
		Description: description,
		Labels:      labels,
		Confidence:  0.90,
	}, nil
}
