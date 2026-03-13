package steps

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// FeedFetcher fetches the raw content of an RSS/Atom/JSON Feed from a URL.
type FeedFetcher struct {
	pipeline.BaseContract
	client *http.Client
}

// FeedFetcherOption configures a FeedFetcher.
type FeedFetcherOption func(*FeedFetcher)

// WithHTTPClient sets a custom HTTP client for the fetcher.
func WithHTTPClient(c *http.Client) FeedFetcherOption {
	return func(f *FeedFetcher) { f.client = c }
}

// NewFeedFetcher creates a FeedFetcher with optional configuration.
func NewFeedFetcher(opts ...FeedFetcherOption) *FeedFetcher {
	f := &FeedFetcher{
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

func (s *FeedFetcher) Name() string { return "feed_fetcher" }

func (s *FeedFetcher) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	url := draft.Source
	if url == "" {
		return nil, fmt.Errorf("feed_fetcher: no source URL")
	}

	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Metadata["feed_url"] = url

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("feed_fetcher: build request: %w", err)
	}

	// Send conditional request headers if available.
	if etag, ok := draft.Metadata["etag"].(string); ok && etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if lm, ok := draft.Metadata["last_modified"].(string); ok && lm != "" {
		req.Header.Set("If-Modified-Since", lm)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("feed_fetcher: fetch %s: %w", url, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNotModified:
		draft.Metadata["not_modified"] = true
		return draft, nil
	case http.StatusNotFound, http.StatusGone:
		draft.Metadata["feed_gone"] = true
		return draft, nil
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("feed_fetcher: unexpected status %d for %s", resp.StatusCode, url)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("feed_fetcher: read body: %w", err)
	}

	draft.RawContent = string(body)

	if etag := resp.Header.Get("ETag"); etag != "" {
		draft.Metadata["etag"] = etag
	}
	if lm := resp.Header.Get("Last-Modified"); lm != "" {
		draft.Metadata["last_modified"] = lm
	}

	return draft, nil
}
