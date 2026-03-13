package steps

import (
	"context"
	"regexp"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

var fencedCodeBlockRe = regexp.MustCompile("(?s)```([a-zA-Z0-9_-]*)\\n(.*?)```")

// CodeBlockExtractor extracts fenced code blocks from Markdown content.
type CodeBlockExtractor struct {
	pipeline.BaseContract
}

// NewCodeBlockExtractor creates a CodeBlockExtractor.
func NewCodeBlockExtractor() *CodeBlockExtractor {
	return &CodeBlockExtractor{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"RawContent"},
			Produces: []string{"Sections"},
		}),
	}
}

func (s *CodeBlockExtractor) Name() string { return "code_block_extractor" }

func (s *CodeBlockExtractor) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	matches := fencedCodeBlockRe.FindAllStringSubmatch(draft.RawContent, -1)
	for _, m := range matches {
		lang := strings.TrimSpace(m[1])
		code := strings.TrimSpace(m[2])

		title := "Code Block"
		if lang != "" {
			title = "Code Block (" + lang + ")"
		}

		draft.Sections = append(draft.Sections, storage.Section{
			Title:   title,
			Content: code,
			Order:   len(draft.Sections),
			Metadata: map[string]any{
				"type":     "code_block",
				"language": lang,
			},
		})
	}

	draft.Metadata["code_block_count"] = len(matches)

	return draft, nil
}
