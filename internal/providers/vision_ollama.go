package providers

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

// OllamaVisionProvider uses Ollama's API with a multimodal model (e.g. llava).
type OllamaVisionProvider struct {
	endpoint string
	model    string
}

func NewOllamaVisionProvider(endpoint, model string) *OllamaVisionProvider {
	return &OllamaVisionProvider{endpoint: endpoint, model: model}
}

func (p *OllamaVisionProvider) Name() string { return "ollama" }

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

func (p *OllamaVisionProvider) Analyze(ctx context.Context, imageData []byte, contentType string) (*VisionResult, error) {
	b64 := base64.StdEncoding.EncodeToString(imageData)

	reqBody := ollamaGenerateRequest{
		Model:  p.model,
		Prompt: "Describe this image in detail. List the main objects, text, colors, and overall scene. Then list detected labels as a comma-separated list after 'Labels:'.",
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

	description, labels := parseVisionResponse(ollamaResp.Response)

	return &VisionResult{
		Description: description,
		Labels:      labels,
		Confidence:  0.85,
	}, nil
}

// parseVisionResponse splits the LLM response into description and labels.
func parseVisionResponse(response string) (string, []string) {
	lower := strings.ToLower(response)
	idx := strings.Index(lower, "labels:")
	if idx < 0 {
		return strings.TrimSpace(response), []string{}
	}

	description := strings.TrimSpace(response[:idx])
	labelsStr := strings.TrimSpace(response[idx+7:])

	var labels []string
	for _, l := range strings.Split(labelsStr, ",") {
		l = strings.TrimSpace(l)
		if l != "" {
			labels = append(labels, l)
		}
	}

	return description, labels
}
