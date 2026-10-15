package steps

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// MIME types several steps assign or match.
const (
	mediaTextPlain = "text/plain"
	mediaTextHTML  = "text/html"
)

// subtypePDF is the document subtype FormatDetector gives .pdf files and
// PDFExtractor runs on.
const subtypePDF = "pdf"

// fileFormat is what a file extension says about its content. An empty
// subtype leaves the draft's own.
type fileFormat struct {
	contentType, typ, subtype string
}

// formatsByExt maps every lower-case file extension FormatDetector
// accepts to its format.
var formatsByExt = func() map[string]fileFormat {
	m := map[string]fileFormat{
		// Images
		".png":  {"image/png", "image", ""},
		".jpg":  {"image/jpeg", "image", ""},
		".jpeg": {"image/jpeg", "image", ""},
		".webp": {"image/webp", "image", ""},
		".tiff": {"image/tiff", "image", ""},
		".tif":  {"image/tiff", "image", ""},
		".bmp":  {"image/bmp", "image", ""},
		".gif":  {"image/gif", "image", ""},

		// Audio
		".mp3":  {"audio/mpeg", "audio", ""},
		".wav":  {"audio/wav", "audio", ""},
		".ogg":  {"audio/ogg", "audio", ""},
		".flac": {"audio/flac", "audio", ""},
		".m4a":  {"audio/mp4", "audio", ""},
		".webm": {"audio/webm", "audio", ""},

		// Video
		".mp4": {"video/mp4", "video", ""},
		".mov": {"video/quicktime", "video", ""},
		".avi": {"video/x-msvideo", "video", ""},
		".mkv": {"video/x-matroska", "video", ""},

		// Documents
		".pdf":      {"application/pdf", "document", subtypePDF},
		".md":       {"text/markdown", "document", "markdown"},
		".markdown": {"text/markdown", "document", "markdown"},
		".docx":     {"application/vnd.openxmlformats-officedocument.wordprocessingml.document", "document", "office"},
		".epub":     {"application/epub+zip", "document", "epub"},
		".html":     {mediaTextHTML, "document", "html"},
		".htm":      {mediaTextHTML, "document", "html"},
	}
	for _, ext := range []string{".go", ".py", ".js", ".ts", ".rs", ".java", ".rb", ".cpp", ".c", ".cs"} {
		m[ext] = fileFormat{mediaTextPlain, "document", "code"}
	}
	return m
}()

type FormatDetector struct {
	pipeline.BaseContract
}

func NewFormatDetector() *FormatDetector {
	return &FormatDetector{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"RawContent"},
			Produces: []string{"ContentType", "Type", "Subtype", "Metadata"},
		}),
	}
}

func (s *FormatDetector) Name() string { return "format_detector" }

func (s *FormatDetector) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	// Try MIME detection from content bytes first.
	if draft.RawContent != "" {
		mime := http.DetectContentType([]byte(draft.RawContent))
		draft.ContentType = mime
	}

	// Refine from file extension if available.
	if draft.Source != "" {
		ext := strings.ToLower(filepath.Ext(draft.Source))
		draft.Metadata["file_extension"] = ext

		f, ok := formatsByExt[ext]
		if !ok {
			return nil, fmt.Errorf("format_detector: unsupported format %q", ext)
		}
		draft.ContentType = f.contentType
		draft.Type = f.typ
		if f.subtype != "" {
			draft.Subtype = f.subtype
		}
	}

	return draft, nil
}
