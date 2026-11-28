package steps

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/github"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

const repoJSON = `{"name":"Hello-World","full_name":"octocat/Hello-World","owner":{"login":"octocat"},
"html_url":"https://github.com/octocat/Hello-World","description":"My first repository",
"language":"Go","topics":["Octocat","atom"],"homepage":"https://github.com","stargazers_count":80,
"forks_count":9,"open_issues_count":2,"archived":true,"fork":false,"default_branch":"master",
"created_at":"2011-01-26T19:01:12Z","updated_at":"2024-05-01T10:00:00Z","pushed_at":"2024-04-30T09:00:00Z",
"license":{"spdx_id":"MIT","name":"MIT License"}}`

// githubAPI serves repoJSON (stars adjustable) and a README.
func githubAPI(t *testing.T, stars *int, hits *[]string) *github.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits != nil {
			*hits = append(*hits, r.URL.Path)
		}
		switch strings.ToLower(r.URL.Path) {
		case "/repos/octocat/hello-world":
			body := repoJSON
			if stars != nil {
				body = strings.Replace(body, `"stargazers_count":80`, fmt.Sprintf(`"stargazers_count":%d`, *stars), 1)
			}
			fmt.Fprint(w, body)
		case "/repos/octocat/hello-world/readme":
			fmt.Fprint(w, "Hello README body")
		case "/repos/octocat/missing":
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		case "/repos/octocat/limited":
			w.Header().Set("Retry-After", "3600")
			http.Error(w, `{"message":"secondary rate limit"}`, http.StatusTooManyRequests)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return github.NewClient(github.WithBaseURL(srv.URL), github.WithMaxWait(0), github.WithMaxRetries(0))
}

func runRepoSteps(t *testing.T, c *github.Client, source string) *storage.KnowledgeObject {
	t.Helper()
	d := &storage.KnowledgeObject{Source: source, RawContent: source}
	for _, s := range []pipeline.PipelineStep{NewGitHubRepoFetcher(c), NewGitHubRepoMapper()} {
		out, err := s.Run(context.Background(), d)
		if err != nil {
			t.Fatalf("%s: %v", s.Name(), err)
		}
		d = out
	}
	return d
}

func TestGitHubRepoContracts(t *testing.T) {
	f := NewGitHubRepoFetcher(nil)
	assertRequires(t, "github_repo_fetcher", f.Contract(), []string{"Source"})
	assertProduces(t, "github_repo_fetcher", f.Contract(), []string{"Metadata"})
	assertCapabilities(t, "github_repo_fetcher", f.Contract(), nil)
	m := NewGitHubRepoMapper()
	assertRequires(t, "github_repo_mapper", m.Contract(), []string{"Metadata"})
	assertProduces(t, "github_repo_mapper", m.Contract(), []string{"Type", "Subtype", "RawContent", "ContentType", "Source", "Metadata", "Tags"})
	assertCapabilities(t, "github_repo_mapper", m.Contract(), nil)
	if f.Name() != "github_repo_fetcher" || m.Name() != "github_repo_mapper" {
		t.Errorf("names = %q, %q", f.Name(), m.Name())
	}
}

func TestGitHubRepoFetchAndMap(t *testing.T) {
	d := runRepoSteps(t, githubAPI(t, nil, nil), "https://github.com/OctoCat/hello-world/")

	if d.Type != "repo" || d.Subtype != "github" {
		t.Errorf("type/subtype = %q/%q", d.Type, d.Subtype)
	}
	if d.Source != "https://github.com/octocat/Hello-World" {
		t.Errorf("Source = %q, want the API's canonical URL", d.Source)
	}
	for _, want := range []string{"# octocat/Hello-World", "My first repository", "Topics: Octocat, atom", "Hello README body"} {
		if !strings.Contains(d.RawContent, want) {
			t.Errorf("RawContent lacks %q:\n%s", want, d.RawContent)
		}
	}
	m := d.Metadata
	want := map[string]any{
		"forge": "github", "forge_id": "octocat/Hello-World", "repo_url": "https://github.com/octocat/Hello-World",
		"owner": "octocat", "name": "Hello-World", "description": "My first repository", "language": "Go",
		"license": "MIT", "homepage": "https://github.com", "stars": 80, "forks": 9, "open_issues": 2,
		"archived": true, "is_fork": false, "default_branch": "master",
		"pushed_at": "2024-04-30T09:00:00Z", "created_at": "2011-01-26T19:01:12Z", "updated_at": "2024-05-01T10:00:00Z",
	}
	for k, v := range want {
		if m[k] != v {
			t.Errorf("metadata[%q] = %#v, want %#v", k, m[k], v)
		}
	}
	if topics, _ := m["topics"].([]string); len(topics) != 2 || topics[0] != "Octocat" {
		t.Errorf("topics = %#v", m["topics"])
	}
	if _, left := m[MetaGitHubSnapshot]; left {
		t.Error("the snapshot must not persist in metadata")
	}
	labels := map[string]string{}
	for _, tag := range d.Tags {
		labels[tag.Label] = tag.Source
	}
	for _, l := range []string{"lang:go", "octocat", "atom", "archived"} {
		if labels[l] != "github" {
			t.Errorf("tag %q missing or wrong source; tags = %v", l, labels)
		}
	}
}

func TestGitHubRepoFetcher_ReadmeSkippedWhenZero(t *testing.T) {
	var hits []string
	c := githubAPI(t, nil, &hits)
	d := &storage.KnowledgeObject{Source: "https://github.com/octocat/Hello-World"}
	if _, err := NewGitHubRepoFetcher(c, WithGitHubREADMEBytes(0)).Run(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Errorf("requests = %v, want the repository call only", hits)
	}
}

