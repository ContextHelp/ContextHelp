package builtins

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
)

func TestGitHubPRDetector(t *testing.T) {
	det := GitHubPRDetector()

	tests := []struct {
		url  string
		want string // empty = expect ErrDelegate
	}{
		{"https://github.com/foo/bar/pull/42", "url.github.pr"},
		{"https://github.com/foo/bar/pull/1", "url.github.pr"},
		{"https://github.com/org/repo/pull/9999", "url.github.pr"},
		// Not PR URLs — expect ErrDelegate.
		{"https://github.com/foo/bar/issues/42", ""},
		{"https://github.com/foo/bar", ""},
		{"https://github.com/foo/bar/tree/main", ""},
		{"https://example.com/foo/bar/pull/42", ""},
		{"not-a-url", ""},
	}

	for _, tt := range tests {
		in := pipeline.DetectInput{Source: tt.url}
		name, err := det.Detect(in)
		if tt.want == "" {
			if err == nil {
				t.Errorf("Detect(%q) = %q, want ErrDelegate", tt.url, name)
			}
		} else {
			if err != nil {
				t.Errorf("Detect(%q) error: %v", tt.url, err)
			} else if name != tt.want {
				t.Errorf("Detect(%q) = %q, want %q", tt.url, name, tt.want)
			}
		}
	}
}

func TestRegistrySelectsGitHubPR(t *testing.T) {
	r := Registry()

	tests := []struct {
		url  string
		want string
	}{
		{"https://github.com/foo/bar/pull/42", "url.github.pr"},
		{"https://github.com/foo/bar/issues/42", "url.generic"},
		{"https://github.com/foo/bar", "url.generic"},
		{"https://example.com/page", "url.generic"},
	}

	for _, tt := range tests {
		got := r.SelectPipeline(tt.url)
		if got != tt.want {
			t.Errorf("SelectPipeline(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}
