package builtins

import "regexp"

// githubIssuePattern matches GitHub issue URLs.
var githubIssuePattern = regexp.MustCompile(`^https://github\.com/[^/]+/[^/]+/issues/\d+`)

func init() {
	MustRegister("url.github.issue", Def{
		Description: "GitHub issue pipeline",
		URLPattern:  githubIssuePattern,
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
