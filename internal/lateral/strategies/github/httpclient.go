package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// HTTPAPIClient is a thin reference APIClient that talks to a
// GitHub-compatible REST surface via the package-local Fetcher
// interface. Daemon wiring may use this directly (paired with a
// Fetcher backed by net/http or ibr) or supply its own SDK-backed
// adapter. The reference exists to:
//
//  1. Give the package a self-contained APIClient for cassette tests
//     (T-0266) without dragging in a vendor SDK.
//  2. Document the canonical mapping from GitHub's REST endpoints to
//     the APIClient interface.
//
// BaseURL defaults to "https://api.github.com" when empty. Tests
// override it to point at httptest.Server URLs (the "cassette" pattern;
// when xrr lands the same wiring switches to xrr-replay without
// changing the strategy code).
type HTTPAPIClient struct {
	Fetcher Fetcher
	BaseURL string

	// snapshotMu guards lastSnapshot. Fetcher concurrency is permitted
	// (probes fan out sub-paths), so reads via RateSnapshot may race
	// against writes from rateAwareGet.
	snapshotMu   sync.RWMutex
	lastSnapshot RateSnapshot
}

// NewHTTPAPIClient constructs the reference client.
func NewHTTPAPIClient(f Fetcher, baseURL string) *HTTPAPIClient {
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}
	return &HTTPAPIClient{Fetcher: f, BaseURL: strings.TrimRight(baseURL, "/")}
}

// fetchJSON performs a GET, parses rate-limit headers from the
// response, and JSON-decodes Body into out. Returns the raw FetchResult
// for callers that need status/content-type. Does not gate on rate
// limits itself (guardedAPI handles gating).
func (c *HTTPAPIClient) fetchJSON(ctx context.Context, path string, out any) error {
	if c.Fetcher == nil {
		return fmt.Errorf("github: no Fetcher wired")
	}
	url := c.BaseURL + path
	res, err := c.Fetcher.Get(ctx, url)
	if err != nil {
		return err
	}
	if res.Status >= 300 {
		return fmt.Errorf("github: %s status %d", path, res.Status)
	}
	if out != nil && len(res.Body) > 0 {
		if err := json.Unmarshal(res.Body, out); err != nil {
			return fmt.Errorf("github: %s decode: %w", path, err)
		}
	}
	return nil
}

// rateAwareGet wraps fetchJSON and records the rate-limit snapshot
// implied by the last fetch's headers when the underlying Fetcher
// surfaces them. Plain FetchResult doesn't carry headers; T-0266 keeps
// rate-limit observation behind a Fetcher capability flag — callers
// that need rate-limit visibility wrap their Fetcher to also implement
// HeaderFetcher (defined below) and HTTPAPIClient detects it.
func (c *HTTPAPIClient) rateAwareGet(ctx context.Context, path string, out any) error {
	err := c.fetchJSON(ctx, path, out)
	// If the Fetcher surfaces headers, record the snapshot.
	if hf, ok := c.Fetcher.(HeaderFetcher); ok {
		if h, herr := hf.LastResponseHeaders(); herr == nil {
			snap := parseRateHeaders(h)
			c.snapshotMu.Lock()
			c.lastSnapshot = snap
			c.snapshotMu.Unlock()
		}
	}
	return err
}

// HeaderFetcher is an optional capability a Fetcher may implement to
// expose response headers from its last successful Get. Used by
// HTTPAPIClient to track rate-limit headers.
type HeaderFetcher interface {
	LastResponseHeaders() (http.Header, error)
}

// parseRateHeaders extracts a RateSnapshot from github rate-limit
// headers (X-RateLimit-Limit, X-RateLimit-Remaining, X-RateLimit-Reset).
// Returns the zero RateSnapshot when no useful headers are present.
func parseRateHeaders(h http.Header) RateSnapshot {
	if h == nil {
		return RateSnapshot{}
	}
	limit := h.Get("X-RateLimit-Limit")
	remaining := h.Get("X-RateLimit-Remaining")
	reset := h.Get("X-RateLimit-Reset")
	if limit == "" && remaining == "" && reset == "" {
		return RateSnapshot{}
	}
	s := RateSnapshot{ObservedAt: time.Now()}
	if v, err := strconv.Atoi(limit); err == nil {
		s.Limit = v
	}
	if v, err := strconv.Atoi(remaining); err == nil {
		s.Remaining = v
	}
	if v, err := strconv.ParseInt(reset, 10, 64); err == nil {
		s.ResetAt = time.Unix(v, 0).UTC()
	}
	return s
}

