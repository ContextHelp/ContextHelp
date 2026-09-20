// Package registry — registry index (reference registry / registry of registries).
//
// The RegistryIndex is a root discovery document that lists known registries
// with their metadata (namespaces, trust level, entity counts, version timestamps).
// It is used for discovery only — subscribing to a registry is a separate action.
package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// RegistryIndexEntry describes one registry in the reference registry index.
type RegistryIndexEntry struct {
	// Name is a human-readable label for the registry.
	Name string `json:"name"`
	// URL is the base URL of the registry.
	URL string `json:"url"`
	// Description is a short summary of what the registry covers.
	Description string `json:"description"`
	// Namespaces lists the top-level namespace prefixes the registry owns.
	Namespaces []string `json:"namespaces,omitempty"`
	// Tags are subject-matter labels used for discovery (e.g. "ai", "web", "devops").
	Tags []string `json:"tags,omitempty"`
	// TrustLevel is the recommended trust level for new subscribers.
	// Informational; the subscriber sets the actual trust in their config.
	TrustLevel string `json:"trust_level,omitempty"`
	// EntityCount is the approximate number of entities in the registry.
	EntityCount int `json:"entity_count,omitempty"`
	// Version is the latest published version of the registry manifest.
	Version string `json:"version,omitempty"`
	// UpdatedAt is when the registry was last updated in the index.
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

// RegistryIndex is the root discovery document (reference registry).
type RegistryIndex struct {
	// Version is the schema version of this index document.
	Version string `json:"version"`
	// Registries is the list of known registries.
	Registries []RegistryIndexEntry `json:"registries"`
	// FetchedAt is when the index was last fetched from the remote.
	FetchedAt time.Time `json:"fetched_at,omitempty"`
}

// DefaultIndexURL is the well-known URL for the official reference registry index.
const DefaultIndexURL = "https://registry.ctxt.dev/index.json"

// IndexSearchResult is one match returned by SearchIndex.
type IndexSearchResult struct {
	Entry RegistryIndexEntry
	// Score indicates relevance (higher = better). Informational; not guaranteed stable.
	Score int
}

// SearchIndex searches an in-memory RegistryIndex for registries matching query.
//
// Matches on: namespace prefix, tags (substring), name (substring), description (substring).
// Returns results ordered by relevance score (descending).
func SearchIndex(idx *RegistryIndex, query string) []IndexSearchResult {
	if idx == nil || query == "" {
		return nil
	}

	q := strings.ToLower(strings.TrimSpace(query))
	var results []IndexSearchResult

	for _, entry := range idx.Registries {
		score := scoreIndexEntry(entry, q)
		if score > 0 {
			results = append(results, IndexSearchResult{Entry: entry, Score: score})
		}
	}

	// Sort by score descending (simple insertion sort; indices are small).
	for i := 1; i < len(results); i++ {
		for j := i; j > 0 && results[j].Score > results[j-1].Score; j-- {
			results[j], results[j-1] = results[j-1], results[j]
		}
	}

	return results
}

func scoreIndexEntry(entry RegistryIndexEntry, q string) int {
	score := 0

	// Namespace prefix match: highest weight.
	for _, ns := range entry.Namespaces {
		if strings.HasPrefix(strings.ToLower(ns), q) {
			score += 10
		} else if strings.Contains(strings.ToLower(ns), q) {
			score += 5
		}
	}

	// Tag match.
	for _, tag := range entry.Tags {
		if strings.EqualFold(tag, q) {
			score += 8
		} else if strings.Contains(strings.ToLower(tag), q) {
			score += 3
		}
	}

	// Name match.
	nameLower := strings.ToLower(entry.Name)
	if strings.EqualFold(entry.Name, q) {
		score += 6
	} else if strings.Contains(nameLower, q) {
		score += 2
	}

	// Description match.
	if strings.Contains(strings.ToLower(entry.Description), q) {
		score += 1
	}

	return score
}

// FetchIndex fetches the registry index from indexURL and returns the parsed document.
// Uses a 15 s HTTP timeout.
func FetchIndex(ctx context.Context, client *http.Client, indexURL string) (*RegistryIndex, error) {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, indexURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build index request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch registry index: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry index returned HTTP %d", resp.StatusCode)
	}

	var idx RegistryIndex
	if err := json.NewDecoder(resp.Body).Decode(&idx); err != nil {
		return nil, fmt.Errorf("decode registry index: %w", err)
	}
	idx.FetchedAt = time.Now()
	return &idx, nil
}
