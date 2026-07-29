package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ICAFetcher calls the ICA REST API to fetch and parse a feed.
type ICAFetcher struct {
	pipeline.BaseContract
	apiURL string
	client *http.Client
}

// NewICAFetcher creates an ICAFetcher targeting the given API base URL.
func NewICAFetcher(
	apiURL string,
	client *http.Client,
) *ICAFetcher {
	if client == nil {
		client = http.DefaultClient
	}
	return &ICAFetcher{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires:     []string{"Source"},
			Produces:     []string{"RawContent", "Metadata"},
			Capabilities: []string{"ica_api"},
		}),
		apiURL: apiURL,
		client: client,
	}
}

func (s *ICAFetcher) Name() string { return "ica_fetcher" }

func (s *ICAFetcher) Run(
	ctx context.Context,
	draft *storage.KnowledgeObject,
) (*storage.KnowledgeObject, error) {
	feedURL := draft.Source
	if feedURL == "" {
		return nil, fmt.Errorf("ica_fetcher: no source URL")
	}

	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	endpoint := s.apiURL + "/api/v1/feeds/sync?url=" +
		url.QueryEscape(feedURL)

	req, err := http.NewRequestWithContext(
		ctx, http.MethodGet, endpoint, nil,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"ica_fetcher: build request: %w", err,
		)
	}

	// Conditional request headers.
	if etag, ok := draft.Metadata["etag"].(string); ok &&
		etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if lm, ok := draft.Metadata["last_modified"].(string); ok &&
		lm != "" {
		req.Header.Set("If-Modified-Since", lm)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf(
			"ica_fetcher: fetch %s: %w", feedURL, err,
		)
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
		return nil, fmt.Errorf(
			"ica_fetcher: unexpected status %d for %s",
			resp.StatusCode, feedURL,
		)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf(
			"ica_fetcher: read body: %w", err,
		)
	}

	draft.RawContent = string(body)

	var items []any
	if err := json.Unmarshal(body, &items); err == nil {
		draft.Metadata["feed_items"] = items
	}

	if etag := resp.Header.Get("ETag"); etag != "" {
		draft.Metadata["etag"] = etag
	}
	if lm := resp.Header.Get("Last-Modified"); lm != "" {
		draft.Metadata["last_modified"] = lm
	}

	return draft, nil
}