// RateSnapshot returns the last observed rate-limit snapshot.
func (c *HTTPAPIClient) RateSnapshot(_ context.Context) RateSnapshot {
	c.snapshotMu.RLock()
	defer c.snapshotMu.RUnlock()
	return c.lastSnapshot
}

// repoJSON is the minimal subset of /repos/{owner}/{repo} we decode.
type repoJSON struct {
	FullName    string `json:"full_name"`
	Name        string `json:"name"`
	HTMLURL     string `json:"html_url"`
	Description string `json:"description"`
	Stars       int    `json:"stargazers_count"`
	Language    string `json:"language"`
	Archived    bool   `json:"archived"`
	Fork        bool   `json:"fork"`
	Owner       struct {
		Login string `json:"login"`
		Type  string `json:"type"`
	} `json:"owner"`
}

func (r repoJSON) toSummary() RepoSummary {
	return RepoSummary{
		Owner:       r.Owner.Login,
		Name:        r.Name,
		URL:         r.HTMLURL,
		Description: r.Description,
		Stars:       r.Stars,
		Language:    r.Language,
		Archived:    r.Archived,
		Fork:        r.Fork,
	}
}

// ListRepoSiblings: GET /users/{login}/repos. exclude is filtered
// client-side.
func (c *HTTPAPIClient) ListRepoSiblings(ctx context.Context, owner, exclude string) ([]RepoSummary, error) {
	var raw []repoJSON
	if err := c.rateAwareGet(ctx, "/users/"+owner+"/repos?per_page=100", &raw); err != nil {
		return nil, err
	}
	out := make([]RepoSummary, 0, len(raw))
	for _, r := range raw {
		if r.Name == exclude {
			continue
		}
		out = append(out, r.toSummary())
	}
	return out, nil
}

// ListOwnerStarred: GET /users/{login}/starred.
func (c *HTTPAPIClient) ListOwnerStarred(ctx context.Context, login string) ([]RepoSummary, error) {
	var raw []repoJSON
	if err := c.rateAwareGet(ctx, "/users/"+login+"/starred?per_page=30", &raw); err != nil {
		return nil, err
	}
	out := make([]RepoSummary, 0, len(raw))
	for _, r := range raw {
		out = append(out, r.toSummary())
	}
	return out, nil
}

// ListOwnerPinned: github's REST API has no pinned-items endpoint.
// The reference impl falls back to scraping the profile page; the
// Fetcher's Get is expected to return HTML for the profile URL. v1
// keeps this simple and returns the raw response unparsed — daemon
// adapters with GraphQL access override this method directly.
//
// Returns nil, nil to indicate "no signal" rather than failing the
// caller; the strategy treats empty + nil error as a successful fetch
// with no candidates.
func (c *HTTPAPIClient) ListOwnerPinned(_ context.Context, _ string) ([]RepoSummary, error) {
	return nil, nil
}

// HasSponsorPage probes the sponsor page URL. github's API requires
// GraphQL for sponsor metadata; a simple HTML HEAD/GET is the v1 path.
// Reference impl issues a GET via the Fetcher and treats 200 OK as
// "has sponsor page". 404 returns false, nil; other status codes
// return the error.
func (c *HTTPAPIClient) HasSponsorPage(ctx context.Context, login string) (bool, error) {
	if c.Fetcher == nil {
		return false, fmt.Errorf("github: no Fetcher wired")
	}
	res, err := c.Fetcher.Get(ctx, "https://github.com/sponsors/"+login)
	if err != nil {
		return false, err
	}
	switch {
	case res.Status == 200:
		return true, nil
	case res.Status == 404:
		return false, nil
	default:
		return false, fmt.Errorf("github: sponsors/%s status %d", login, res.Status)
	}
}

// pullRequestJSON is the minimal subset of a PR list response.
type pullRequestJSON struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	HTMLURL string `json:"html_url"`
	State   string `json:"state"`
	Base    struct {
		Repo struct {
			Name  string `json:"name"`
			Owner struct {
				Login string `json:"login"`
			} `json:"owner"`
		} `json:"repo"`
	} `json:"base"`
	User struct {
		Login string `json:"login"`
	} `json:"user"`
}

func (p pullRequestJSON) toSummary() PullRequestSummary {
	return PullRequestSummary{
		Owner:  p.Base.Repo.Owner.Login,
		Repo:   p.Base.Repo.Name,
		Number: p.Number,
		Title:  p.Title,
		URL:    p.HTMLURL,
		State:  p.State,
		Author: p.User.Login,
	}
}

