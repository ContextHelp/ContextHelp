package builtins

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/github"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

func TestURLGitHubRepoDetector(t *testing.T) {
	r := Registry()

	tests := []struct {
		url  string
		want string
		desc string
	}{
		{
			url:  "https://github.com/foo/bar",
			want: "url.github.repo",
			desc: "bare repo URL",
		},
		{
			url:  "https://github.com/foo/bar/",
			want: "url.github.repo",
			desc: "repo URL with trailing slash",
		},
		{
			url:  "https://github.com/foo/bar/issues/1",
			want: "url.github.issue",
			desc: "issue URL routes to url.github.issue (more specific)",
		},
		{
			url:  "https://github.com/foo/bar/pulls",
			want: "url.generic",
			desc: "pulls URL should NOT match url.github.repo",
		},
		{
			url:  "https://github.com/foo/bar/releases",
			want: "url.github.release",
			desc: "releases URL routes to url.github.release (more specific)",
		},
		{
			url:  "https://gitlab.com/foo/bar",
			want: "url.repo",
			desc: "gitlab URL should NOT match url.github.repo (routes to url.repo)",
		},
	}

	for _, tt := range tests {
		got := r.SelectPipeline(tt.url, "")
		if got != tt.want {
			t.Errorf("%s: SelectPipeline(%q) = %q, want %q", tt.desc, tt.url, got, tt.want)
		}
	}
}

func TestURLGitHubRepoPipelineRegistered(t *testing.T) {
	d := Defs()
	if _, ok := d["url.github.repo"]; !ok {
		t.Error("url.github.repo not found in defs")
	}
}

func TestURLGitHubRepoUsesAPISteps(t *testing.T) {
	d := Defs()["url.github.repo"]
	if len(d.Steps) < 2 || d.Steps[0] != "github_repo_fetcher" || d.Steps[1] != "github_repo_mapper" {
		t.Fatalf("steps = %v, want github_repo_fetcher then github_repo_mapper first", d.Steps)
	}
	for _, banned := range []string{"url_fetcher", "html_cleaner", "typedetector"} {
		if slices.Contains(d.Steps, banned) {
			t.Errorf("url.github.repo still runs %s", banned)
		}
	}
	if _, err := buildPipeline("url.github.repo", d, BuildOpts{}, false); err != nil {
		t.Fatalf("pipeline does not compose: %v", err)
	}
}

// A repo URL capture runs through the API steps to one typed object whose
// dedup hash names the repository.
func TestURLGitHubRepoPipelineEndToEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/foo/bar":
			fmt.Fprint(w, `{"name":"bar","full_name":"foo/bar","owner":{"login":"foo"},"html_url":"https://github.com/foo/bar",
			"description":"numbat tracker","language":"Go","topics":["wildlife"],"stargazers_count":5,"pushed_at":"2024-04-30T09:00:00Z",
			"license":{"spdx_id":"Apache-2.0"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p, err := buildPipeline("url.github.repo", Defs()["url.github.repo"],
		BuildOpts{GitHub: github.NewClient(github.WithBaseURL(srv.URL))}, false)
	if err != nil {
		t.Fatal(err)
	}
	draft := &storage.KnowledgeObject{ID: "x", Pipeline: "url.github.repo", Source: "https://github.com/foo/bar", RawContent: "https://github.com/foo/bar"}
	for _, s := range p.Steps {
		if draft, err = s.Run(context.Background(), draft); err != nil {
			t.Fatalf("step %s: %v", s.Name(), err)
		}
	}
	if draft.Type != "repo" || draft.Subtype != "github" {
		t.Errorf("type/subtype = %q/%q", draft.Type, draft.Subtype)
	}
	if draft.Metadata["stars"] != 5 || draft.Metadata["license"] != "Apache-2.0" || draft.Metadata["pushed_at"] != "2024-04-30T09:00:00Z" {
		t.Errorf("metadata = %v", draft.Metadata)
	}
	want := storageutil.ContentHash("forge:github:foo/bar", "https://github.com/foo/bar")
	if got := storageutil.DraftContentHash(draft.RawContent, draft.Source, draft.Metadata); got != want {
		t.Errorf("dedup hash = %s, want the repository's", got)
	}
}
