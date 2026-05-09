package github

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
)

// fixtureFetcher is a Fetcher that pulls bytes from an in-memory
// fixtures map keyed by URL. Substitutes for the eventual xrr cassette
// replay path; when xrr lands the same Fetcher interface lets the
// strategy switch transparently. Each cassette test below installs a
// fixtureFetcher with the URLs and JSON bodies it expects to see.
type fixtureFetcher struct {
	mu          chan struct{}
	fixtures    map[string][]byte
	statuses    map[string]int
	calls       []string
	lastHeaders http.Header
}

func newFixtureFetcher() *fixtureFetcher {
	return &fixtureFetcher{
		mu:       make(chan struct{}, 1),
		fixtures: map[string][]byte{},
		statuses: map[string]int{},
	}
}

func (f *fixtureFetcher) lock()   { f.mu <- struct{}{} }
func (f *fixtureFetcher) unlock() { <-f.mu }

func (f *fixtureFetcher) Get(_ context.Context, url string) (FetchResult, error) {
	f.lock()
	defer f.unlock()
	f.calls = append(f.calls, url)
	body, ok := f.fixtures[url]
	if !ok {
		return FetchResult{Status: 404}, nil
	}
	status := 200
	if s, ok := f.statuses[url]; ok {
		status = s
	}
	return FetchResult{Body: body, Status: status, ContentType: "application/json"}, nil
}

