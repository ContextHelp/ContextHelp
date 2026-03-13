package steps

import (
	"context"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type TypeDetector struct {
	pipeline.BaseContract
}

func NewTypeDetector() *TypeDetector {
	return &TypeDetector{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"RawContent"},
			Produces: []string{"Type", "Subtype"},
		}),
	}
}

func (d *TypeDetector) Name() string { return "typedetect" }

func (d *TypeDetector) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	content := strings.TrimSpace(draft.RawContent)

	if draft.Type == "" || draft.Type == "text" {
		if strings.HasPrefix(content, "http://") || strings.HasPrefix(content, "https://") {
			draft.Type = "url"
		} else {
			draft.Type = "text"
		}
	}

	if len(content) < 500 {
		draft.Subtype = "short"
	} else {
		draft.Subtype = "long"
	}

	return draft, nil
}
