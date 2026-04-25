// Package github provides a GitHub importer that fetches repos from a user's
// starred, watched, and contributed lists and converts them to ImportedRepo records.
package github

import (
	"context"
	"fmt"
	"io"
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
	Topics      []string `json:"topics,omitempty"`
	License     string   `json:"license,omitempty"`
	Homepage    string   `json:"homepage,omitempty"`
	Forks       int      `json:"forks,omitempty"`
	Archived    bool     `json:"archived,omitempty"`
	Source      ListType `json:"source"`
	Readme      string   `json:"readme,omitempty"`
	Context7    string   `json:"context7,omitempty"`
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

// Enrich fetches README and Context7 docs for each repo (best-effort).
func (c *Client) Enrich(ctx context.Context, repos []ImportedRepo) {
	for i := range repos {
		c.enrichOne(ctx, &repos[i])
	}
}

func (c *Client) enrichOne(ctx context.Context, repo *ImportedRepo) {
	// README via API
	parts := strings.SplitN(repo.FullName, "/", 2)
	if len(parts) == 2 {
		url := fmt.Sprintf("%s/repos/%s/readme", c.baseURL, repo.FullName)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err == nil {
			if c.token != "" {
				req.Header.Set("Authorization", "Bearer "+c.token)
			}
			req.Header.Set("Accept", "application/vnd.github.raw+json")
			resp, err := c.hc.Do(req)
			if err == nil {
				defer resp.Body.Close()
				if resp.StatusCode == 200 {
					body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
					if err == nil {
						repo.Readme = string(body)
					}
				}
			}
		}
	}
	// Context7
	c7url := fmt.Sprintf("https://context7.com/%s/llms.txt", repo.FullName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c7url, nil)
	if err == nil {
		resp, err := c.hc.Do(req)
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == 200 {
				body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
				if err == nil && len(body) > 100 {
					repo.Context7 = string(body)
				}
			}
		}
	}
}

// RenderContent formats an ImportedRepo into rich markdown for ingestion.
func RenderContent(repo ImportedRepo) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# %s\n\n", repo.FullName))
	if repo.Description != "" {
		sb.WriteString(repo.Description + "\n\n")
	}
	sb.WriteString(fmt.Sprintf("- URL: %s\n", repo.URL))
	if repo.Homepage != "" {
		sb.WriteString(fmt.Sprintf("- Homepage: %s\n", repo.Homepage))
	}
	sb.WriteString(fmt.Sprintf("- Stars: %d / Forks: %d\n", repo.Stars, repo.Forks))
	if repo.Language != "" {
		sb.WriteString(fmt.Sprintf("- Language: %s\n", repo.Language))
	}
	if repo.License != "" {
		sb.WriteString(fmt.Sprintf("- License: %s\n", repo.License))
	}
	if len(repo.Topics) > 0 {
		sb.WriteString(fmt.Sprintf("- Topics: %s\n", strings.Join(repo.Topics, ", ")))
	}
	if repo.Archived {
		sb.WriteString("- Status: ARCHIVED\n")
	}
	sb.WriteString(fmt.Sprintf("- Source: %s\n", repo.Source))
	if repo.Readme != "" {
		sb.WriteString("\n## README\n\n" + repo.Readme + "\n")
	}
	if repo.Context7 != "" {
		sb.WriteString("\n## Documentation (Context7)\n\n" + repo.Context7 + "\n")
	}
	return sb.String()
}
