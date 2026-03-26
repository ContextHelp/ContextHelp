package builtins

import "testing"

func TestURLRepoRegistered(t *testing.T) {
	d := Defs()
	def, ok := d["url.repo"]
	if !ok {
		t.Fatal("url.repo pipeline not registered")
	}
	if def.Description == "" {
		t.Error("url.repo: empty description")
	}
	if def.URLPattern == nil {
		t.Error("url.repo: URLPattern must be set")
	}
}

func TestURLRepoSelectedForRepoURLs(t *testing.T) {
	r := Registry()

	// GitHub URLs are intentionally excluded: url.github.repo is more specific
	// and will match them when that pipeline is registered.
	match := []string{
		"https://github.com/foo/bar.git",
		"https://gitlab.com/foo/bar",
		"https://gitlab.com/foo/bar/",
		"https://bitbucket.org/foo/bar",
		"https://bitbucket.org/foo/bar/",
	}
	for _, url := range match {
		got := r.SelectPipeline(url)
		if got != "url.repo" {
			t.Errorf("SelectPipeline(%q) = %q, want url.repo", url, got)
		}
	}
}

func TestURLRepoNotSelectedForNonRepoURLs(t *testing.T) {
	r := Registry()

	noMatch := []string{
		"https://github.com/foo/bar/issues/1",
		"https://github.com/foo/bar/pull/42",
		"https://github.com/foo/bar/commit/abc123",
		"https://example.com",
		"https://example.com/foo/bar",
	}
	for _, url := range noMatch {
		got := r.SelectPipeline(url)
		if got == "url.repo" {
			t.Errorf("SelectPipeline(%q) = url.repo, want something else (url.generic or other)", url)
		}
	}
}
