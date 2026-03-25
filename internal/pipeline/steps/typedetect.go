package steps

import (
	"context"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/taxonomy"
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

	if draft.Type == "" || draft.Type == taxonomy.TypeText {
		if strings.HasPrefix(content, "http://") || strings.HasPrefix(content, "https://") {
			draft.Type = taxonomy.TypeURL
		} else {
			draft.Type = taxonomy.TypeText
		}
	}

	if len(content) < 500 {
		draft.Subtype = taxonomy.SubtypeTextShort
	} else {
		draft.Subtype = taxonomy.SubtypeTextLong
	}

	return draft, nil
}
