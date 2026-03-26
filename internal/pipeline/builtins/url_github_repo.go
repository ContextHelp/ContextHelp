package builtins

import (
	"regexp"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
)

func init() {
	MustRegister("url.github.repo", Def{
		Description: "GitHub repository pipeline",
		Steps:       []string{"url_fetcher", "html_cleaner", "typedetector", "textcleaner", "sectioner", "tagger", "entity_extractor", "entity_resolver", "embedding"},
	})
	MustRegisterDetector(pipeline.NewURLPatternDetector(
		"url.github.repo",
		regexp.MustCompile(`^https://github\.com/[^/]+/[^/]+/?$`),
	))
}
