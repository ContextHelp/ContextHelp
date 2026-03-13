package watcher_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/watcher"
)

func TestMatchGlob(t *testing.T) {
	cases := []struct {
		pattern string
		path    string
		want    bool
	}{
		{"**/*.md", "a/b/c.md", true},
		{"**/*.md", "note.md", true},
		{"**/*.md", "a/b/c.txt", false},
		{"**/.git/**", ".git/objects/abc", true},
		{"**/.git/**", "repo/.git/HEAD", true},
		{"**/.git/**", "readme.md", false},
		{"*.go", "main.go", true},
		{"*.go", "sub/main.go", false},
		{"**", "anything/at/all", true},
	}
	for _, tc := range cases {
		got := watcher.MatchGlob(tc.pattern, tc.path)
		if got != tc.want {
			t.Errorf("MatchGlob(%q, %q) = %v, want %v", tc.pattern, tc.path, got, tc.want)
		}
	}
}

func TestMatchAny_EmptyPatterns(t *testing.T) {
	if !watcher.MatchAny(nil, "anything.md") {
		t.Error("expected MatchAny(nil, ...) == true")
	}
	if !watcher.MatchAny([]string{}, "anything.md") {
		t.Error("expected MatchAny([], ...) == true")
	}
}

func TestMatchAny_MultiplePatterns(t *testing.T) {
	patterns := []string{"**/*.md", "**/*.txt"}
	if !watcher.MatchAny(patterns, "notes/idea.md") {
		t.Error("expected match for .md")
	}
	if !watcher.MatchAny(patterns, "notes/log.txt") {
		t.Error("expected match for .txt")
	}
	if watcher.MatchAny(patterns, "image.png") {
		t.Error("expected no match for .png")
	}
}
