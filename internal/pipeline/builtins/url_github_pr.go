package builtins

import (
	"regexp"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
)

// githubPRPattern matches GitHub pull request URLs.
var githubPRPattern = regexp.MustCompile(`^https://github\.com/[^/]+/[^/]+/pull/\d+`)

func init() {
	MustRegister("url.github.pr", Def{
		Description: "GitHub pull request pipeline",
		Steps: []string{
			"url_fetcher", "html_cleaner", "typedetector", "textcleaner",
			"sectioner", "tagger", "entity_extractor", "entity_resolver", "embedding",
		},
	})
}

// GitHubPRDetector returns a URLPatternDetector for GitHub pull request URLs.
// Register it before url.generic detectors to ensure PR URLs are routed correctly.
func GitHubPRDetector() pipeline.Detector {
	return pipeline.NewURLPatternDetector("url.github.pr", githubPRPattern)
}
