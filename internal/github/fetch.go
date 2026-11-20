package github

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"
)

// FetchOption tunes what FetchRepo gathers beyond the repository record.
type FetchOption func(*fetchOpts)

type fetchOpts struct {
	readmeMax int
	releases  int
}

// WithREADME also fetches the README (raw text, truncated to maxBytes).
// A repository without a README is not an error.
func WithREADME(maxBytes int) FetchOption { return func(o *fetchOpts) { o.readmeMax = maxBytes } }

// WithReleases also fetches the n newest releases (n <= 100).
func WithReleases(n int) FetchOption { return func(o *fetchOpts) { o.releases = n } }

type apiRepo struct {
	FullName      string    `json:"full_name"`
	HTMLURL       string    `json:"html_url"`
	Description   string    `json:"description"`
	Language      string    `json:"language"`
	Topics        []string  `json:"topics"`
	Homepage      string    `json:"homepage"`
	Stars         int       `json:"stargazers_count"`
	Forks         int       `json:"forks_count"`
	OpenIssues    int       `json:"open_issues_count"`
	Archived      bool      `json:"archived"`
	Fork          bool      `json:"fork"`
	DefaultBranch string    `json:"default_branch"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	PushedAt      time.Time `json:"pushed_at"`
	License       *struct {
		SpdxID string `json:"spdx_id"`
		Name   string `json:"name"`
	} `json:"license"`
	Owner struct {
		Login string `json:"login"`
	} `json:"owner"`
	Name string `json:"name"`
}

type apiRelease struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	PublishedAt time.Time `json:"published_at"`
}

// FetchRepo returns the snapshot of owner/name: one request for the
// repository record (description, topics, language, stars, license,
// timestamps, flags), plus one per option asked for. Identity fields carry
// GitHub's canonical casing, not the caller's.
func (c *Client) FetchRepo(ctx context.Context, owner, name string, opts ...FetchOption) (*RepoSnapshot, error) {
	var o fetchOpts
	for _, f := range opts {
		f(&o)
	}
	if owner == "" || name == "" {
		return nil, fmt.Errorf("github: FetchRepo: owner and name required")
	}
	var r apiRepo
	if err := c.getJSON(ctx, owner, "/repos/"+escapeSegments(owner, name), &r); err != nil {
		return nil, err
	}
	snap := &RepoSnapshot{
		Owner:         r.Owner.Login,
		Name:          r.Name,
		FullName:      r.FullName,
		URL:           r.HTMLURL,
		Description:   r.Description,
		Language:      r.Language,
		Topics:        r.Topics,
		Homepage:      r.Homepage,
		Stars:         r.Stars,
		Forks:         r.Forks,
		OpenIssues:    r.OpenIssues,
		IsArchived:    r.Archived,
		IsFork:        r.Fork,
		DefaultBranch: r.DefaultBranch,
		CreatedAt:     r.CreatedAt,
		UpdatedAt:     r.UpdatedAt,
		PushedAt:      r.PushedAt,
	}
	if snap.Owner == "" {
		snap.Owner = owner
	}
	if snap.Name == "" {
		snap.Name = name
	}
	if snap.FullName == "" {
		snap.FullName = snap.Owner + "/" + snap.Name
	}
	if snap.URL == "" {
		snap.URL = "https://github.com/" + snap.FullName
	}
	if r.License != nil {
		snap.License = r.License.SpdxID
		if snap.License == "" || snap.License == "NOASSERTION" {
			snap.License = r.License.Name
		}
	}

	if o.readmeMax > 0 {
		body, err := c.get(ctx, owner, "/repos/"+escapeSegments(owner, name)+"/readme", "application/vnd.github.raw+json")
		switch {
		case err == nil:
			snap.README = truncateUTF8(string(body), o.readmeMax)
		case errors.Is(err, ErrNotFound):
			// no README
		default:
			return nil, err
		}
	}
	if o.releases > 0 {
		var rels []apiRelease
		path := fmt.Sprintf("/repos/%s/releases?per_page=%d", escapeSegments(owner, name), min(o.releases, 100))
		if err := c.getJSON(ctx, owner, path, &rels); err != nil {
			return nil, err
		}
		for _, rel := range rels {
			snap.Releases = append(snap.Releases, Release{Tag: rel.TagName, Name: rel.Name, Body: rel.Body, PublishedAt: rel.PublishedAt})
		}
	}
	return snap, nil
}

type apiIssue struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	State   string `json:"state"`
	HTMLURL string `json:"html_url"`
	Labels  []struct {
		Name string `json:"name"`
	} `json:"labels"`
	User struct {
		Login string `json:"login"`
	} `json:"user"`
	Assignees []struct {
		Login string `json:"login"`
	} `json:"assignees"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	ClosedAt  *time.Time `json:"closed_at"`
}

// FetchIssue returns the snapshot of issue number in owner/repo. The
// endpoint also answers for pull requests of that number.
func (c *Client) FetchIssue(ctx context.Context, owner, repo string, number int) (*IssueSnapshot, error) {
	if owner == "" || repo == "" || number <= 0 {
		return nil, fmt.Errorf("github: FetchIssue: owner, repo and positive number required")
	}
	var i apiIssue
	path := fmt.Sprintf("/repos/%s/issues/%d", escapeSegments(owner, repo), number)
	if err := c.getJSON(ctx, owner, path, &i); err != nil {
		return nil, err
	}
	snap := &IssueSnapshot{
		Owner: owner, Repo: repo, Number: i.Number, URL: i.HTMLURL,
		Title: i.Title, Body: i.Body, State: i.State, Author: i.User.Login,
		CreatedAt: i.CreatedAt, UpdatedAt: i.UpdatedAt, ClosedAt: i.ClosedAt,
	}
	for _, l := range i.Labels {
		snap.Labels = append(snap.Labels, l.Name)
	}
	for _, a := range i.Assignees {
		snap.Assignees = append(snap.Assignees, a.Login)
	}
	return snap, nil
}

// truncateUTF8 cuts s to at most max bytes without splitting a rune.
func truncateUTF8(s string, max int) string {
	if len(s) <= max {
		return s
	}
	for max > 0 && !utf8.RuneStart(s[max]) {
		max--
	}
	return s[:max]
}
