package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// OllamaEmbeddingProvider generates embeddings via Ollama's /api/embeddings endpoint.
type OllamaEmbeddingProvider struct {
	endpoint string
	model    string
	client   *http.Client
}

// NewOllamaEmbeddingProvider creates an Ollama embedding provider.
func NewOllamaEmbeddingProvider(endpoint, model string) *OllamaEmbeddingProvider {
	if endpoint == "" {
		endpoint = "http://localhost:11434"
	}
	if model == "" {
		model = "nomic-embed-text"
	}
	return &OllamaEmbeddingProvider{endpoint: endpoint, model: model, client: &http.Client{}}
}

func (p *OllamaEmbeddingProvider) Name() string    { return "ollama-embed" }
func (p *OllamaEmbeddingProvider) Dimensions() int { return 768 } // nomic-embed-text default

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
