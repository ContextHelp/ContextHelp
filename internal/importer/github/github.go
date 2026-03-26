// Package github provides a GitHub importer that fetches repos from a user's
// starred, watched, and contributed lists and converts them to ImportedRepo records.
package github

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// ListType is a string type for GitHub user lists.
type ListType string

const (
	ListStarred     ListType = "starred"
	ListWatched     ListType = "watched"
	ListContributed ListType = "contributed"

	defaultBaseURL = "https://api.github.com"
	defaultPerPage = 100
)

// FetchOptions controls which lists and user to fetch from.
type FetchOptions struct {
	Username string
	Lists    []ListType
}

// ImportedRepo is a single repository record from a GitHub list.
type ImportedRepo struct {
	URL         string   `json:"url"`
	FullName    string   `json:"full_name"`
	Description string   `json:"description,omitempty"`
	Stars       int      `json:"stars"`
	Language    string   `json:"language,omitempty"`
	Source      ListType `json:"source"`
}

// Client fetches repos from GitHub user lists.
type Client struct {
	baseURL string
	token   string
	hc      *http.Client
}

// NewClient constructs a GitHub client.
func NewClient(httpClient *http.Client, baseURL, token string) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		hc:      httpClient,
	}
}

// Fetch fetches repos based on the provided options.
func (c *Client) Fetch(ctx context.Context, opts FetchOptions) ([]ImportedRepo, error) {
	if opts.Username == "" {
		return nil, fmt.Errorf("github fetch: username required")
	}
	if len(opts.Lists) == 0 {
		opts.Lists = []ListType{ListStarred, ListWatched, ListContributed}
	}

	var all []ImportedRepo
	seen := make(map[string]bool)

	for _, list := range opts.Lists {
		repos, err := c.fetchList(ctx, opts.Username, list)
		if err != nil {
			return nil, err
		}
		for _, repo := range repos {
			if !seen[repo.FullName] {
				seen[repo.FullName] = true
				all = append(all, repo)
			}
		}
	}
	return all, nil
}

func (c *Client) fetchList(ctx context.Context, username string, list ListType) ([]ImportedRepo, error) {
	var url string
	switch list {
	case ListStarred:
		url = fmt.Sprintf("%s/users/%s/starred?per_page=%d", c.baseURL, username, defaultPerPage)
		return c.fetchPaginated(ctx, url, list)
	case ListWatched:
		url = fmt.Sprintf("%s/users/%s/subscriptions?per_page=%d", c.baseURL, username, defaultPerPage)
		return c.fetchPaginated(ctx, url, list)
	case ListContributed:
		url = fmt.Sprintf("%s/search/repositories?q=contributor:%s&per_page=%d", c.baseURL, username, defaultPerPage)
		return c.fetchSearchPaginated(ctx, url, list)
	default:
		return nil, fmt.Errorf("github importer: unknown list %q", list)
	}
}

// RenderContent formats an ImportedRepo into a text payload for ingestion.
func RenderContent(repo ImportedRepo) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Repository: %s\n", repo.FullName))
	sb.WriteString(fmt.Sprintf("URL: %s\n", repo.URL))
	sb.WriteString(fmt.Sprintf("Source: %s\n", repo.Source))
	if repo.Language != "" {
		sb.WriteString(fmt.Sprintf("Language: %s\n", repo.Language))
	}
	sb.WriteString(fmt.Sprintf("Stars: %d\n", repo.Stars))
	if repo.Description != "" {
		sb.WriteString("\nDescription:\n")
		sb.WriteString(repo.Description)
		sb.WriteString("\n")
	}
	return sb.String()
}
