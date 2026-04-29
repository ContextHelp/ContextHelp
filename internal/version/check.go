// Package version provides update-check helpers for CLI binaries.
//
// The implementation now delegates network discovery to kit/go/core/upgrade
// when DefaultFetcher is left at its package default. Tests can still
// override DefaultFetcher with a mock Fetcher to avoid hitting the real
// GitHub API; the shim path bypasses kit/upgrade entirely.
package version

import (
	"context"
	"fmt"
	"strings"
	"time"

	"hop.top/kit/go/core/upgrade"
)

const (
	githubRepo   = "jadb/ContextHelp"
	checkTimeout = 5 * time.Second
)

// Fetcher retrieves the latest release tag. Swappable in tests.
type Fetcher func(ctx context.Context) (string, error)

// DefaultFetcher delegates to kit/go/core/upgrade. Tests override this
// to avoid hitting the network; production goes through kit's checker.
var DefaultFetcher Fetcher = kitFetcher

// kitFetcher uses kit/upgrade.Checker to discover the latest tag.
func kitFetcher(ctx context.Context) (string, error) {
	c := upgrade.New(
		upgrade.WithBinary("ctxt", ""),
		upgrade.WithGitHub(githubRepo),
		upgrade.WithTimeout(checkTimeout),
		upgrade.WithCacheTTL(0), // bypass kit's cache for --check
	)
	r := c.Check(ctx)
	if r.Err != nil {
		return "", r.Err
	}
	return r.Latest, nil
}

// CheckResult holds the outcome of a version check.
type CheckResult struct {
	Current  string
	Latest   string
	UpToDate bool
	FetchErr error
}

// Check compares current against the latest GitHub release.
// Uses f (or DefaultFetcher if nil). Network errors → soft warning (FetchErr set).
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
