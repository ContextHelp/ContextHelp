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

// chatCompletionsAPI is an OpenAI-compatible chat completions endpoint
// that takes an image as a data-URL content part.
type chatCompletionsAPI struct {
	name    string // provider name, as Name reports it
	url     string // POST target
	keyName string // the API key's name, for the missing-key error
	label   string // error prefix, e.g. "openai vision"
}

// chatProvider is the vision provider over a chatCompletionsAPI; the
// OpenAI and OpenRouter providers embed it.
type chatProvider struct {
	api    chatCompletionsAPI
	apiKey string
	model  string
	client *http.Client
}

func (p *chatProvider) Name() string { return p.api.name }

func (p *chatProvider) Analyze(ctx context.Context, imageData []byte, contentType string) (*Result, error) {
	return p.api.analyze(ctx, p.client, p.apiKey, p.model, imageData, contentType)
}

// analyze sends imageData with the shared prompt to api as model,
// authenticating with apiKey, and parses the first choice.
func (api chatCompletionsAPI) analyze(ctx context.Context, client *http.Client, apiKey, model string, imageData []byte, contentType string) (*Result, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("%s not set in the secrets store or environment", api.keyName)
	}

	b64 := base64.StdEncoding.EncodeToString(imageData)
	dataURL := "data:" + contentType + ";base64," + b64

	reqBody := map[string]interface{}{
		"model": model,
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
		return nil, fmt.Errorf("%s: marshal request: %w", api.label, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, api.url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%s: create request: %w", api.label, err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := clientOr(client).Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", api.label, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%s: HTTP %d: %s", api.label, resp.StatusCode, string(body))
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
		return nil, fmt.Errorf("%s: decode response: %w", api.label, err)
	}
	if result.Error != nil {
		return nil, fmt.Errorf("%s: %s", api.label, result.Error.Message)
	}
	if len(result.Choices) == 0 {
		return nil, fmt.Errorf("%s: no choices returned", api.label)
	}

	description, labels := parseResponse(result.Choices[0].Message.Content)
	return &Result{
		Description: description,
		Labels:      labels,
		Confidence:  0.90,
	}, nil
}
