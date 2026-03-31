package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestContentTypeRouter_PromotesMetadataContentType(t *testing.T) {
	step := NewContentTypeRouter()
	draft := &storage.KnowledgeObject{
		Metadata: map[string]any{"content_type": "application/pdf; charset=utf-8"},
	}
	out, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatal(err)
	}
	if out.ContentType != "application/pdf" {
		t.Errorf("ContentType: got %q, want %q", out.ContentType, "application/pdf")
	}
	if out.Subtype != "pdf" {
		t.Errorf("Subtype: got %q, want %q", out.Subtype, "pdf")
	}
}

func TestContentTypeRouter_FallsBackToExtension(t *testing.T) {
	step := NewContentTypeRouter()
	draft := &storage.KnowledgeObject{
		Source:   "https://example.com/report.pdf",
		Metadata: map[string]any{},
	}
	out, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatal(err)
	}
	if out.Subtype != "pdf" {
		t.Errorf("Subtype: got %q, want %q", out.Subtype, "pdf")
	}
}

func TestContentTypeRouter_HTMLSubtype(t *testing.T) {
	step := NewContentTypeRouter()
	draft := &storage.KnowledgeObject{
		Metadata: map[string]any{"content_type": "text/html; charset=utf-8"},
	}
	out, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatal(err)
	}
	if out.Subtype != "html" {
		t.Errorf("Subtype: got %q, want %q", out.Subtype, "html")
	}
}

func TestContentTypeRouter_UnknownTypeLogsAndSetsUnsupported(t *testing.T) {
	step := NewContentTypeRouter()
	draft := &storage.KnowledgeObject{
		Source:   "https://example.com/file.bin",
		Metadata: map[string]any{"content_type": "application/x-unknown-binary"},
	}
	out, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatal(err)
	}
	if out.Subtype != "unsupported" {
		t.Errorf("Subtype: got %q, want %q", out.Subtype, "unsupported")
	}
}

func TestContentTypeRouter_NoMetadata(t *testing.T) {
	step := NewContentTypeRouter()
	draft := &storage.KnowledgeObject{}
	out, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatal(err)
	}
	if out.Subtype != "" {
		t.Errorf("Subtype: got %q, want empty", out.Subtype)
	}
}

func TestContentTypeRouter_PreservesExistingContentType(t *testing.T) {
	step := NewContentTypeRouter()
	draft := &storage.KnowledgeObject{
		ContentType: "text/markdown",
		Metadata:    map[string]any{"content_type": "text/html"},
	}
	out, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatal(err)
	}
	// Pre-set ContentType must not be overwritten by Metadata.
	if out.ContentType != "text/markdown" {
		t.Errorf("ContentType: got %q, want %q", out.ContentType, "text/markdown")
	}
	if out.Subtype != "markdown" {
		t.Errorf("Subtype: got %q, want %q", out.Subtype, "markdown")
	}
}

func TestContentTypeRouter_AllRegisteredHandlers(t *testing.T) {
	tests := []struct {
		mime    string
		subtype string
	}{
		{"application/pdf", "pdf"},
		{"application/vnd.openxmlformats-officedocument.wordprocessingml.document", "office"},
		{"application/epub+zip", "epub"},
		{"text/markdown", "markdown"},
		{"text/csv", "csv"},
		{"application/json", "json"},
		{"application/xml", "xml"},
		{"text/html", "html"},
		{"image/png", "image"},
		{"image/jpeg", "image"},
		{"audio/mpeg", "audio"},
		{"video/mp4", "video"},
		{"text/plain", "text"},
	}
	step := NewContentTypeRouter()
	for _, tc := range tests {
		t.Run(tc.mime, func(t *testing.T) {
			draft := &storage.KnowledgeObject{
				Metadata: map[string]any{"content_type": tc.mime},
			}
			out, err := step.Run(context.Background(), draft)
			if err != nil {
				t.Fatal(err)
			}
			if out.Subtype != tc.subtype {
				t.Errorf("mime %q: Subtype got %q, want %q", tc.mime, out.Subtype, tc.subtype)
			}
		})
	}
}
