package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Every extension FormatDetector accepts, with the content type, type and
// subtype it assigns. A subtype of "keep" means the draft's own survives.
func TestFormatDetectorEveryExtension(t *testing.T) {
	cases := []struct{ ext, contentType, typ, subtype string }{
		{".png", "image/png", "image", "keep"},
		{".jpg", "image/jpeg", "image", "keep"},
		{".jpeg", "image/jpeg", "image", "keep"},
		{".webp", "image/webp", "image", "keep"},
		{".tiff", "image/tiff", "image", "keep"},
		{".tif", "image/tiff", "image", "keep"},
		{".bmp", "image/bmp", "image", "keep"},
		{".gif", "image/gif", "image", "keep"},
		{".mp3", "audio/mpeg", "audio", "keep"},
		{".wav", "audio/wav", "audio", "keep"},
		{".ogg", "audio/ogg", "audio", "keep"},
		{".flac", "audio/flac", "audio", "keep"},
		{".m4a", "audio/mp4", "audio", "keep"},
		{".webm", "audio/webm", "audio", "keep"},
		{".mp4", "video/mp4", "video", "keep"},
		{".mov", "video/quicktime", "video", "keep"},
		{".avi", "video/x-msvideo", "video", "keep"},
		{".mkv", "video/x-matroska", "video", "keep"},
		{".pdf", "application/pdf", "document", "pdf"},
		{".md", "text/markdown", "document", "markdown"},
		{".markdown", "text/markdown", "document", "markdown"},
		{".go", "text/plain", "document", "code"},
		{".py", "text/plain", "document", "code"},
		{".js", "text/plain", "document", "code"},
		{".ts", "text/plain", "document", "code"},
		{".rs", "text/plain", "document", "code"},
		{".java", "text/plain", "document", "code"},
		{".rb", "text/plain", "document", "code"},
		{".cpp", "text/plain", "document", "code"},
		{".c", "text/plain", "document", "code"},
		{".cs", "text/plain", "document", "code"},
		{".docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "document", "office"},
		{".epub", "application/epub+zip", "document", "epub"},
		{".html", "text/html", "document", "html"},
		{".htm", "text/html", "document", "html"},
		{".PDF", "application/pdf", "document", "pdf"},
	}
	step := NewFormatDetector()
	for _, c := range cases {
		draft := &storage.KnowledgeObject{Source: "/tmp/test" + c.ext, Subtype: "keep"}
		got, err := step.Run(context.Background(), draft)
		if err != nil {
			t.Errorf("%s: %v", c.ext, err)
			continue
		}
		if got.ContentType != c.contentType || got.Type != c.typ || got.Subtype != c.subtype {
			t.Errorf("%s: got (%q, %q, %q), want (%q, %q, %q)", c.ext,
				got.ContentType, got.Type, got.Subtype, c.contentType, c.typ, c.subtype)
		}
	}

	for _, ext := range []string{".exe", ".txt", ""} {
		if _, err := step.Run(context.Background(), &storage.KnowledgeObject{Source: "/tmp/test" + ext}); err == nil {
			t.Errorf("%q: accepted, want unsupported format", ext)
		}
	}
}
