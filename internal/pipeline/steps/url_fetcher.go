package steps

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// URLFetcher fetches the raw content of a URL.
type URLFetcher struct {
	pipeline.BaseContract
	client *http.Client
}

// URLFetcherOption configures a URLFetcher.
type URLFetcherOption func(*URLFetcher)

// WithURLHTTPClient sets a custom HTTP client for the fetcher.
func WithURLHTTPClient(c *http.Client) URLFetcherOption {
	return func(f *URLFetcher) { f.client = c }
}

// NewURLFetcher creates a URLFetcher with optional configuration.
func NewURLFetcher(opts ...URLFetcherOption) *URLFetcher {
	f := &URLFetcher{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"Source"},
			Produces: []string{"RawContent", "Metadata"},
		}),
		client: http.DefaultClient,
	}
	for _, opt := range opts {
		opt(f)
	}
	return f
}

func (s *URLFetcher) Name() string { return "url_fetcher" }

func (s *URLFetcher) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	url := draft.Source
	if url == "" {
		// Fallback to RawContent if Source is missing but it looks like a URL
		if strings.HasPrefix(strings.TrimSpace(draft.RawContent), "http") {
			url = strings.TrimSpace(draft.RawContent)
		}
	}
	if url == "" {
		log.Printf("url_fetcher: no URL to fetch (Source: %q, RawContent length: %d)", draft.Source, len(draft.RawContent))
		return nil, fmt.Errorf("url_fetcher: no URL to fetch")
	}

	log.Printf("url_fetcher: fetching %s", url)

	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("url_fetcher: build request: %w", err)
	}

	// Simple User-Agent to avoid some bot blocks
	req.Header.Set("User-Agent", "ContextHelp/1.0 (+https://context.help)")

	resp, err := s.client.Do(req)
	if err != nil {
		log.Printf("url_fetcher: fetch error: %v", err)
		return nil, fmt.Errorf("url_fetcher: fetch %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("url_fetcher: unexpected status %d", resp.StatusCode)
		return nil, fmt.Errorf("url_fetcher: unexpected status %d for %s", resp.StatusCode, url)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("url_fetcher: read body: %w", err)
	}

	log.Printf("url_fetcher: fetched %d bytes", len(body))

	draft.RawContent = string(body)
	draft.Metadata["final_url"] = resp.Request.URL.String()
	draft.Metadata["content_type"] = resp.Header.Get("Content-Type")

	return draft, nil
}
