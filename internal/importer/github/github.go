// Package github provides a GitHub importer that fetches repositories a user
// has starred, is watching, or has contributed to.
//
// This is a stub implementation — T-0116 will replace it with a full client
// backed by the GitHub REST API. The interface is intentionally minimal so
// import_github.go can be written against it today.
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const DefaultBaseURL = "https://api.github.com"

// ListType selects which repository collection to fetch.
type ListType string

const (
	ListStarred     ListType = "starred"
	ListWatched     ListType = "watched"
	ListContributed ListType = "contributed"
)

// ImportedRepo is a single repository record returned by the importer.
type ImportedRepo struct {
	Source      ListType `json:"source"`
	FullName    string   `json:"full_name"`
	Description string   `json:"description,omitempty"`
	Language    string   `json:"language,omitempty"`
	Stars       int      `json:"stargazers_count"`
	URL         string   `json:"html_url"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// FetchOptions controls what the importer fetches.
type FetchOptions struct {
	Username string
	Lists    []ListType
	MaxItems int // 0 = all
}

// Client fetches GitHub repository lists via the REST API.
type Client struct {
	token   string
	baseURL string
	http    *http.Client
}

// NewClient creates a Client.  baseURL defaults to DefaultBaseURL when empty.
func NewClient(httpClient *http.Client, baseURL, token string) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{token: token, baseURL: strings.TrimRight(baseURL, "/"), http: httpClient}
}

// Fetch returns repos matching opts.
func (c *Client) Fetch(ctx context.Context, opts FetchOptions) ([]ImportedRepo, error) {
	var out []ImportedRepo
	seen := map[string]struct{}{}

	for _, lt := range opts.Lists {
		repos, err := c.fetchList(ctx, opts.Username, lt, opts.MaxItems)
		if err != nil {
			return nil, fmt.Errorf("github %s: %w", lt, err)
		}
		for _, r := range repos {
			if _, dup := seen[r.FullName]; dup {
				continue
			}
			seen[r.FullName] = struct{}{}
			out = append(out, r)
			if opts.MaxItems > 0 && len(out) >= opts.MaxItems {
				return out, nil
			}
		}
	}
	return out, nil
}

// RenderContent produces a plain-text summary of a repo suitable for ingestion.
func RenderContent(r ImportedRepo) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", r.FullName)
	if r.Description != "" {
		fmt.Fprintf(&b, "%s\n\n", r.Description)
	}
	if r.Language != "" {
		fmt.Fprintf(&b, "Language: %s\n", r.Language)
	}
	fmt.Fprintf(&b, "Stars: %d\n", r.Stars)
	fmt.Fprintf(&b, "URL: %s\n", r.URL)
	fmt.Fprintf(&b, "Source: github/%s\n", r.Source)
	return b.String()
}

// fetchList pages through a single endpoint and returns all repos.
func (c *Client) fetchList(ctx context.Context, username string, lt ListType, max int) ([]ImportedRepo, error) {
	var endpoint string
	switch lt {
	case ListStarred:
		endpoint = fmt.Sprintf("/users/%s/starred", username)
	case ListWatched:
		endpoint = fmt.Sprintf("/users/%s/subscriptions", username)
	case ListContributed:
		// GitHub has no direct "contributed" endpoint; use search as best-effort.
		endpoint = fmt.Sprintf("/search/repositories?q=user:%s+is:public&sort=updated", username)
	default:
		return nil, fmt.Errorf("unknown list type: %s", lt)
	}

	var out []ImportedRepo
	page := 1
	for {
		sep := "?"
		if strings.Contains(endpoint, "?") {
			sep = "&"
		}
		url := fmt.Sprintf("%s%s%sper_page=100&page=%d", c.baseURL, endpoint, sep, page)
		repos, hasMore, err := c.fetchPage(ctx, lt, url)
		if err != nil {
			return nil, err
		}
		out = append(out, repos...)
		if !hasMore || len(repos) == 0 {
			break
		}
		if max > 0 && len(out) >= max {
			break
		}
		page++
	}
	return out, nil
}

// apiRepo is the minimal GitHub API repo shape we decode.
type apiRepo struct {
	FullName    string    `json:"full_name"`
	Description string    `json:"description"`
	Language    string    `json:"language"`
	Stars       int       `json:"stargazers_count"`
	HTMLURL     string    `json:"html_url"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// searchResult wraps the search/repositories response.
type searchResult struct {
	Items []apiRepo `json:"items"`
}

func (c *Client) fetchPage(ctx context.Context, lt ListType, url string) ([]ImportedRepo, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("github API %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var items []apiRepo
	if lt == ListContributed {
		var sr searchResult
		if err := json.Unmarshal(body, &sr); err != nil {
			return nil, false, fmt.Errorf("decode search response: %w", err)
		}
		items = sr.Items
	} else {
		if err := json.Unmarshal(body, &items); err != nil {
			return nil, false, fmt.Errorf("decode repo list: %w", err)
		}
	}

	out := make([]ImportedRepo, 0, len(items))
	for _, r := range items {
		out = append(out, ImportedRepo{
			Source:      lt,
			FullName:    r.FullName,
			Description: r.Description,
			Language:    r.Language,
			Stars:       r.Stars,
			URL:         r.HTMLURL,
			UpdatedAt:   r.UpdatedAt,
		})
	}

	// GitHub paginates — infer hasMore from result count.
	hasMore := len(items) == 100
	return out, hasMore, nil
}
