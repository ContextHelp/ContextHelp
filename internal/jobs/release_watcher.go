package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"
)

// ReleaseWatcher polls GitHub for new releases and enqueues pipeline jobs.
type ReleaseWatcher struct {
	Repos        []string
	PollInterval time.Duration
	StateFile    string
	Queue        *Queue
	MaxRetries   int

	// injectable for tests
	httpClient *http.Client
}

// watcherState tracks last-seen release tag per repo (persisted to StateFile).
type watcherState struct {
	LastSeen map[string]string `json:"last_seen"` // repo full_name -> tag
}

// ghRelease is the minimal GitHub REST response for a release.
type ghRelease struct {
	TagName     string    `json:"tag_name"`
	HTMLURL     string    `json:"html_url"`
	PublishedAt time.Time `json:"published_at"`
}

// Run executes one poll cycle; intended to be called on a timer.
func (w *ReleaseWatcher) Run(ctx context.Context) error {
	state, err := w.loadState()
	if err != nil {
		return fmt.Errorf("release_watcher: load state: %w", err)
	}

	token := os.Getenv("GITHUB_TOKEN")
	client := w.client()

	changed := false
	for _, repo := range w.Repos {
		rel, err := w.fetchLatest(ctx, client, token, repo)
		if err != nil {
			slog.Warn("release_watcher: fetch failed", "repo", repo, "err", err)
			continue
		}
		if rel == nil {
			continue // no releases yet
		}

		prev := state.LastSeen[repo]
		if prev == rel.TagName {
			continue // nothing new
		}

		if err := w.enqueue(ctx, repo, rel); err != nil {
			slog.Warn("release_watcher: enqueue failed", "repo", repo, "tag", rel.TagName, "err", err)
			continue
		}

		slog.Info("release_watcher: new release detected", "repo", repo, "tag", rel.TagName)
		state.LastSeen[repo] = rel.TagName
		changed = true
	}

	if changed {
		if err := w.saveState(state); err != nil {
			return fmt.Errorf("release_watcher: save state: %w", err)
		}
	}
	return nil
}

// Start runs the watcher loop until ctx is cancelled.
func (w *ReleaseWatcher) Start(ctx context.Context) error {
	if err := w.Run(ctx); err != nil {
		slog.Warn("release_watcher: initial run failed", "err", err)
	}

	ticker := time.NewTicker(w.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := w.Run(ctx); err != nil {
				slog.Warn("release_watcher: poll failed", "err", err)
			}
		}
	}
}

// fetchLatest returns the latest release for a repo (nil if none).
func (w *ReleaseWatcher) fetchLatest(ctx context.Context, client *http.Client, token, repo string) (*ghRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil // no releases
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("github API %d: %s", resp.StatusCode, body)
	}

	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

// enqueue adds a pipeline job for a newly detected release.
func (w *ReleaseWatcher) enqueue(ctx context.Context, repo string, rel *ghRelease) error {
	return w.Queue.EnqueueIngestJob(ctx, "ingest:github_release", rel.HTMLURL, "github.release", rel.HTMLURL, w.maxRetries())
}

func (w *ReleaseWatcher) maxRetries() int {
	if w.MaxRetries > 0 {
		return w.MaxRetries
	}
	return 3
}

func (w *ReleaseWatcher) client() *http.Client {
	if w.httpClient != nil {
		return w.httpClient
	}
	return &http.Client{Timeout: 15 * time.Second}
}

// loadState reads state from StateFile; returns empty state on first run.
func (w *ReleaseWatcher) loadState() (*watcherState, error) {
	state := &watcherState{LastSeen: make(map[string]string)}
	if w.StateFile == "" {
		return state, nil
	}
	data, err := os.ReadFile(w.StateFile)
	if os.IsNotExist(err) {
		return state, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, state); err != nil {
		return nil, err
	}
	if state.LastSeen == nil {
		state.LastSeen = make(map[string]string)
	}
	return state, nil
}

// saveState writes state to StateFile atomically via a temp-file rename.
func (w *ReleaseWatcher) saveState(state *watcherState) error {
	if w.StateFile == "" {
		return nil
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	tmp := w.StateFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, w.StateFile)
}
