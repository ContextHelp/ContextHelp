package steps

import (
	"context"
	"regexp"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

var markdownHeadingRe = regexp.MustCompile(`(?m)^(#{1,6})\s+(.+)$`)

// MarkdownParser parses Markdown headings and creates sections.
type MarkdownParser struct {
	pipeline.BaseContract
}

// NewMarkdownParser creates a MarkdownParser.
func NewMarkdownParser() *MarkdownParser {
	return &MarkdownParser{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"RawContent"},
			Produces: []string{"Sections"},
		}),
	}
}

func (s *MarkdownParser) Name() string { return "markdown_parser" }

func (s *MarkdownParser) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	content := draft.RawContent
	matches := markdownHeadingRe.FindAllStringSubmatchIndex(content, -1)
	if len(matches) == 0 {
		return draft, nil
	}

	for i, match := range matches {
		level := len(content[match[2]:match[3]])
		title := strings.TrimSpace(content[match[4]:match[5]])

		// Extract content between this heading and the next.
		start := match[1]
		end := len(content)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		body := strings.TrimSpace(content[start:end])

		draft.Sections = append(draft.Sections, storage.Section{
			Title:   title,
			Content: body,
			Order:   len(draft.Sections),
			Metadata: map[string]any{
				"type":          "heading",
				"heading_level": level,
			},
		})
	}

	draft.Metadata["markdown_heading_count"] = len(matches)

	return draft, nil
}
