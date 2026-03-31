package builtins

import "strings"

func init() {
	MustRegister("url.github.starred", Def{
		Description: "GitHub starred repository pipeline — enriches with star context and list metadata",
		// ContentTest matches payloads that contain a GitHub URL and were sourced
		// from a starred list (e.g. import.github starred export).
		// The payload format is: "Repository: owner/repo\nURL: https://github.com/...\nSource: starred\n..."
		ContentTest: func(content string) bool {
			return strings.Contains(content, "github.com") &&
				strings.Contains(content, "Source: starred")
		},
		// Run before generic text-length selectors (text.short / text.long).
		Priority: -1,
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
			"dependency_enricher",
			"embedding",
		},
	})
}