func TestGitHubRepoFetcher_SourceFallsBackToRawContent(t *testing.T) {
	d := &storage.KnowledgeObject{Source: "cli", RawContent: "https://github.com/octocat/Hello-World"}
	if _, err := NewGitHubRepoFetcher(githubAPI(t, nil, nil)).Run(context.Background(), d); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestGitHubRepoFetcher_Errors(t *testing.T) {
	c := githubAPI(t, nil, nil)
	cases := []struct {
		name, source string
		permanent    bool
		is           error
	}{
		{"not a repo URL", "https://github.com/octocat/Hello-World/issues/1", true, nil},
		{"404", "https://github.com/octocat/missing", true, github.ErrNotFound},
		{"rate limited stays retryable", "https://github.com/octocat/limited", false, github.ErrRateLimited},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewGitHubRepoFetcher(c).Run(context.Background(), &storage.KnowledgeObject{Source: tc.source})
			if err == nil {
				t.Fatal("want an error")
			}
			var perm *pipeline.PermanentError
			if got := errors.As(err, &perm); got != tc.permanent {
				t.Errorf("permanent = %v, want %v (%v)", got, tc.permanent, err)
			}
			if tc.is != nil && !errors.Is(err, tc.is) {
				t.Errorf("err = %v, want %v", err, tc.is)
			}
		})
	}
}

func TestGitHubRepoMapper_NeedsSnapshot(t *testing.T) {
	_, err := NewGitHubRepoMapper().Run(context.Background(), &storage.KnowledgeObject{Metadata: map[string]any{}})
	var perm *pipeline.PermanentError
	if !errors.As(err, &perm) {
		t.Errorf("err = %v, want permanent", err)
	}
}

func TestGitHubRepoMapper_ReleasesAndZeroTimes(t *testing.T) {
	snap := &github.RepoSnapshot{Owner: "o", Name: "r", FullName: "o/r", URL: "https://github.com/o/r",
		Releases: []github.Release{{Tag: "v1", Name: "One", PublishedAt: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)}}}
	d, err := NewGitHubRepoMapper().Run(context.Background(), &storage.KnowledgeObject{
		Metadata: map[string]any{MetaGitHubSnapshot: snap}})
	if err != nil {
		t.Fatal(err)
	}
	rels, _ := d.Metadata["releases"].([]map[string]any)
	if len(rels) != 1 || rels[0]["tag"] != "v1" || rels[0]["published_at"] != "2024-01-02T00:00:00Z" {
		t.Errorf("releases = %#v", d.Metadata["releases"])
	}
	if _, ok := d.Metadata["pushed_at"]; ok {
		t.Error("a zero timestamp must not be written")
	}
}

// One object per repository: the dedup hash is the same however the URL is
// spelled and however the repository changed between submissions, so a
// re-submit reinforces; another repository hashes differently.
func TestGitHubRepoDedupHashIsPerRepository(t *testing.T) {
	stars := 80
	c := githubAPI(t, &stars, nil)
	hash := func(source string) string {
		d := runRepoSteps(t, c, source)
		return storageutil.DraftContentHash(d.RawContent, d.Source, d.Metadata)
	}
	first := hash("https://github.com/octocat/Hello-World")
	stars = 81
	if again := hash("https://github.com/octocat/hello-world/"); again != first {
		t.Error("re-submitting the repository (other spelling, more stars) must hash the same")
	}
	d := runRepoSteps(t, c, "https://github.com/octocat/Hello-World")
	if storageutil.ContentHash(d.RawContent, d.Source) == first {
		t.Error("control: a raw-content hash changes with the stars; the forge hash must not")
	}
}

// The tagger keeps the tags the mapper read from the API.
func TestTaggerKeepsGitHubTags(t *testing.T) {
	d := runRepoSteps(t, githubAPI(t, nil, nil), "https://github.com/octocat/Hello-World")
	out, err := NewTagger().Run(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tag := range out.Tags {
		if tag.Label == "lang:go" && tag.Source == "github" {
			found = true
		}
	}
	if !found {
		t.Errorf("tags = %+v, want lang:go from github kept", out.Tags)
	}
}

// The fetch failure says whether the host asked to slow down: GitHub's
// primary limit (a 403 with no requests remaining) and a 429 do; a plain 403
// and a 404 do not.
func TestGitHubRepoFetcher_FailuresSayWhetherTheyAreRateLimits(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		headers map[string]string
		limited bool
	}{
		{"primary limit", http.StatusForbidden, map[string]string{"X-RateLimit-Remaining": "0"}, true},
		{"429", http.StatusTooManyRequests, nil, true},
		{"plain 403", http.StatusForbidden, nil, false},
		{"404", http.StatusNotFound, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				for k, v := range tc.headers {
					w.Header().Set(k, v)
				}
				http.Error(w, `{"message":"no"}`, tc.status)
			}))
			defer srv.Close()
			c := github.NewClient(github.WithBaseURL(srv.URL), github.WithMaxWait(0), github.WithMaxRetries(0))
			_, err := NewGitHubRepoFetcher(c).Run(context.Background(), &storage.KnowledgeObject{Source: "https://github.com/octocat/Hello-World"})
			var fe *pipeline.FetchError
			if !errors.As(err, &fe) || fe.Status != tc.status || fe.RateLimited != tc.limited {
				t.Fatalf("err = %v, FetchError %+v, want status %d rate limited %v", err, fe, tc.status, tc.limited)
			}
		})
	}
}
