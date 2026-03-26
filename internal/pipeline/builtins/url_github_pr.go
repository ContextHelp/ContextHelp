package builtins

import "regexp"

// githubPRPattern matches GitHub pull request URLs.
var githubPRPattern = regexp.MustCompile(`^https://github\.com/[^/]+/[^/]+/pull/\d+`)

func init() {
	MustRegister("url.github.pr", Def{
		Description: "GitHub pull request pipeline",
		URLPattern:  githubPRPattern,
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
