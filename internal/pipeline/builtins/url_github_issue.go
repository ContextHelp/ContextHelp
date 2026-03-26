package builtins

import (
	"regexp"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
)

var githubIssuePattern = regexp.MustCompile(`^https://github\.com/[^/]+/[^/]+/issues/\d+`)

func init() {
	MustRegister("url.github.issue", Def{
		Description: "GitHub issue pipeline",
		Steps:       []string{"url_fetcher", "html_cleaner", "typedetector", "textcleaner", "sectioner", "tagger", "entity_extractor", "entity_resolver", "embedding"},
	})

	registerURLDetector(pipeline.NewURLPatternDetector("url.github.issue", githubIssuePattern))
}

// urlDetectors holds URL pattern detectors registered by pipeline init() calls.
// Detectors are tried in registration order; the first match wins.
var urlDetectors []*pipeline.URLPatternDetector

// registerURLDetector appends a URLPatternDetector to the list of URL detectors
// consulted by selectPipeline before falling back to url.generic.
func registerURLDetector(d *pipeline.URLPatternDetector) {
	urlDetectors = append(urlDetectors, d)
}

// detectURL tries registered URL detectors in order. Returns "" if none match.
func detectURL(source string) string {
	if !strings.HasPrefix(source, "http://") && !strings.HasPrefix(source, "https://") {
		return ""
	}
	in := pipeline.DetectInput{Source: source}
	for _, d := range urlDetectors {
		name, err := d.Detect(in)
		if err == nil {
			return name
		}
	}
	return ""
}
