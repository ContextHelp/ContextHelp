package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"unicode/utf8"
)

// ollamaSpecialTokenReserve is the part of the context kept free for the
// special tokens a tokenizer adds around the input ([CLS]/[SEP], <s>/</s>,
// a leading word marker).
const ollamaSpecialTokenReserve = 8

// OllamaEmbeddingProvider generates embeddings via Ollama's /api/embed
// endpoint.
//
// It applies no defaults of its own: endpoint and model come from the
// embedding provider resolver (internal/embeddings), which owns defaults
// and precedence.
//
// Every request runs the model at its full context. Ollama otherwise
// embeds with its own defaults (num_ctx 4096, num_batch 2048), and an
// embedding model must fit its whole input in one batch, so longer inputs
// fail with "the input length exceeds the context length". The context
// length comes from WithOllamaEmbeddingContextLength or, when unset, from
// the model's /api/show metadata, read once per provider.
//
// Input is cut, on a rune boundary, to inputByteBudget(context length)
// bytes. A token spans at least one byte, so the cut input always fits and
// the request sets truncate false: an input that still overflows fails
// instead of being cut silently by Ollama.
type OllamaEmbeddingProvider struct {
	endpoint  string
	model     string
	dimension int
	client    *http.Client

	mu         sync.Mutex
	contextLen int // fixed by option or discovered; 0 until known
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

// WithOllamaEmbeddingContextLength fixes the context length, in tokens,
// used for num_ctx, num_batch and the input byte budget. It takes
// precedence over the model's /api/show metadata, which is then never
// read. n <= 0 leaves discovery on.
func WithOllamaEmbeddingContextLength(n int) OllamaEmbeddingOption {
	return func(p *OllamaEmbeddingProvider) {
		if n > 0 {
			p.contextLen = n
		}
	}
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

// inputByteBudget is the most input bytes sent for a model with the given
// context length in tokens: the context minus a reserve for special
// tokens, never below one byte.
func inputByteBudget(contextLen int) int {
	return max(contextLen-ollamaSpecialTokenReserve, 1)
}

// truncateUTF8 returns the longest prefix of s that is at most n bytes and
// ends on a rune boundary, so it never splits a UTF-8 sequence.
func truncateUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 0 {
		return ""
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

type ollamaEmbedOptions struct {
	NumCtx   int `json:"num_ctx"`
	NumBatch int `json:"num_batch"`
}

type ollamaEmbedRequest struct {
	Model    string             `json:"model"`
	Input    string             `json:"input"`
	Truncate bool               `json:"truncate"`
	Options  ollamaEmbedOptions `json:"options"`
}

// Embed calls Ollama's /api/embed and returns a float32 vector.
func (p *OllamaEmbeddingProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	n, err := p.contextLength(ctx)
	if err != nil {
		return nil, err
	}
	if budget := inputByteBudget(n); len(text) > budget {
		slog.Debug("ollama embed: input cut to the model context",
			"model", p.model, "bytes", len(text), "budget", budget, "context_length", n)
		text = truncateUTF8(text, budget)
	}
	data, err := json.Marshal(ollamaEmbedRequest{
		Model:    p.model,
		Input:    text,
		Truncate: false,
		Options:  ollamaEmbedOptions{NumCtx: n, NumBatch: n},
	})
	if err != nil {
		return nil, fmt.Errorf("ollama embed encode: %w", err)
	}
	var result struct {
		Embeddings [][]float64 `json:"embeddings"`
	}
	if err := p.post(ctx, "/api/embed", data, &result); err != nil {
		return nil, fmt.Errorf("ollama embed (model %s): %w", p.model, err)
	}
	if len(result.Embeddings) != 1 {
		return nil, fmt.Errorf("ollama embed (model %s): got %d embeddings for one input", p.model, len(result.Embeddings))
	}
	out := make([]float32, len(result.Embeddings[0]))
	for i, v := range result.Embeddings[0] {
		out[i] = float32(v)
	}
	return out, nil
}

// contextLength returns the fixed context length, or reads it from the
// model's /api/show metadata. Only a successful read is kept, so a
// transient failure is retried on the next call.
func (p *OllamaEmbeddingProvider) contextLength(ctx context.Context) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.contextLen > 0 {
		return p.contextLen, nil
	}
	data, err := json.Marshal(map[string]string{"model": p.model})
	if err != nil {
		return 0, fmt.Errorf("ollama show encode: %w", err)
	}
	var show struct {
		ModelInfo map[string]any `json:"model_info"`
	}
	if err := p.post(ctx, "/api/show", data, &show); err != nil {
		return 0, fmt.Errorf("ollama show (model %s): %w", p.model, err)
	}
	n, ok := modelContextLength(show.ModelInfo)
	if !ok {
		return 0, fmt.Errorf("ollama show (model %s): model_info has no context_length", p.model)
	}
	p.contextLen = n
	return n, nil
}

// modelContextLength reads "<architecture>.context_length" from /api/show
// model_info, falling back to the only "*.context_length" key present.
func modelContextLength(info map[string]any) (int, bool) {
	if arch, ok := info["general.architecture"].(string); ok {
		if n, ok := positiveInt(info[arch+".context_length"]); ok {
			return n, true
		}
	}
	found, n := 0, 0
	for k, v := range info {
		if strings.HasSuffix(k, ".context_length") {
			if m, ok := positiveInt(v); ok {
				found, n = found+1, m
			}
		}
	}
	if found != 1 {
		return 0, false
	}
	return n, true
}

func positiveInt(v any) (int, bool) {
	f, ok := v.(float64)
	if !ok || f < 1 || f != float64(int(f)) {
		return 0, false
	}
	return int(f), true
}

// post sends a JSON body to path and decodes a 200 response into out. An
// error names Ollama's own message when the body carries one.
func (p *OllamaEmbeddingProvider) post(ctx context.Context, path string, body []byte, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	var apiErr struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(raw, &apiErr) == nil && apiErr.Error != "" {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, apiErr.Error)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

var _ EmbeddingProvider = (*OllamaEmbeddingProvider)(nil)
