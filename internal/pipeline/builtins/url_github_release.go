package builtins

import (
	"regexp"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
)

// githubReleasePattern matches GitHub release URLs:
//   - https://github.com/<owner>/<repo>/releases/tag/<tag>
//   - https://github.com/<owner>/<repo>/releases
var githubReleasePattern = regexp.MustCompile(`^https://github\.com/[^/]+/[^/]+/releases`)

func init() {
	MustRegister("url.github.release", Def{
		Description: "GitHub release pipeline",
		Steps: []string{
			"url_fetcher", "html_cleaner", "typedetector", "textcleaner",
			"sectioner", "tagger", "entity_extractor", "entity_resolver", "embedding",
		},
	})
}

// githubReleaseDetector returns a URLPatternDetector for GitHub release URLs.
func githubReleaseDetector() pipeline.Detector {
	return pipeline.NewURLPatternDetector("url.github.release", githubReleasePattern)
}
