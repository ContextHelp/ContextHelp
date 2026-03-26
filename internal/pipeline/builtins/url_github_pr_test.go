package builtins

import "testing"

func TestURLGitHubPRPipelineRegistered(t *testing.T) {
	d := Defs()
	def, ok := d["url.github.pr"]
	if !ok {
		t.Fatal("url.github.pr not registered")
	}
	if def.Description == "" {
		t.Error("url.github.pr: empty description")
	}
	if def.URLPattern == nil {
		t.Error("url.github.pr: URLPattern must be set")
	}
}

func TestURLGitHubPRDetector(t *testing.T) {
	r := Registry()

	tests := []struct {
		url  string
		want string
	}{
		{"https://github.com/foo/bar/pull/42", "url.github.pr"},
		{"https://github.com/foo/bar/pull/1", "url.github.pr"},
		{"https://github.com/org/repo/pull/9999", "url.github.pr"},
		// Not PR URLs.
		{"https://github.com/foo/bar/issues/42", "url.generic"},
		{"https://github.com/foo/bar", "url.repo"},
		{"https://github.com/foo/bar/tree/main", "url.generic"},
		{"https://example.com/page", "url.generic"},
	}

	for _, tt := range tests {
		got := r.SelectPipeline(tt.url)
		if got != tt.want {
			t.Errorf("SelectPipeline(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

func TestURLGitHubPRPatternDirectly(t *testing.T) {
	matches := []string{
		"https://github.com/foo/bar/pull/42",
		"https://github.com/org/repo/pull/1",
	}
	noMatches := []string{
		"https://github.com/foo/bar/issues/42",
		"https://github.com/foo/bar",
		"https://example.com/pull/42",
	}
	for _, u := range matches {
		if !githubPRPattern.MatchString(u) {
			t.Errorf("expected match: %q", u)
		}
	}
	for _, u := range noMatches {
		if githubPRPattern.MatchString(u) {
			t.Errorf("unexpected match: %q", u)
		}
	}
}
