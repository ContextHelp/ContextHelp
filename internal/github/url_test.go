package github

import "testing"

func TestParseRepoURL(t *testing.T) {
	tests := []struct {
		in          string
		owner, name string
		ok          bool
	}{
		{"https://github.com/foo/bar", "foo", "bar", true},
		{"https://github.com/foo/bar/", "foo", "bar", true},
		{"  https://GitHub.com/Foo/Bar  ", "Foo", "Bar", true},
		{"https://github.com/foo/bar?tab=readme", "foo", "bar", true},
		{"https://github.com/foo/bar/issues/1", "", "", false},
		{"https://github.com/foo/bar.git", "", "", false},
		{"https://github.com/foo", "", "", false},
		{"https://gitlab.com/foo/bar", "", "", false},
		{"cli", "", "", false},
	}
	for _, tt := range tests {
		o, n, ok := ParseRepoURL(tt.in)
		if o != tt.owner || n != tt.name || ok != tt.ok {
			t.Errorf("ParseRepoURL(%q) = %q, %q, %v", tt.in, o, n, ok)
		}
	}
}
