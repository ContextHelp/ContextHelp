package builtins

import (
	"testing"
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
			want: "url.generic",
			desc: "issue URL should NOT match url.github.repo",
		},
		{
			url:  "https://github.com/foo/bar/pulls",
			want: "url.generic",
			desc: "pulls URL should NOT match url.github.repo",
		},
		{
			url:  "https://github.com/foo/bar/releases",
			want: "url.generic",
			desc: "releases URL should NOT match url.github.repo",
		},
		{
			url:  "https://gitlab.com/foo/bar",
			want: "url.generic",
			desc: "gitlab URL should NOT match url.github.repo",
		},
	}

	for _, tt := range tests {
		got := r.SelectPipeline(tt.url)
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
