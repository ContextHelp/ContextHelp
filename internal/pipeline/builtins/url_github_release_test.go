package builtins

import (
	"testing"
)

func TestURLGitHubReleasePipelineRegistered(t *testing.T) {
	d := Defs()
	if _, ok := d["url.github.release"]; !ok {
		t.Fatal("url.github.release not registered")
	}
}

func TestURLGitHubReleaseDetector(t *testing.T) {
	r := Registry()

	tests := []struct {
		url  string
		want string
	}{
		{"https://github.com/foo/bar/releases/tag/v1.2.3", "url.github.release"},
		{"https://github.com/foo/bar/releases", "url.github.release"},
		{"https://github.com/foo/bar/pull/42", "url.github.pr"},
		{"https://github.com/foo/bar", "url.github.repo"},
	}

	for _, tt := range tests {
		got := r.SelectPipeline(tt.url, "")
		if got != tt.want {
			t.Errorf("SelectPipeline(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}
