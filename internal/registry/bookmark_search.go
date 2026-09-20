// Package registry implements entity index sync and remote bookmark search
// for configured registries.
package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// DefaultBookmarkSearchTimeout is the per-registry HTTP timeout for bookmark queries.
const DefaultBookmarkSearchTimeout = 5 * time.Second

// RemoteBookmark is the wire format returned by a registry's
// GET /bookmarks/search?q=<query> endpoint.
type RemoteBookmark struct {
	ID          string         `json:"id"`
	Title       string         `json:"title"`
	URL         string         `json:"url"`
	Description string         `json:"description,omitempty"`
	Tags        []string       `json:"tags,omitempty"`
	Mentions    []string       `json:"mentions,omitempty"`   // @namespace.slug strings
	EntityIDs   []string       `json:"entity_ids,omitempty"` // namespace-safe canonical entity IDs
	RegistryURL string         `json:"registry_url,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
}

// SourceResult holds the bookmark results (or error) from one registry.
type SourceResult struct {
	RegistryURL string
	Bookmarks   []RemoteBookmark
	Err         error
}

// BookmarkSearchClient queries a single registry's bookmark search endpoint.
type BookmarkSearchClient struct {
	httpClient *http.Client
	timeout    time.Duration
}

// NewBookmarkSearchClient creates a client with the given per-request timeout.
// Pass 0 to use DefaultBookmarkSearchTimeout.
func NewBookmarkSearchClient(timeout time.Duration) *BookmarkSearchClient {
	if timeout <= 0 {
		timeout = DefaultBookmarkSearchTimeout
	}
	return &BookmarkSearchClient{
		httpClient: &http.Client{Timeout: timeout},
		timeout:    timeout,
	}
}

// Search calls GET <registryURL>/bookmarks/search?q=<query> and returns results.
// On HTTP error or parse failure it returns a descriptive error; callers decide
// whether to degrade gracefully.
func (c *BookmarkSearchClient) Search(
	ctx context.Context,
	registryURL, query string,
) ([]RemoteBookmark, error) {
	endpoint := registryURL + "/bookmarks/search"
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("bookmark search: invalid registry URL %q: %w", registryURL, err)
	}

	q := u.Query()
	q.Set("q", query)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("bookmark search: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bookmark search: %s: %w", registryURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		// Registry does not expose /bookmarks/search — skip silently.
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bookmark search: %s returned HTTP %d", registryURL, resp.StatusCode)
	}

	var results []RemoteBookmark
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, fmt.Errorf("bookmark search: decode response from %s: %w", registryURL, err)
	}

	// Stamp registry source on each result.
	for i := range results {
		results[i].RegistryURL = registryURL
	}

	return results, nil
}

// ScatterGather queries registryURLs in parallel, enforcing per-source timeout
// via ctx (callers should set a deadline). Results from unreachable registries
// are dropped with a warning log; the method never returns an error for
// individual source failures (graceful degradation).
func ScatterGather(
	ctx context.Context,
	client *BookmarkSearchClient,
	registryURLs []string,
	query string,
) []SourceResult {
	if len(registryURLs) == 0 {
		return nil
	}

	results := make([]SourceResult, len(registryURLs))
	var wg sync.WaitGroup
	wg.Add(len(registryURLs))

	for i, u := range registryURLs {
		i, u := i, u
		go func() {
			defer wg.Done()
			bookmarks, err := client.Search(ctx, u, query)
			if err != nil {
				slog.WarnContext(ctx, "registry bookmark search failed (skipping)",
					"registry", u, "query", query, "err", err)
			}
			results[i] = SourceResult{RegistryURL: u, Bookmarks: bookmarks, Err: err}
		}()
	}

	wg.Wait()
	return results
}

// MergeResults converts scatter/gather output into KnowledgeObject candidates.
// Dedup is by (registryURL + remote ID); local objects are never included here.
// Results from failed registries are skipped.
func MergeResults(results []SourceResult) []*storage.KnowledgeObject {
	seen := make(map[string]struct{})
	var merged []*storage.KnowledgeObject

	for _, src := range results {
		if src.Err != nil {
			continue // already logged; graceful degradation
		}
		for _, bm := range src.Bookmarks {
			key := src.RegistryURL + "\x00" + bm.ID
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			merged = append(merged, remoteBookmarkToKO(bm, src.RegistryURL))
		}
	}

	return merged
}

// remoteBookmarkToKO maps a RemoteBookmark to a KnowledgeObject candidate.
// The object is marked with Source=registryURL and metadata["registry_source"]
// so callers can distinguish registry results from local ones.
func remoteBookmarkToKO(bm RemoteBookmark, registryURL string) *storage.KnowledgeObject {
	now := bm.CreatedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}

	tags := make([]storage.Tag, 0, len(bm.Tags))
	for _, t := range bm.Tags {
		tags = append(tags, storage.Tag{Label: t, Source: "registry"})
	}

	meta := make(map[string]any)
	for k, v := range bm.Metadata {
		meta[k] = v
	}
	meta["registry_source"] = registryURL
	meta["registry_bookmark_id"] = bm.ID
	if len(bm.EntityIDs) > 0 {
		meta["registry_entity_ids"] = bm.EntityIDs
	}
	if len(bm.Mentions) > 0 {
		meta["registry_mentions"] = bm.Mentions
	}

	var summaries []string
	if bm.Description != "" {
		summaries = []string{bm.Description}
	}

	return &storage.KnowledgeObject{
		// No ID: registry candidates are not persisted locally; callers decide.
		Type:       "url",
		Subtype:    "registry_bookmark",
		RawContent: bm.URL,
		Source:     registryURL,
		Summaries:  summaries,
		Tags:       tags,
		Metadata:   meta,
		Status:     "registry",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}