// fixturesServer spins up an httptest.Server that returns canned
// payloads keyed by request URL (path + query). The real github API
// surface stays out of the picture; the test owns every byte. This is
// the cassette pattern in stub form — when xrr lands the same
// HTTPAPIClient is fronted by an xrr-recorded Fetcher.
func fixturesServer(t *testing.T, fixtures map[string]string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.RequestURI()
		body, ok := fixtures[key]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		// Add github rate-limit headers so HTTPAPIClient observes a
		// non-zero RateSnapshot under the cassette.
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", "4998")
		w.Header().Set("X-RateLimit-Reset", "1747999999")
		_, _ = io.WriteString(w, body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// httpFetcher is a Fetcher backed by net/http for the cassette tests.
type httpFetcher struct {
	client     *http.Client
	lastHeader http.Header
}

func newHTTPFetcher() *httpFetcher {
	return &httpFetcher{client: &http.Client{}}
}

func (f *httpFetcher) Get(ctx context.Context, url string) (FetchResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return FetchResult{}, err
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return FetchResult{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return FetchResult{}, err
	}
	f.lastHeader = resp.Header
	return FetchResult{
		Body:        body,
		Status:      resp.StatusCode,
		ContentType: resp.Header.Get("Content-Type"),
	}, nil
}

// LastResponseHeaders implements HeaderFetcher so HTTPAPIClient can
// observe rate-limit headers under the cassette.
func (f *httpFetcher) LastResponseHeaders() (http.Header, error) {
	return f.lastHeader, nil
}

func TestCassette_RepoSiblings(t *testing.T) {
	srv := fixturesServer(t, map[string]string{
		"/users/samber/repos?per_page=100": `[
			{"name":"lo","html_url":"https://github.com/samber/lo","stargazers_count":12000,"owner":{"login":"samber","type":"User"}},
			{"name":"do","html_url":"https://github.com/samber/do","stargazers_count":1500,"owner":{"login":"samber","type":"User"}}
		]`,
	})
	c := NewHTTPAPIClient(newHTTPFetcher(), srv.URL)
	got, err := c.ListRepoSiblings(context.Background(), "samber", "lo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Name != "do" {
		t.Errorf("got %+v, want [do]", got)
	}
	// Snapshot observed under cassette.
	snap := c.RateSnapshot(context.Background())
	if snap.Limit != 5000 || snap.Remaining != 4998 {
		t.Errorf("RateSnapshot = %+v, want Limit=5000 Remaining=4998", snap)
	}
}

func TestCassette_OwnerStarred(t *testing.T) {
	srv := fixturesServer(t, map[string]string{
		"/users/jadb/starred?per_page=30": `[
			{"name":"thanos","html_url":"https://github.com/thanos-io/thanos","owner":{"login":"thanos-io","type":"Organization"}}
		]`,
	})
	c := NewHTTPAPIClient(newHTTPFetcher(), srv.URL)
	got, err := c.ListOwnerStarred(context.Background(), "jadb")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Name != "thanos" {
		t.Errorf("got %+v, want [thanos]", got)
	}
}

func TestCassette_HasSponsorPage_200(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/sponsors/jadb", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	// Fetcher overridden to point at the test server (only for sponsor
	// URL; the HTTPAPIClient prefixes its other paths with BaseURL).
	f := &redirectingFetcher{realBase: srv.URL, inner: newHTTPFetcher()}
	c := NewHTTPAPIClient(f, srv.URL)
	got, err := c.HasSponsorPage(context.Background(), "jadb")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got {
		t.Error("HasSponsorPage = false, want true")
	}
}

func TestCassette_HasSponsorPage_404(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/sponsors/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	f := &redirectingFetcher{realBase: srv.URL, inner: newHTTPFetcher()}
	c := NewHTTPAPIClient(f, srv.URL)
	got, err := c.HasSponsorPage(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got {
		t.Error("HasSponsorPage = true, want false (404)")
	}
}

func TestCassette_AuthoredPRs(t *testing.T) {
	srv := fixturesServer(t, map[string]string{
		"/search/issues?q=author:jadb+type:pr&per_page=25": `{"items":[
			{"number":42,"title":"fix typo","html_url":"https://github.com/x/y/pull/42","state":"open",
			 "base":{"repo":{"name":"y","owner":{"login":"x"}}},
			 "user":{"login":"jadb"}}
		]}`,
	})
	c := NewHTTPAPIClient(newHTTPFetcher(), srv.URL)
	got, err := c.ListAuthoredPRs(context.Background(), "jadb", 25)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Owner != "x" || got[0].Repo != "y" || got[0].Number != 42 {
		t.Errorf("got %+v", got)
	}
}

func TestCassette_PRReviewers(t *testing.T) {
	srv := fixturesServer(t, map[string]string{
		"/repos/x/y/pulls/42/requested_reviewers": `{"users":[
			{"login":"alice","html_url":"https://github.com/alice","type":"User"}
		]}`,
	})
	c := NewHTTPAPIClient(newHTTPFetcher(), srv.URL)
	got, err := c.ListPRReviewers(context.Background(), "x", "y", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Login != "alice" {
		t.Errorf("got %+v, want [alice]", got)
	}
}

func TestCassette_AuthoredIssues(t *testing.T) {
	srv := fixturesServer(t, map[string]string{
		"/search/issues?q=author:jadb+type:issue&per_page=25": `{"items":[
			{"number":7,"title":"flake","html_url":"https://github.com/x/y/issues/7","state":"closed",
			 "repository_url":"https://api.github.com/repos/x/y","user":{"login":"jadb"}}
		]}`,
	})
	c := NewHTTPAPIClient(newHTTPFetcher(), srv.URL)
	got, err := c.ListAuthoredIssues(context.Background(), "jadb", 25)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Owner != "x" || got[0].Repo != "y" {
		t.Errorf("got %+v", got)
	}
}

func TestCassette_IssueLabels(t *testing.T) {
	srv := fixturesServer(t, map[string]string{
		"/repos/x/y/issues/7/labels": `[
			{"name":"bug","color":"ff0000"},
			{"name":"good first issue","color":"00ff00"}
		]`,
	})
	c := NewHTTPAPIClient(newHTTPFetcher(), srv.URL)
	got, err := c.ListIssueLabels(context.Background(), "x", "y", 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %d labels, want 2", len(got))
	}
}

func TestCassette_ContributionOrgs(t *testing.T) {
	srv := fixturesServer(t, map[string]string{
		"/users/jadb/orgs?per_page=30": `[
			{"login":"ideacrafterslabs","html_url":"https://github.com/ideacrafterslabs","name":"IdeaCrafters Labs"}
		]`,
	})
	c := NewHTTPAPIClient(newHTTPFetcher(), srv.URL)
	got, err := c.ListContributionOrgs(context.Background(), "jadb")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Login != "ideacrafterslabs" {
		t.Errorf("got %+v", got)
	}
}

func TestCassette_OwnerGists(t *testing.T) {
	srv := fixturesServer(t, map[string]string{
		"/users/jadb/gists?per_page=30": `[
			{"id":"abc","html_url":"https://gist.github.com/jadb/abc","description":"x","owner":{"login":"jadb"},"files":{"a.go":1,"b.go":1}}
		]`,
	})
	c := NewHTTPAPIClient(newHTTPFetcher(), srv.URL)
	got, err := c.ListOwnerGists(context.Background(), "jadb")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].ID != "abc" || got[0].Files != 2 {
		t.Errorf("got %+v", got)
	}
}

func TestCassette_GlobalAdvisories(t *testing.T) {
	srv := fixturesServer(t, map[string]string{
		"/advisories?per_page=10": `[
			{"ghsa_id":"GHSA-aaa","html_url":"https://github.com/advisories/GHSA-aaa","summary":"x","severity":"high",
			 "vulnerabilities":[{"package":{"ecosystem":"go","name":"hop.top/kit"}}]}
		]`,
	})
	c := NewHTTPAPIClient(newHTTPFetcher(), srv.URL)
	got, err := c.ListGlobalAdvisories(context.Background(), "", "", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].GHSAID != "GHSA-aaa" || got[0].Ecosystem != "go" {
		t.Errorf("got %+v", got)
	}
}

func TestCassette_EndToEnd_RepoProbe_AllSubpathsExercised(t *testing.T) {
	srv := fixturesServer(t, map[string]string{
		"/users/samber/repos?per_page=100": `[
			{"name":"do","html_url":"https://github.com/samber/do","owner":{"login":"samber"}}
		]`,
		"/users/samber/starred?per_page=30": `[
			{"name":"thanos","html_url":"https://github.com/thanos-io/thanos","owner":{"login":"thanos-io","type":"Organization"}}
		]`,
	})
	mux := http.NewServeMux()
	mux.HandleFunc("/sponsors/samber", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.RequestURI()
		// The fixturesServer mux already handled the API paths; this
		// fallback supports the parallel /sponsors path.
		http.Error(w, key, http.StatusNotFound)
	})
	sponSrv := httptest.NewServer(mux)
	t.Cleanup(sponSrv.Close)

	// Compose: API calls go through srv.URL; HasSponsorPage queries the
	// sponsorSrv. The redirectingFetcher routes by host substring.
	f := &composedFetcher{
		baseAPI:    srv.URL,
		baseSponsor: sponSrv.URL,
		inner:       newHTTPFetcher(),
	}
	c := NewHTTPAPIClient(f, srv.URL)
	s := NewGitHubStrategy(Dependencies{APIClient: c})
	got, err := s.Probe(context.Background(),
		lateral.CapturedEvent{SourceURL: "https://github.com/samber/lo"},
		lateral.ActiveContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{TypeSiblingRepo, TypeOwnerProfile, TypeSponsorPage, TypeStarredRepo} {
		if !hasType(got, want) {
			t.Errorf("missing %q in %v", want, candidateTypes(got))
		}
	}
}

// redirectingFetcher routes all requests through realBase regardless of
// the URL passed in. Used by the sponsor-page tests where the strategy
// hardcodes "https://github.com/sponsors/<login>" but we need to redirect
// to the test server.
type redirectingFetcher struct {
	realBase string
	inner    *httpFetcher
}

func (r *redirectingFetcher) Get(ctx context.Context, url string) (FetchResult, error) {
	// Replace the real github.com prefix with the test server.
	url = strings.Replace(url, "https://github.com", r.realBase, 1)
	url = strings.Replace(url, "https://api.github.com", r.realBase, 1)
	return r.inner.Get(ctx, url)
}

func (r *redirectingFetcher) LastResponseHeaders() (http.Header, error) {
	return r.inner.LastResponseHeaders()
}

// composedFetcher routes API calls to baseAPI and sponsor pages to
// baseSponsor. Used in the end-to-end repo-probe cassette.
type composedFetcher struct {
	baseAPI     string
	baseSponsor string
	inner       *httpFetcher
}

func (c *composedFetcher) Get(ctx context.Context, url string) (FetchResult, error) {
	if strings.HasPrefix(url, "https://github.com/sponsors/") {
		url = strings.Replace(url, "https://github.com", c.baseSponsor, 1)
	} else if strings.HasPrefix(url, "https://api.github.com") {
		url = strings.Replace(url, "https://api.github.com", c.baseAPI, 1)
	} else if strings.HasPrefix(url, c.baseAPI) {
		// already targeted
	} else if strings.HasPrefix(url, "https://github.com") {
		url = strings.Replace(url, "https://github.com", c.baseAPI, 1)
	}
	return c.inner.Get(ctx, url)
}

func (c *composedFetcher) LastResponseHeaders() (http.Header, error) {
	return c.inner.LastResponseHeaders()
}
