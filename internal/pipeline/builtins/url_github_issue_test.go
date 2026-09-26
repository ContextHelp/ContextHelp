package builtins

import (
	"testing"
)

func TestURLGitHubIssueDetector(t *testing.T) {
	r := Registry()

	tests := []struct {
		url  string
		want string
	}{
		{"https://github.com/foo/bar/issues/42", "url.github.issue"},
		{"https://github.com/foo/bar/issues/1", "url.github.issue"},
		{"https://github.com/org/repo/issues/999", "url.github.issue"},
		// Not a GitHub issue URL.
		{"https://github.com/foo/bar/pull/42", "url.github.pr"},
		{"https://github.com/foo/bar", "url.github.repo"},
		{"https://github.com/foo/bar/issues", "url.generic"},
		{"https://example.com/issues/42", "url.generic"},
	}

	for _, tt := range tests {
		got := r.SelectPipeline(tt.url, "")
		if got != tt.want {
			t.Errorf("SelectPipeline(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

func TestURLGitHubIssuePipelineRegistered(t *testing.T) {
	d := Defs()
	def, ok := d["url.github.issue"]
	if !ok {
		t.Fatal("url.github.issue pipeline not registered")
	}
	if def.Description == "" {
		t.Error("url.github.issue pipeline has empty description")
	}
	if len(def.Steps) == 0 {
		t.Error("url.github.issue pipeline has no steps")
	}
}

func TestURLGitHubIssuePatternDirectly(t *testing.T) {
	matches := []string{
		"https://github.com/foo/bar/issues/42",
		"https://github.com/org-name/repo-name/issues/1234",
	}
	noMatches := []string{
		"https://github.com/foo/bar/pull/42",
		"https://github.com/foo/bar",
		"https://github.com/foo/bar/issues",
		"https://github.com/foo/bar/issues/",
		"http://github.com/foo/bar/issues/42", // http, not https
	}

	for _, u := range matches {
		if !githubIssuePattern.MatchString(u) {
			t.Errorf("expected match: %q", u)
		}
	}
	for _, u := range noMatches {
		if githubIssuePattern.MatchString(u) {
			t.Errorf("unexpected match: %q", u)
		}
	}
}
