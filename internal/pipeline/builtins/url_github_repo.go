package builtins

import "regexp"

// githubRepoPattern matches GitHub repository root web URLs (no sub-paths, no .git suffix).
// .git-suffixed URLs are git clone URLs handled by url.repo, not web browser URLs.
var githubRepoPattern = regexp.MustCompile(`(?i)^https?://(www\.)?github\.com/[^/]+/[^/]+/?$`)

func init() {
	MustRegister("url.github.repo", Def{
		Description: "GitHub repository pipeline",
		URLPattern:  githubRepoPattern,
		// The REST API replaces the HTML scrape: github_repo_fetcher
		// reads the repository record and README, github_repo_mapper
		// types the object (repo/github) and sets forge identity, which
		// the dedup hash is taken over. No typedetector: it would reset
		// the subtype.
		Steps: []string{
			"github_repo_fetcher",
			"github_repo_mapper",
			"sectioner",
			"tagger",
			"entity_extractor",
			"entity_resolver",
			"dependency_enricher",
			"embedding",
		},
	})
}
