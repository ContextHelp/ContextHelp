package steps

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// HeadingSplitter splits content at heading boundaries up to a configurable depth.
type HeadingSplitter struct {
	pipeline.BaseContract
	maxDepth int
}

// HeadingSplitterOption configures a HeadingSplitter.
type HeadingSplitterOption func(*HeadingSplitter)

// WithMaxHeadingDepth sets the maximum heading depth to split on (1-6).
func WithMaxHeadingDepth(d int) HeadingSplitterOption {
	return func(s *HeadingSplitter) {
		if d < 1 {
			d = 1
		}
		if d > 6 {
			d = 6
		}
		s.maxDepth = d
	}
}

// NewHeadingSplitter creates a HeadingSplitter with optional configuration.
func NewHeadingSplitter(opts ...HeadingSplitterOption) *HeadingSplitter {
	s := &HeadingSplitter{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"RawContent"},
			Produces: []string{"Sections"},
		}),
		maxDepth: 2,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *HeadingSplitter) Name() string { return "heading_splitter" }

func (s *HeadingSplitter) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	pattern := fmt.Sprintf(`(?m)^(#{1,%d})\s+(.+)$`, s.maxDepth)
	re := regexp.MustCompile(pattern)

	content := draft.RawContent
	matches := re.FindAllStringSubmatchIndex(content, -1)
	if len(matches) == 0 {
		return draft, nil
	}

	for i, match := range matches {
		level := len(content[match[2]:match[3]])
		title := strings.TrimSpace(content[match[4]:match[5]])

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

	return draft, nil
}
