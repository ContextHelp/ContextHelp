// Package github provides a GitHub importer that fetches repos from a user's
// starred, watched, and contributed lists and converts them to ImportedRepo records.
package github

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"
)

const (
	ListStarred     = "starred"
	ListWatched     = "watched"
	ListContributed = "contributed"

	defaultBaseURL  = "https://api.github.com"
	defaultPerPage  = 100
)

// Config holds configuration for GitHubImporter.
type Config struct {
	// Token is the GitHub personal access token. Falls back to GITHUB_TOKEN env.
	Token string
	// Username is the GitHub login to fetch lists for.
	Username string
	// Lists selects which lists to fetch: "starred", "watched", "contributed".
	Lists []string
	// BaseURL overrides the GitHub API base URL (useful for testing).
	BaseURL string
}

// ImportedRepo is a single repository record from a GitHub list.
type ImportedRepo struct {
	URL         string `json:"url"`
	FullName    string `json:"full_name"`
	Description string `json:"description,omitempty"`
	Stars       int    `json:"stars"`
	Language    string `json:"language,omitempty"`
	// Source is the list name that produced this record: "starred", "watched", "contributed".
	Source string `json:"source"`
}

// GitHubImporter fetches repos from GitHub user lists.
type GitHubImporter struct {
	cfg    Config
	client *http.Client
}

// New constructs a GitHubImporter with the given config.
// Returns an error if neither cfg.Token nor the GITHUB_TOKEN env is set.
func New(cfg Config) (*GitHubImporter, error) {
	if cfg.Token == "" {
		cfg.Token = os.Getenv("GITHUB_TOKEN")
	}
	if cfg.Token == "" {
		return nil, fmt.Errorf("github importer: token required (set Token or GITHUB_TOKEN)")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURL
	}
	return &GitHubImporter{
		cfg:    cfg,
		client: &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// Import fetches all configured lists concurrently and returns deduplicated repos.
func (g *GitHubImporter) Import(ctx context.Context) ([]ImportedRepo, error) {
	lists := g.cfg.Lists
	if len(lists) == 0 {
		lists = []string{ListStarred, ListWatched, ListContributed}
	}

	type result struct {
		repos []ImportedRepo
		err   error
	}

	results := make([]result, len(lists))
	var wg sync.WaitGroup
	wg.Add(len(lists))

	for i, list := range lists {
		i, list := i, list
		go func() {
			defer wg.Done()
			repos, err := g.fetchList(ctx, list)
			results[i] = result{repos: repos, err: err}
		}()
	}
	wg.Wait()

	seen := make(map[string]bool)
	var all []ImportedRepo
	for _, r := range results {
		if r.err != nil {
			return nil, r.err
		}
		for _, repo := range r.repos {
			if !seen[repo.FullName] {
				seen[repo.FullName] = true
				all = append(all, repo)
			}
		}
	}
	return all, nil
}

// fetchList dispatches to the appropriate fetch function by list name.
func (g *GitHubImporter) fetchList(ctx context.Context, list string) ([]ImportedRepo, error) {
	switch list {
	case ListStarred:
		return g.fetchStarred(ctx)
	case ListWatched:
		return g.fetchWatched(ctx)
	case ListContributed:
		return g.fetchContributed(ctx)
	default:
		return nil, fmt.Errorf("github importer: unknown list %q", list)
	}
}

func (g *GitHubImporter) fetchStarred(ctx context.Context) ([]ImportedRepo, error) {
	url := fmt.Sprintf("%s/users/%s/starred?per_page=%d", g.cfg.BaseURL, g.cfg.Username, defaultPerPage)
	return g.fetchPaginated(ctx, url, ListStarred)
}

func (g *GitHubImporter) fetchWatched(ctx context.Context) ([]ImportedRepo, error) {
	url := fmt.Sprintf("%s/users/%s/subscriptions?per_page=%d", g.cfg.BaseURL, g.cfg.Username, defaultPerPage)
	return g.fetchPaginated(ctx, url, ListWatched)
}

func (g *GitHubImporter) fetchContributed(ctx context.Context) ([]ImportedRepo, error) {
	url := fmt.Sprintf("%s/search/repositories?q=contributor:%s&per_page=%d", g.cfg.BaseURL, g.cfg.Username, defaultPerPage)
	return g.fetchSearchPaginated(ctx, url, ListContributed)
}
