package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// OllamaSummarizer summarizes with a local Ollama chat model through
// /api/chat. It is the local default of the summarization role. No model
// is baked in: the model comes from providers.summarization.model.
type OllamaSummarizer struct {
	endpoint string
	model    string
	prompt   SummaryPrompt
	numCtx   int
	client   *http.Client
}

// NewOllamaSummarizer creates the summarizer. An empty endpoint means the
// local Ollama default.
func NewOllamaSummarizer(endpoint, model string, prompt SummaryPrompt) *OllamaSummarizer {
	if endpoint == "" {
		endpoint = defaultOllamaEndpoint
	}
	return &OllamaSummarizer{endpoint: strings.TrimRight(endpoint, "/"), model: model, prompt: prompt, client: &http.Client{}}
}

// WithNumCtx returns a copy that asks Ollama for a context window of n
// tokens (its default is small enough to truncate a long transcript);
// n <= 0 leaves it to the model.
func (s *OllamaSummarizer) WithNumCtx(n int) *OllamaSummarizer {
	cp := *s
	cp.numCtx = n
	return &cp
}

// WithHTTPClient returns a copy that sends requests through c.
func (s *OllamaSummarizer) WithHTTPClient(c *http.Client) *OllamaSummarizer {
	cp := *s
	cp.client = c
	return &cp
}

func (s *OllamaSummarizer) Name() string { return backendOllama }

func (s *OllamaSummarizer) Summarize(ctx context.Context, req SummarizeRequest) (*SummaryResult, error) {
	prompt, err := s.prompt.Render(req)
	if err != nil {
		return nil, err
	}
	if s.model == "" {
		return nil, ErrNoSummaryModel
	}
	payload := map[string]any{
		"model":    s.model,
		"stream":   false,
		"messages": []map[string]string{{"role": "user", "content": prompt}},
	}
	if s.numCtx > 0 {
		payload["options"] = map[string]any{"num_ctx": s.numCtx}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("ollama summarization: marshal request: %w", err)
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	hreq.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(hreq)
	if err != nil {
		return nil, fmt.Errorf("ollama summarization: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ollama summarization: read response: %w", err)
	}
	var out struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("ollama summarization: status %d: decode response: %w", resp.StatusCode, err)
	}
	if out.Error != "" {
		return nil, fmt.Errorf("ollama summarization: %s", out.Error)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama summarization: status %d", resp.StatusCode)
	}
	text := strings.TrimSpace(out.Message.Content)
	if text == "" {
		return nil, fmt.Errorf("ollama summarization: empty summary")
	}
	return &SummaryResult{Text: text, Provider: backendOllama, Model: s.model}, nil
}
