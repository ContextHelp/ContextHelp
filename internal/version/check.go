// Package version provides update-check helpers for CLI binaries.
package version

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	releaseAPIURL = "https://api.github.com/repos/jadb/ContextHelp/releases/latest"
	checkTimeout  = 5 * time.Second
)

// Fetcher retrieves the latest release tag from the GitHub releases API.
// Swappable in tests.
type Fetcher func(ctx context.Context) (string, error)

// DefaultFetcher hits the real GitHub releases API.
var DefaultFetcher Fetcher = func(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releaseAPIURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("unexpected response: %w", err)
	}
	if payload.TagName == "" {
		return "", fmt.Errorf("empty tag_name in GitHub response")
	}
	return payload.TagName, nil
}

// CheckResult holds the outcome of a version check.
type CheckResult struct {
	Current   string
	Latest    string
	UpToDate  bool
	FetchErr  error
}

// Check compares current against the latest GitHub release.
// Uses f (or DefaultFetcher if nil). Network errors → soft warning (FetchErr set, no panic).
func Check(current string, f Fetcher) CheckResult {
	if f == nil {
		f = DefaultFetcher
	}

	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()

	latest, err := f(ctx)
	if err != nil {
		return CheckResult{Current: current, FetchErr: err}
	}

	// Normalise: strip leading 'v' for comparison.
	norm := func(s string) string { return strings.TrimPrefix(s, "v") }
	upToDate := norm(latest) == norm(current)

	return CheckResult{
		Current:  current,
		Latest:   latest,
		UpToDate: upToDate,
	}
}

// FormatResult returns the human-readable update-check line.
func FormatResult(r CheckResult) string {
	if r.FetchErr != nil {
		return fmt.Sprintf("warning: update check failed: %v", r.FetchErr)
	}
	if r.UpToDate {
		return "Up to date"
	}
	return fmt.Sprintf("Update available: %s (you have %s)", r.Latest, r.Current)
}
