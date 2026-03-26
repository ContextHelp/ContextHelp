package builtins

import "regexp"

// repoURLPattern matches GitHub, GitLab, and Bitbucket repository root URLs:
//   https://github.com/<owner>/<repo>
//   https://gitlab.com/<owner>/<repo>
//   https://bitbucket.org/<owner>/<repo>
//
// Sub-paths (issues, PRs, commits, …) are intentionally excluded so that
// non-root forge URLs fall through to url.generic.
var repoURLPattern = regexp.MustCompile(
	`(?i)^https?://(github\.com|gitlab\.com|bitbucket\.org)/[^/]+/[^/]+(\.git)?/?$`,
)

func init() {
	MustRegister("url.repo", Def{
		Description: "Git repository pipeline (GitHub, GitLab, Bitbucket)",
		URLPattern:  repoURLPattern,
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