// ListAuthoredPRs: GET /search/issues?q=author:{login}+type:pr.
func (c *HTTPAPIClient) ListAuthoredPRs(ctx context.Context, login string, limit int) ([]PullRequestSummary, error) {
	if limit <= 0 {
		limit = 25
	}
	var raw struct {
		Items []pullRequestJSON `json:"items"`
	}
	if err := c.rateAwareGet(ctx,
		"/search/issues?q=author:"+login+"+type:pr&per_page="+strconv.Itoa(limit),
		&raw); err != nil {
		return nil, err
	}
	out := make([]PullRequestSummary, 0, len(raw.Items))
	for _, p := range raw.Items {
		out = append(out, p.toSummary())
	}
	return out, nil
}

// userJSON is the minimal subset of a user response.
type userJSON struct {
	Login   string `json:"login"`
	HTMLURL string `json:"html_url"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Company string `json:"company"`
}

func (u userJSON) toSummary() UserSummary {
	return UserSummary{
		Login:   u.Login,
		URL:     u.HTMLURL,
		Type:    u.Type,
		Name:    u.Name,
		Company: u.Company,
	}
}

// ListPRReviewers: GET /repos/{owner}/{repo}/pulls/{n}/requested_reviewers.
// github returns {users: [...], teams: [...]}. v1 surfaces users only.
func (c *HTTPAPIClient) ListPRReviewers(ctx context.Context, owner, repo string, number int) ([]UserSummary, error) {
	var raw struct {
		Users []userJSON `json:"users"`
	}
	if err := c.rateAwareGet(ctx,
		"/repos/"+owner+"/"+repo+"/pulls/"+strconv.Itoa(number)+"/requested_reviewers",
		&raw); err != nil {
		return nil, err
	}
	out := make([]UserSummary, 0, len(raw.Users))
	for _, u := range raw.Users {
		out = append(out, u.toSummary())
	}
	return out, nil
}

// issueJSON is the minimal subset of an issue list response.
type issueJSON struct {
	Number        int    `json:"number"`
	Title         string `json:"title"`
	HTMLURL       string `json:"html_url"`
	State         string `json:"state"`
	RepositoryURL string `json:"repository_url"`
	User          struct {
		Login string `json:"login"`
	} `json:"user"`
}

func (i issueJSON) toSummary() IssueSummary {
	owner, repo := splitRepoURL(i.RepositoryURL)
	return IssueSummary{
		Owner:  owner,
		Repo:   repo,
		Number: i.Number,
		Title:  i.Title,
		URL:    i.HTMLURL,
		State:  i.State,
		Author: i.User.Login,
	}
}

// splitRepoURL extracts (owner, repo) from a github API repository_url
// of the form https://api.github.com/repos/{owner}/{repo}. Returns
// empty strings on malformed input.
func splitRepoURL(u string) (string, string) {
	const marker = "/repos/"
	i := strings.Index(u, marker)
	if i < 0 {
		return "", ""
	}
	rest := u[i+len(marker):]
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) < 2 {
		return "", ""
	}
	return parts[0], parts[1]
}

// ListAuthoredIssues: GET /search/issues?q=author:{login}+type:issue.
func (c *HTTPAPIClient) ListAuthoredIssues(ctx context.Context, login string, limit int) ([]IssueSummary, error) {
	if limit <= 0 {
		limit = 25
	}
	var raw struct {
		Items []issueJSON `json:"items"`
	}
	if err := c.rateAwareGet(ctx,
		"/search/issues?q=author:"+login+"+type:issue&per_page="+strconv.Itoa(limit),
		&raw); err != nil {
		return nil, err
	}
	out := make([]IssueSummary, 0, len(raw.Items))
	for _, i := range raw.Items {
		out = append(out, i.toSummary())
	}
	return out, nil
}

// labelJSON is the minimal subset of a label payload.
type labelJSON struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	Description string `json:"description"`
	Color       string `json:"color"`
}

func (l labelJSON) toSummary() LabelSummary {
	return LabelSummary{
		Name:        l.Name,
		URL:         l.URL,
		Description: l.Description,
		Color:       l.Color,
	}
}

// ListIssueLabels: GET /repos/{owner}/{repo}/issues/{n}/labels.
func (c *HTTPAPIClient) ListIssueLabels(ctx context.Context, owner, repo string, number int) ([]LabelSummary, error) {
	var raw []labelJSON
	if err := c.rateAwareGet(ctx,
		"/repos/"+owner+"/"+repo+"/issues/"+strconv.Itoa(number)+"/labels",
		&raw); err != nil {
		return nil, err
	}
	out := make([]LabelSummary, 0, len(raw))
	for _, l := range raw {
		out = append(out, l.toSummary())
	}
	return out, nil
}

// ListSponsored: requires GraphQL for full coverage; reference impl
// returns nil, nil so callers know the source has no signal here. v1
// daemon adapters with GraphQL access override directly.
func (c *HTTPAPIClient) ListSponsored(_ context.Context, _ string) ([]UserSummary, error) {
	return nil, nil
}

// ListContributionOrgs: GET /users/{login}/orgs.
func (c *HTTPAPIClient) ListContributionOrgs(ctx context.Context, login string) ([]OrgSummary, error) {
	var raw []struct {
		Login string `json:"login"`
		URL   string `json:"html_url"`
		Name  string `json:"name"`
	}
	if err := c.rateAwareGet(ctx, "/users/"+login+"/orgs?per_page=30", &raw); err != nil {
		return nil, err
	}
	out := make([]OrgSummary, 0, len(raw))
	for _, o := range raw {
		out = append(out, OrgSummary{Login: o.Login, URL: o.URL, Name: o.Name})
	}
	return out, nil
}

// ListSimilarSponsors: requires graph traversal of sponsorship edges.
// Reference impl returns nil, nil; daemon GraphQL adapters override.
func (c *HTTPAPIClient) ListSimilarSponsors(_ context.Context, _ string) ([]UserSummary, error) {
	return nil, nil
}

// gistJSON is the minimal subset of a gist payload.
type gistJSON struct {
	ID          string `json:"id"`
	HTMLURL     string `json:"html_url"`
	Description string `json:"description"`
	Owner       struct {
		Login string `json:"login"`
	} `json:"owner"`
	Files map[string]any `json:"files"`
}

func (g gistJSON) toSummary() GistSummary {
	return GistSummary{
		ID:          g.ID,
		URL:         g.HTMLURL,
		Description: g.Description,
		Owner:       g.Owner.Login,
		Files:       len(g.Files),
	}
}

// ListOwnerGists: GET /users/{login}/gists.
func (c *HTTPAPIClient) ListOwnerGists(ctx context.Context, login string) ([]GistSummary, error) {
	var raw []gistJSON
	if err := c.rateAwareGet(ctx, "/users/"+login+"/gists?per_page=30", &raw); err != nil {
		return nil, err
	}
	out := make([]GistSummary, 0, len(raw))
	for _, g := range raw {
		out = append(out, g.toSummary())
	}
	return out, nil
}

// advisoryJSON is the minimal subset of /advisories.
type advisoryJSON struct {
	GHSAID          string `json:"ghsa_id"`
	HTMLURL         string `json:"html_url"`
	Summary         string `json:"summary"`
	Severity        string `json:"severity"`
	Vulnerabilities []struct {
		Package struct {
			Ecosystem string `json:"ecosystem"`
			Name      string `json:"name"`
		} `json:"package"`
	} `json:"vulnerabilities"`
}

func (a advisoryJSON) toSummary() AdvisorySummary {
	out := AdvisorySummary{
		GHSAID:   a.GHSAID,
		URL:      a.HTMLURL,
		Summary:  a.Summary,
		Severity: a.Severity,
	}
	if len(a.Vulnerabilities) > 0 {
		out.Ecosystem = a.Vulnerabilities[0].Package.Ecosystem
		out.PackageName = a.Vulnerabilities[0].Package.Name
	}
	return out
}

// ListGlobalAdvisories: GET /advisories. ecosystem/severity are query
// filters when non-empty.
func (c *HTTPAPIClient) ListGlobalAdvisories(ctx context.Context, ecosystem, severity string, limit int) ([]AdvisorySummary, error) {
	if limit <= 0 {
		limit = 10
	}
	q := "?per_page=" + strconv.Itoa(limit)
	if ecosystem != "" {
		q += "&ecosystem=" + ecosystem
	}
	if severity != "" {
		q += "&severity=" + severity
	}
	var raw []advisoryJSON
	if err := c.rateAwareGet(ctx, "/advisories"+q, &raw); err != nil {
		return nil, err
	}
	out := make([]AdvisorySummary, 0, len(raw))
	for _, a := range raw {
		out = append(out, a.toSummary())
	}
	return out, nil
}
