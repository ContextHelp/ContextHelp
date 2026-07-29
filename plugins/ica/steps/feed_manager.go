package steps

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ICAFeedManager is a thin REST wrapper around ica's
// /api/v1/feeds endpoint. It ensures a feed exists for the
// draft's source URL and populates metadata from the response.
type ICAFeedManager struct {
	pipeline.BaseContract
	apiURL string
	client *http.Client
}

// NewICAFeedManager creates an ICAFeedManager targeting the
// given API base URL.
func NewICAFeedManager(
	apiURL string,
	client *http.Client,
) *ICAFeedManager {
	if client == nil {
		client = http.DefaultClient
	}
	return &ICAFeedManager{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires:     []string{"Source"},
			Produces:     []string{"Metadata"},
			Capabilities: []string{"ica_api"},
		}),
		apiURL: apiURL,
		client: client,
	}
}

func (s *ICAFeedManager) Name() string {
	return "ica_feed_manager"
}

// feedResponse is the subset of fields returned by the
// /api/v1/feeds endpoints.
type feedResponse struct {
	ID         string `json:"id"`
	URL        string `json:"url"`
	Status     string `json:"status"`
	LastSyncAt string `json:"last_sync_at,omitempty"`
	ItemCount  int    `json:"item_count,omitempty"`
}

func (s *ICAFeedManager) Run(
	ctx context.Context,
	draft *storage.KnowledgeObject,
) (*storage.KnowledgeObject, error) {
	feedURL := draft.Source
	if feedURL == "" {
		return nil, fmt.Errorf(
			"ica_feed_manager: no source URL",
		)
	}

	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	feed, err := s.findFeed(ctx, feedURL)
	if err != nil {
		return nil, err
	}

	if feed == nil {
		feed, err = s.createFeed(ctx, feedURL)
		if err != nil {
			return nil, err
		}
	}

	s.populateMetadata(draft, feed)
	return draft, nil
}

// findFeed looks up an existing feed by URL.
// Returns nil if not found.
func (s *ICAFeedManager) findFeed(
	ctx context.Context,
	feedURL string,
) (*feedResponse, error) {
	endpoint := s.apiURL + "/api/v1/feeds?url=" +
		url.QueryEscape(feedURL)

	req, err := http.NewRequestWithContext(
		ctx, http.MethodGet, endpoint, nil,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"ica_feed_manager: build find request: %w", err,
		)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf(
			"ica_feed_manager: find feed %s: %w",
			feedURL, err,
		)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf(
			"ica_feed_manager: find feed: status %d",
			resp.StatusCode,
		)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf(
			"ica_feed_manager: read find body: %w", err,
		)
	}

	var feeds []feedResponse
	if err := json.Unmarshal(body, &feeds); err != nil {
		return nil, fmt.Errorf(
			"ica_feed_manager: decode feeds: %w", err,
		)
	}

	if len(feeds) == 0 {
		return nil, nil
	}
	return &feeds[0], nil
}

// createFeed registers a new feed via POST.
func (s *ICAFeedManager) createFeed(
	ctx context.Context,
	feedURL string,
) (*feedResponse, error) {
	payload, err := json.Marshal(map[string]string{
		"url":    feedURL,
		"status": "active",
	})
	if err != nil {
		return nil, fmt.Errorf(
			"ica_feed_manager: marshal create body: %w", err,
		)
	}

	endpoint := s.apiURL + "/api/v1/feeds"
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, endpoint,
		bytes.NewReader(payload),
	)
	if err != nil {
		return nil, fmt.Errorf(
			"ica_feed_manager: build create request: %w", err,
		)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf(
			"ica_feed_manager: create feed %s: %w",
			feedURL, err,
		)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf(
			"ica_feed_manager: create feed: status %d",
			resp.StatusCode,
		)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf(
			"ica_feed_manager: read create body: %w", err,
		)
	}

	var feed feedResponse
	if err := json.Unmarshal(body, &feed); err != nil {
		return nil, fmt.Errorf(
			"ica_feed_manager: decode created feed: %w", err,
		)
	}
	return &feed, nil
}

func (s *ICAFeedManager) populateMetadata(
	draft *storage.KnowledgeObject,
	feed *feedResponse,
) {
	draft.Metadata["feed_id"] = feed.ID
	draft.Metadata["feed_status"] = feed.Status
	if feed.LastSyncAt != "" {
		draft.Metadata["feed_last_sync_at"] = feed.LastSyncAt
	}
	if feed.ItemCount > 0 {
		draft.Metadata["feed_item_count"] = feed.ItemCount
	}
}
