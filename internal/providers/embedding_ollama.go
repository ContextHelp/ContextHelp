package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// OllamaEmbeddingProvider generates embeddings via Ollama's /api/embeddings endpoint.
//
// It applies no defaults of its own: endpoint and model come from the
// embedding provider resolver (internal/embeddings), which owns defaults
// and precedence.
type OllamaEmbeddingProvider struct {
	endpoint  string
	model     string
	dimension int
	client    *http.Client
}

// OllamaEmbeddingOption configures an OllamaEmbeddingProvider.
type OllamaEmbeddingOption func(*OllamaEmbeddingProvider)

// WithOllamaEmbeddingHTTPClient sets the HTTP client used for embed calls.
func WithOllamaEmbeddingHTTPClient(c *http.Client) OllamaEmbeddingOption {
	return func(p *OllamaEmbeddingProvider) {
		if c != nil {
			p.client = c
		}
	}
}

// WithOllamaEmbeddingDimension records the expected vector dimension
// reported by Dimensions. Without it Dimensions reports 0 (unknown).
func WithOllamaEmbeddingDimension(n int) OllamaEmbeddingOption {
	return func(p *OllamaEmbeddingProvider) { p.dimension = n }
}

// NewOllamaEmbeddingProvider creates an Ollama embedding provider for the
// given base endpoint (e.g. "http://localhost:11434") and model.
func NewOllamaEmbeddingProvider(endpoint, model string, opts ...OllamaEmbeddingOption) *OllamaEmbeddingProvider {
	p := &OllamaEmbeddingProvider{
		endpoint: strings.TrimRight(endpoint, "/"),
		model:    model,
		client:   &http.Client{},
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

func (p *OllamaEmbeddingProvider) Name() string { return "ollama-embed" }

// Dimensions reports the configured vector dimension; 0 means unknown.
func (p *OllamaEmbeddingProvider) Dimensions() int { return p.dimension }

// Embed calls the Ollama embedding API and returns a float32 vector.
func (p *OllamaEmbeddingProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	if len(text) > 8192 {
		text = text[:8192]
	}
	reqBody := map[string]interface{}{
		"model":  p.model,
		"prompt": text,
	}
	data, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, "POST", p.endpoint+"/api/embeddings", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama embed: %w", err)
	}
	defer resp.Body.Close()
	var result struct {
		Embedding []float64 `json:"embedding"`
		Error     string    `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ollama embed decode: %w", err)
	}
	if result.Error != "" {
		return nil, fmt.Errorf("ollama embed error: %s", result.Error)
	}
	out := make([]float32, len(result.Embedding))
	for i, v := range result.Embedding {
		out[i] = float32(v)
	}
	return out, nil
}
