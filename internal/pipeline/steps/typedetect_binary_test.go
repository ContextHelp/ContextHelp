package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/taxonomy"
)

const pngSig = "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\xff\xfe"

func TestTypeDetectorKeepsRouterSubtypeOnBinary(t *testing.T) {
	d := &storage.KnowledgeObject{RawContent: pngSig, ContentType: "image/png", Subtype: "image"}
	out, err := NewTypeDetector().Run(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	if out.Subtype != "image" || out.ContentType != "image/png" {
		t.Fatalf("subtype %q content type %q, want image, image/png", out.Subtype, out.ContentType)
	}
}

func TestTypeDetectorSniffsBinaryWithoutSubtype(t *testing.T) {
	out, err := NewTypeDetector().Run(context.Background(), &storage.KnowledgeObject{RawContent: pngSig})
	if err != nil {
		t.Fatal(err)
	}
	if out.ContentType != "image/png" {
		t.Errorf("ContentType = %q, want image/png", out.ContentType)
	}
	if out.Subtype != "" {
		t.Errorf("Subtype = %q, binary gets no text subtype", out.Subtype)
	}
}

func TestTypeDetectorStillLabelsText(t *testing.T) {
	out, err := NewTypeDetector().Run(context.Background(), &storage.KnowledgeObject{RawContent: "short note"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Subtype != taxonomy.SubtypeTextShort || out.ContentType != "" {
		t.Fatalf("subtype %q content type %q", out.Subtype, out.ContentType)
	}
}

func TestContentTypeRouterRunsOnBinary(t *testing.T) {
	r := NewContentTypeRouter()
	if !r.Contract().Binary {
		t.Fatal("router reads metadata, not content: must be Binary")
	}
}
