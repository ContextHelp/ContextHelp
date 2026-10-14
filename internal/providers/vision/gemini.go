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

// GeminiProvider analyzes images using the Google Gemini API.
type GeminiProvider struct {
	apiKey string
	model  string
	client *http.Client
}

// NewGeminiProvider creates an Gemini vision provider that authenticates with
// apiKey. An empty apiKey makes every request fail; the caller resolves the key.
func NewGeminiProvider(apiKey, model string) *GeminiProvider {
	if model == "" {
		model = "gemini-2.0-flash"
	}
	return &GeminiProvider{
		apiKey: apiKey,
		model:  model,
	}
}

// WithHTTPClient returns a copy of the provider that sends requests through c.
func (p *GeminiProvider) WithHTTPClient(c *http.Client) *GeminiProvider {
	cp := *p
	cp.client = c
	return &cp
}

func (p *GeminiProvider) Name() string { return "gemini" }

func (p *GeminiProvider) Analyze(ctx context.Context, imageData []byte, contentType string) (*Result, error) {
	if p.apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY not set in the secrets store or environment")
	}

	b64 := base64.StdEncoding.EncodeToString(imageData)

	reqBody := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"parts": []map[string]interface{}{
					{"text": prompt},
					{
						"inline_data": map[string]string{
							"mime_type": contentType,
							"data":      b64,
						},
					},
				},
			},
		},
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("gemini vision: marshal request: %w", err)
	}

	url := "https://generativelanguage.googleapis.com/v1beta/models/" + p.model + ":generateContent?key=" + p.apiKey
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("gemini vision: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := clientOr(p.client).Do(req)
	if err != nil {
		return nil, fmt.Errorf("gemini vision: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gemini vision: HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("gemini vision: decode response: %w", err)
	}
	if result.Error != nil {
		return nil, fmt.Errorf("gemini vision: %s", result.Error.Message)
	}
	if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("gemini vision: no content returned")
	}

	description, labels := parseResponse(result.Candidates[0].Content.Parts[0].Text)
	return &Result{
		Description: description,
		Labels:      labels,
		Confidence:  0.90,
	}, nil
}
