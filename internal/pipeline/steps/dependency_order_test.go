package steps

import (
	"slices"
	"testing"
)

// dep_files is a map: the dependency list, and the node ordinals built
// from it, must not depend on its iteration order.
func TestParseDepFilesIsOrderedByFileName(t *testing.T) {
	files := map[string]any{
		"requirements.txt": "flask==3.0.0\nrequests>=2.0.0\n",
		"package.json":     `{"dependencies":{"left-pad":"1"}}`,
		"go.mod":           "module x\n\nrequire example.com/a v1.0.0\n",
		"Gemfile":          "gem 'rake'\n",
	}
	var first []string
	for i := 0; i < 40; i++ {
		deps, err := parseDepFiles(files)
		if err != nil {
			t.Fatal(err)
		}
		var urls []string
		for _, d := range deps {
			urls = append(urls, d.URL)
		}
		if i == 0 {
			first = urls
			if len(urls) != 5 {
				t.Fatalf("deps = %v", urls)
			}
			continue
		}
		if !slices.Equal(urls, first) {
			t.Fatalf("run %d: %v, first %v", i, urls, first)
		}
	}
}
