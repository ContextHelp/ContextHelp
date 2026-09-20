package steps

import (
	"context"
	"regexp"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// HTMLCleaner strips HTML tags from RawContent.
type HTMLCleaner struct {
	pipeline.BaseContract
}

func NewHTMLCleaner() *HTMLCleaner {
	return &HTMLCleaner{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"RawContent"},
			Produces: []string{"RawContent"},
		}),
	}
}

func (s *HTMLCleaner) Name() string { return "html_cleaner" }

var htmlTagRe = regexp.MustCompile(`(?s)<[^>]*>`)
var scriptTagRe = regexp.MustCompile(`(?s)<script.*?</script>`)
var styleTagRe = regexp.MustCompile(`(?s)<style.*?</style>`)

func (s *HTMLCleaner) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if !strings.Contains(strings.ToLower(draft.ContentType), "html") && !strings.HasPrefix(strings.TrimSpace(draft.RawContent), "<!") {
		return draft, nil
	}

	text := draft.RawContent
	text = scriptTagRe.ReplaceAllString(text, " ")
	text = styleTagRe.ReplaceAllString(text, " ")
	text = htmlTagRe.ReplaceAllString(text, " ")

	// Unescape common HTML entities
	text = strings.ReplaceAll(text, "&nbsp;", " ")
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")
	text = strings.ReplaceAll(text, "&amp;", "&")
	text = strings.ReplaceAll(text, "&quot;", "\"")

	draft.RawContent = text
	return draft, nil
}
