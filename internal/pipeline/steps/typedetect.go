package steps

import (
	"context"
	"net/http"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blobref"
	"github.com/ideacrafterslabs/ctxt/pkg/taxonomy"
)

type TypeDetector struct {
	pipeline.BaseContract
}

func NewTypeDetector() *TypeDetector {
	return &TypeDetector{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Binary:   true,
			Requires: []string{"RawContent"},
			Produces: []string{"Type", "Subtype"},
		}),
	}
}

func (d *TypeDetector) Name() string { return "typedetect" }

func (d *TypeDetector) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	// Binary is not short or long text: its subtype is what an earlier
	// step (the content type router) made of it, and when none did, only
	// its content type is sniffed.
	if blobref.NeedsBlob(draft.RawContent) {
		if draft.ContentType == "" {
			sniffed := http.DetectContentType([]byte(draft.RawContent))
			draft.ContentType = strings.TrimSpace(strings.SplitN(sniffed, ";", 2)[0])
		}
		return draft, nil
	}

	content := strings.TrimSpace(draft.RawContent)

	// Only a blank or text type is derived from the content: a type the
	// submitter or an earlier step set (url, image, repo) is kept.
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
