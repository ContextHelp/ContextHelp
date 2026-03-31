package builtins

import "regexp"

// githubProfilePattern matches GitHub user and org profile URLs.
// Exactly one path segment after github.com — no subpaths, no repo name.
// Examples:
//
//	https://github.com/torvalds
//	https://github.com/nomic-ai
//	https://github.com/torvalds?tab=repositories  (tab param ignored)
var githubProfilePattern = regexp.MustCompile(`^https://github\.com/[^/]+/?(\?.*)?$`)

func init() {
	MustRegister("url.github.profile", Def{
		Description: "GitHub user or org profile pipeline — fetches profile metadata; pipeline steps " +
			"can disambiguate user vs org via the GitHub API (api.github.com/users/:name " +
			"returns type:User or type:Organization).",
		URLPattern: githubProfilePattern,
		Steps: []string{
			"url_fetcher",
			"content_type_router",
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
