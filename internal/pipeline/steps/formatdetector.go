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

		switch ext {
		// Images
		case ".png":
			draft.ContentType = "image/png"
			draft.Type = "image"
		case ".jpg", ".jpeg":
			draft.ContentType = "image/jpeg"
			draft.Type = "image"
		case ".webp":
			draft.ContentType = "image/webp"
			draft.Type = "image"
		case ".tiff", ".tif":
			draft.ContentType = "image/tiff"
			draft.Type = "image"
		case ".bmp":
			draft.ContentType = "image/bmp"
			draft.Type = "image"
		case ".gif":
			draft.ContentType = "image/gif"
			draft.Type = "image"

		// Audio
		case ".mp3":
			draft.ContentType = "audio/mpeg"
			draft.Type = "audio"
		case ".wav":
			draft.ContentType = "audio/wav"
			draft.Type = "audio"
		case ".ogg":
			draft.ContentType = "audio/ogg"
			draft.Type = "audio"
		case ".flac":
			draft.ContentType = "audio/flac"
			draft.Type = "audio"
		case ".m4a":
			draft.ContentType = "audio/mp4"
			draft.Type = "audio"
		case ".webm":
			draft.ContentType = "audio/webm"
			draft.Type = "audio"

		// Video
		case ".mp4":
			draft.ContentType = "video/mp4"
			draft.Type = "video"
		case ".mov":
			draft.ContentType = "video/quicktime"
			draft.Type = "video"
		case ".avi":
			draft.ContentType = "video/x-msvideo"
			draft.Type = "video"
		case ".mkv":
			draft.ContentType = "video/x-matroska"
			draft.Type = "video"

		// Documents
		case ".pdf":
			draft.ContentType = "application/pdf"
			draft.Type = "document"
			draft.Subtype = "pdf"
		case ".md", ".markdown":
			draft.ContentType = "text/markdown"
			draft.Type = "document"
			draft.Subtype = "markdown"
		case ".go", ".py", ".js", ".ts", ".rs", ".java", ".rb", ".cpp", ".c", ".cs":
			draft.ContentType = "text/plain"
			draft.Type = "document"
			draft.Subtype = "code"
		case ".docx":
			draft.ContentType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
			draft.Type = "document"
			draft.Subtype = "office"
		case ".epub":
			draft.ContentType = "application/epub+zip"
			draft.Type = "document"
			draft.Subtype = "epub"
		case ".html", ".htm":
			draft.ContentType = "text/html"
			draft.Type = "document"
			draft.Subtype = "html"

		default:
			return nil, fmt.Errorf("format_detector: unsupported format %q", ext)
		}
	}

	return draft, nil
}
