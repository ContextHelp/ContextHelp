package steps

import (
	"context"
	"regexp"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type TextCleaner struct {
	pipeline.BaseContract
}

func NewTextCleaner() *TextCleaner {
	return &TextCleaner{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"RawContent"},
			Produces: []string{"RawContent"},
		}),
	}
}

func (s *TextCleaner) Name() string { return "text_cleaner" }

var multiSpace = regexp.MustCompile(`[ \t]+`)
var multiNewline = regexp.MustCompile(`\n{3,}`)

func (s *TextCleaner) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	text := draft.RawContent
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = multiSpace.ReplaceAllString(text, " ")
	text = multiNewline.ReplaceAllString(text, "\n\n")
	text = strings.TrimSpace(text)
	draft.RawContent = text
	return draft, nil
}
