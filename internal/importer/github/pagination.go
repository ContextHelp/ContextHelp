package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// apiRepo is the minimal GitHub REST API repo shape used by both list and search endpoints.
type apiRepo struct {
	FullName    string   `json:"full_name"`
	HTMLURL     string   `json:"html_url"`
	Description string   `json:"description"`
	StarCount   int      `json:"stargazers_count"`
	Language    string   `json:"language"`
	Topics      []string `json:"topics"`
	License     *struct {
		SpdxID string `json:"spdx_id"`
	} `json:"license"`
	Homepage string `json:"homepage"`
	Forks    int    `json:"forks_count"`
	Archived bool   `json:"archived"`
}

// apiSearchResult is the shape returned by /search/repositories.
type apiSearchResult struct {
	Items []apiRepo `json:"items"`
}

// fetchPaginated follows Link-header pagination for list endpoints.
func (c *Client) fetchPaginated(ctx context.Context, firstURL string, source ListType) ([]ImportedRepo, error) {
	var repos []ImportedRepo
	url := firstURL

	for url != "" {
		raw, nextURL, err := c.getPage(ctx, url)
		if err != nil {
			return nil, fmt.Errorf("fetch %s: %w", source, err)
		}

		var page []apiRepo
		if err := json.Unmarshal(raw, &page); err != nil {
			return nil, fmt.Errorf("decode %s page: %w", source, err)
		}
		for _, r := range page {
			repos = append(repos, toImported(r, source))
		}
		url = nextURL
	}
	return repos, nil
}

// fetchSearchPaginated follows Link-header pagination for the search endpoint.
func (c *Client) fetchSearchPaginated(ctx context.Context, firstURL string, source ListType) ([]ImportedRepo, error) {
	var repos []ImportedRepo
	url := firstURL

	for url != "" {
		raw, nextURL, err := c.getPage(ctx, url)
		if err != nil {
			return nil, fmt.Errorf("fetch %s: %w", source, err)
		}

		var result apiSearchResult
		if err := json.Unmarshal(raw, &result); err != nil {
			return nil, fmt.Errorf("decode %s search page: %w", source, err)
		}
		for _, r := range result.Items {
			repos = append(repos, toImported(r, source))
		}
		url = nextURL
	}
	return repos, nil
}

// getPage performs a single GET, handles rate limiting, and returns (body, nextURL, error).
func (c *Client) getPage(ctx context.Context, url string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	// Rate limit handling: sleep until reset if remaining == 0.
	if remaining := resp.Header.Get("X-RateLimit-Remaining"); remaining == "0" {
		resetAt := resp.Header.Get("X-RateLimit-Reset")
		if resetAt != "" {
			if ts, err := strconv.ParseInt(resetAt, 10, 64); err == nil {
				wait := time.Until(time.Unix(ts, 0))
				if wait > 0 {
					select {
					case <-ctx.Done():
						return nil, "", ctx.Err()
					case <-time.After(wait):
					}
					// Retry the same page after sleeping.
					return c.getPage(ctx, url)
				}
			}
		}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("github API %s: HTTP %d: %s", url, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	next := parseLinkNext(resp.Header.Get("Link"))
	return body, next, nil
}

// parseLinkNext extracts the "next" URL from a GitHub Link header value.
// E.g.: `<https://api.github.com/...?page=2>; rel="next", <...>; rel="last"`
func parseLinkNext(header string) string {
	if header == "" {
		return ""
	}
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		segments := strings.SplitN(part, ";", 2)
		if len(segments) != 2 {
			continue
		}
		urlPart := strings.TrimSpace(segments[0])
		relPart := strings.TrimSpace(segments[1])
		if !strings.Contains(relPart, `rel="next"`) {
			continue
		}
		urlPart = strings.TrimPrefix(urlPart, "<")
		urlPart = strings.TrimSuffix(urlPart, ">")
		return urlPart
	}
	return ""
}

// toImported converts an apiRepo to an ImportedRepo.
func toImported(r apiRepo, source ListType) ImportedRepo {
	repo := ImportedRepo{
		URL:         r.HTMLURL,
		FullName:    r.FullName,
		Description: r.Description,
		Stars:       r.StarCount,
		Language:    r.Language,
		Topics:      r.Topics,
		Homepage:    r.Homepage,
		Forks:       r.Forks,
		Archived:    r.Archived,
		Source:      source,
	}
	if r.License != nil {
		repo.License = r.License.SpdxID
	}
	return repo
}
