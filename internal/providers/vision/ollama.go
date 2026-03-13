package vision

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// OllamaProvider uses Ollama's API with a multimodal model (e.g. llava).
type OllamaProvider struct {
	endpoint string
	model    string
}

func NewOllamaProvider(endpoint, model string) *OllamaProvider {
	return &OllamaProvider{endpoint: endpoint, model: model}
}

func (p *OllamaProvider) Name() string { return "ollama" }

// ollamaGenerateRequest is the request body for Ollama's /api/generate endpoint.
type ollamaGenerateRequest struct {
	Model  string   `json:"model"`
	Prompt string   `json:"prompt"`
	Images []string `json:"images,omitempty"`
	Stream bool     `json:"stream"`
}

// ollamaGenerateResponse is the response from /api/generate.
type ollamaGenerateResponse struct {
	Response string `json:"response"`
	Done     bool   `json:"done"`
}

func (p *OllamaProvider) Analyze(ctx context.Context, imageData []byte, contentType string) (*Result, error) {
	b64 := base64.StdEncoding.EncodeToString(imageData)

	reqBody := ollamaGenerateRequest{
		Model:  p.model,
		Prompt: prompt,
		Images: []string{b64},
		Stream: false,
	}

	bodyJSON, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("ollama vision: marshal request: %w", err)
	}

	url := strings.TrimRight(p.endpoint, "/") + "/api/generate"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("ollama vision: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama vision: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama vision: HTTP %d: %s", resp.StatusCode, string(body))
	}

	var ollamaResp ollamaGenerateResponse
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		return nil, fmt.Errorf("ollama vision: decode response: %w", err)
	}

	description, labels := parseResponse(ollamaResp.Response)

	return &Result{
		Description: description,
		Labels:      labels,
		Confidence:  0.85,
	}, nil
}
