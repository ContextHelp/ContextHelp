package service

import (
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
)

// detectPipeline picks the pipeline for content captured from source when
// the caller named none.
//
// The two signals never cross: URL patterns and extension rules read the
// source, so a note that merely ends in "main.go" stays text; length and
// structure rules read the content, so a .txt or .log file, or text under a
// capture label ("argument", "stdin", "file", ...), routes by what it holds.
func (s *Service) detectPipeline(source, contentType, content string) string {
	return s.Pipes.Detect(pipeline.DetectInput{
		Source:      source,
		ContentType: contentType,
		Content:     content,
	})
}
