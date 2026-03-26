// Package github provides typed snapshot structs for the GitHub pipeline adapter.
//
// Snapshots are produced by FetchRepo / FetchIssue / FetchPR and consumed by
// pipeline mapper steps and post-ingest enrichers.
package github

import (
	"time"

	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// RepoSnapshot is the normalised view of a GitHub repository page.
//
// Populated by FetchRepo; consumed by the github_repo_mapper pipeline step and
// repo_health_enricher / alternative_detector / dependency_enricher plugins.
type RepoSnapshot struct {
	// Identity
	Owner    string `json:"owner"`
	Name     string `json:"name"`
	FullName string `json:"full_name"` // "<owner>/<name>"
	URL      string `json:"url"`

	// Description & content
	Description string   `json:"description,omitempty"`
	Language    string   `json:"language,omitempty"`
	Topics      []string `json:"topics,omitempty"`
	License     string   `json:"license,omitempty"`
	README      string   `json:"readme,omitempty"`
	Sections    []pluginapi.Section `json:"sections,omitempty"`

	// Stats
	Stars      int `json:"stars"`
	Forks      int `json:"forks"`
	OpenIssues int `json:"open_issues"`

	// Flags
	IsArchived bool `json:"is_archived"`
	IsFork     bool `json:"is_fork"`

	// Timestamps
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	PushedAt  time.Time `json:"pushed_at"`

	// Dependencies: populated from dependency manifest (go.mod, package.json, etc.)
	Dependencies []Dependency `json:"dependencies,omitempty"`

	// Releases: last 5 releases, newest first.
	Releases []Release `json:"releases,omitempty"`
}

// IssueSnapshot is the normalised view of a GitHub issue page.
//
// Populated by FetchIssue; consumed by the github_issue_mapper pipeline step.
type IssueSnapshot struct {
	// Identity
	Owner  string `json:"owner"`
	Repo   string `json:"repo"`
	Number int    `json:"number"`
	URL    string `json:"url"`

	// Content
	Title  string `json:"title"`
	Body   string `json:"body,omitempty"`
	State  string `json:"state"` // "open" | "closed"
	Labels []string `json:"labels,omitempty"`

	// Author & assignees
	Author    string   `json:"author,omitempty"`
	Assignees []string `json:"assignees,omitempty"`

	// Linked PR number if the issue was closed via a PR (0 = none).
	LinkedPR int `json:"linked_pr,omitempty"`

	// Sections: extracted comment threads / discussion sections.
	Sections []pluginapi.Section `json:"sections,omitempty"`

	// Timestamps
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	ClosedAt  *time.Time `json:"closed_at,omitempty"`
}

// Dependency is a single dependency entry extracted from a repository manifest.
type Dependency struct {
	Name      string `json:"name"`
	Version   string `json:"version,omitempty"`
	Ecosystem string `json:"ecosystem"` // "npm" | "go" | "pip" | "cargo" | etc.
}

// Release is a single GitHub release entry.
type Release struct {
	Tag         string    `json:"tag"`
	Name        string    `json:"name,omitempty"`
	Body        string    `json:"body,omitempty"`
	PublishedAt time.Time `json:"published_at"`
}
