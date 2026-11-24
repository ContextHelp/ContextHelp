package contracttest

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/providers"
)

// Document is the document role's suite: ParseMarkdown and ExtractOffice
// return text with page numbering from 1. docxPath is a small DOCX.
func Document(t *testing.T, cases []Case[providers.DocumentProvider], docxPath string) {
	Run(t, cases, func(t *testing.T, c Case[providers.DocumentProvider], p providers.DocumentProvider) {
		requireName(t, p.Name())
		ctx := context.Background()

		md, err := p.ParseMarkdown(ctx, "# Title\n\nBody text.\n")
		if err != nil {
			t.Fatalf("ParseMarkdown: %v", err)
		}
		if md.FullText == "" {
			t.Error("ParseMarkdown: empty text")
		}

		doc, err := p.ExtractOffice(ctx, docxPath)
		if err != nil {
			t.Fatalf("ExtractOffice: %v", err)
		}
		if doc.FullText == "" {
			t.Error("ExtractOffice: empty text")
		}
		for i, pg := range doc.Pages {
			if pg.Number != i+1 {
				t.Errorf("page %d numbered %d", i, pg.Number)
			}
		}
	})
}
