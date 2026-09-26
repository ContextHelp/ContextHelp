package service

import (
	"path/filepath"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
)

// detectPipeline picks the pipeline for content captured from source when
// the caller named none.
//
// A locator source (an http/https URL, or a path carrying a file extension)
// routes by the locator: URL patterns and extension rules decide. Any other
// source is a capture label ("argument", "stdin", "clipboard", "file",
// "watcher/screen", ...) that says nothing about the content, so the
// registry's content rules decide instead (text.short vs text.long by length
// and markdown structure, payload-shaped importers).
func (s *Service) detectPipeline(source, contentType, content string) string {
	in := pipeline.DetectInput{
		Source:      source,
		ContentType: contentType,
		Sniff:       contentSniff(content),
	}
	if !isLocator(source) {
		in.Source = content
	}
	return s.Pipes.Detect(in)
}

// isLocator reports whether source names where the content came from in a
// form the registry routes on: an http/https URL or a file extension.
func isLocator(source string) bool {
	lower := strings.ToLower(strings.TrimSpace(source))
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return true
	}
	return filepath.Ext(lower) != ""
}
