package builtins

import "regexp"

// githubRepoPattern matches GitHub repository root web URLs (no sub-paths, no .git suffix).
// .git-suffixed URLs are git clone URLs handled by url.repo, not web browser URLs.
var githubRepoPattern = regexp.MustCompile(`^https://github\.com/[^/]+/[^/]+/?$`)

func init() {
	MustRegister("url.github.repo", Def{
		Description: "GitHub repository pipeline",
		URLPattern:  githubRepoPattern,
		Steps: []string{
			"url_fetcher",
			"html_cleaner",
			"typedetector",
			"textcleaner",
			"sectioner",
			"tagger",
			"entity_extractor",
			"entity_resolver",
			"embedding",
		},
	})
}
