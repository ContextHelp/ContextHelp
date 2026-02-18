package steps

import (
	"context"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type TypeDetector struct{}

func NewTypeDetector() *TypeDetector { return &TypeDetector{} }

func (d *TypeDetector) Name() string { return "typedetect" }

func (d *TypeDetector) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	content := strings.TrimSpace(draft.RawContent)

	if strings.HasPrefix(content, "http://") || strings.HasPrefix(content, "https://") {
		draft.Type = "url"
	} else {
		draft.Type = "text"
	}

	if len(content) < 500 {
		draft.Subtype = "short"
	} else {
		draft.Subtype = "long"
	}

	return draft, nil
}
