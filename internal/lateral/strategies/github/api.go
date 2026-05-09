package github

import "context"

// APIClient is the github-API contract the strategies depend on. It is
// intentionally minimal; only the methods the sub-path probes need land
// here. Daemon wiring adapts go-github / Octokit / a recorded-cassette
// stub to this interface so the strategy never imports a vendor SDK.
//
// Implementations MUST handle rate-limit observability (see ratelimit.go
// for the dynamic-floor helper) and breaker integration (T-0261).
//
// All methods accept and respect ctx for cancellation.
type APIClient interface {
	// ListRepoSiblings returns the repos owned by owner that are NOT named
	// excludeRepo. Used to populate sibling_repo candidates from a repo
	// page. Cap k is enforced by the caller, not the client.
	ListRepoSiblings(ctx context.Context, owner, excludeRepo string) ([]RepoSummary, error)

	// ListOwnerStarred returns repos starred by login.
	ListOwnerStarred(ctx context.Context, login string) ([]RepoSummary, error)

	// ListOwnerPinned returns the pinned repos surfaced on login's
	// profile. Implementations MAY scrape the HTML profile when no API
	// surface exists (the GraphQL pinned-items query is the canonical
	// path; HTML scrape is a fallback).
	ListOwnerPinned(ctx context.Context, login string) ([]RepoSummary, error)

	// HasSponsorPage reports whether login has an active GitHub Sponsors
	// page.
	HasSponsorPage(ctx context.Context, login string) (bool, error)

	// ListAuthoredPRs returns PRs authored by login across github (capped
	// by limit). Used by the PR sub-path probe.
	ListAuthoredPRs(ctx context.Context, login string, limit int) ([]PullRequestSummary, error)

	// ListPRReviewers returns the requested + actual reviewers of the PR
	// at owner/repo#number.
	ListPRReviewers(ctx context.Context, owner, repo string, number int) ([]UserSummary, error)

	// ListAuthoredIssues returns issues authored by login (capped).
	ListAuthoredIssues(ctx context.Context, login string, limit int) ([]IssueSummary, error)

	// ListIssueLabels returns the labels attached to the issue at
	// owner/repo#number.
	ListIssueLabels(ctx context.Context, owner, repo string, number int) ([]LabelSummary, error)

	// ListSponsored returns the profiles login sponsors via GitHub
	// Sponsors.
	ListSponsored(ctx context.Context, login string) ([]UserSummary, error)

	// ListContributionOrgs returns the organizations login publicly
	// contributes to (one year of contributions surface).
	ListContributionOrgs(ctx context.Context, login string) ([]OrgSummary, error)

	// ListSimilarSponsors returns profiles that sponsor a similar set of
	// recipients to login (used by the sponsor sub-path probe).
	ListSimilarSponsors(ctx context.Context, login string) ([]UserSummary, error)

	// ListOwnerGists returns gists owned by login.
	ListOwnerGists(ctx context.Context, login string) ([]GistSummary, error)

	// ListGlobalAdvisories returns recent global advisory database
	// entries; used to seed similar-advisory candidates.
	ListGlobalAdvisories(ctx context.Context, ecosystem, severity string, limit int) ([]AdvisorySummary, error)

	// RateSnapshot returns the current rate-limit snapshot the dynamic
	// floor consults. Implementations MAY return zero values if the
	// upstream did not report rate-limit headers on the last call; the
	// caller treats zero as "no signal" and skips the floor adjustment.
	// RateSnapshot is defined in ratelimit.go.
	RateSnapshot(ctx context.Context) RateSnapshot
}

// RateSnapshot is defined in ratelimit.go alongside the dynamic-floor
// helper that consumes it.

// RepoSummary is the cross-method repo identity payload.
type RepoSummary struct {
	Owner       string
	Name        string
	URL         string
	Description string
	Stars       int
	Language    string
	Archived    bool
	Fork        bool
}

// UserSummary is a github user/org identity payload.
type UserSummary struct {
	Login   string
	URL     string
	Type    string // "User" or "Organization"
	Name    string
	Company string
}

// OrgSummary is an organization identity payload.
type OrgSummary struct {
	Login string
	URL   string
	Name  string
}

// PullRequestSummary identifies a PR for cross-PR candidate emission.
type PullRequestSummary struct {
	Owner  string
	Repo   string
	Number int
	Title  string
	URL    string
	State  string
	Author string
}

// IssueSummary identifies an issue for cross-issue candidate emission.
type IssueSummary struct {
	Owner  string
	Repo   string
	Number int
	Title  string
	URL    string
	State  string
	Author string
}

// LabelSummary identifies a single repo label.
type LabelSummary struct {
	Name        string
	URL         string
	Description string
	Color       string
}

// GistSummary identifies a gist.
type GistSummary struct {
	ID          string
	URL         string
	Description string
	Owner       string
	Files       int
}

// AdvisorySummary identifies a global advisory entry.
type AdvisorySummary struct {
	GHSAID      string
	URL         string
	Summary     string
	Severity    string
	Ecosystem   string
	PackageName string
}
