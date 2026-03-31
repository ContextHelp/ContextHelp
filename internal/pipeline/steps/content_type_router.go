package steps

import (
	"context"
	"log"
	"path/filepath"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Handler registers detection logic and the subtype to assign on a match.
// Each check function is optional; any one matching is sufficient.
// Earlier-registered handlers take priority — register more-specific ones first.
type Handler struct {
	// MIMECheck matches against the Content-Type header value (lowercased, params stripped).
	MIMECheck func(mime string) bool
	// ExtCheck matches against the lowercased file extension (e.g. ".pdf").
	ExtCheck func(ext string) bool
	// URLCheck matches against the raw source URL.
	URLCheck func(url string) bool
	// Subtype is assigned to draft.Subtype when any check passes.
	Subtype string
}

// handlers is the ordered registry. Register more-specific handlers first.
var handlers []Handler

// RegisterContentTypeHandler appends a handler to the global registry.
// Call from init() to keep each format self-contained and independently extensible.
func RegisterContentTypeHandler(h Handler) {
	handlers = append(handlers, h)
}

func init() {
	// Documents
	RegisterContentTypeHandler(Handler{
		MIMECheck: func(m string) bool { return strings.Contains(m, "pdf") },
		ExtCheck:  func(e string) bool { return e == ".pdf" },
		Subtype:   "pdf",
	})
	RegisterContentTypeHandler(Handler{
		MIMECheck: func(m string) bool {
			return strings.Contains(m, "officedocument") || strings.Contains(m, "msword")
		},
		ExtCheck: func(e string) bool { return e == ".docx" || e == ".doc" || e == ".pptx" || e == ".xlsx" },
		Subtype:  "office",
	})
	RegisterContentTypeHandler(Handler{
		MIMECheck: func(m string) bool { return strings.Contains(m, "epub") },
		ExtCheck:  func(e string) bool { return e == ".epub" },
		Subtype:   "epub",
	})
	RegisterContentTypeHandler(Handler{
		MIMECheck: func(m string) bool { return strings.Contains(m, "markdown") },
		ExtCheck:  func(e string) bool { return e == ".md" || e == ".markdown" },
		Subtype:   "markdown",
	})
	RegisterContentTypeHandler(Handler{
		MIMECheck: func(m string) bool { return strings.Contains(m, "csv") },
		ExtCheck:  func(e string) bool { return e == ".csv" },
		Subtype:   "csv",
	})
	RegisterContentTypeHandler(Handler{
		MIMECheck: func(m string) bool { return strings.Contains(m, "json") },
		ExtCheck:  func(e string) bool { return e == ".json" },
		Subtype:   "json",
	})
	RegisterContentTypeHandler(Handler{
		MIMECheck: func(m string) bool { return strings.Contains(m, "xml") },
		ExtCheck:  func(e string) bool { return e == ".xml" },
		Subtype:   "xml",
	})

	// Web
	RegisterContentTypeHandler(Handler{
		MIMECheck: func(m string) bool { return strings.Contains(m, "html") },
		ExtCheck:  func(e string) bool { return e == ".html" || e == ".htm" },
		Subtype:   "html",
	})

	// Images
	RegisterContentTypeHandler(Handler{
		MIMECheck: func(m string) bool { return strings.HasPrefix(m, "image/") },
		ExtCheck: func(e string) bool {
			return e == ".png" || e == ".jpg" || e == ".jpeg" || e == ".gif" ||
				e == ".webp" || e == ".bmp" || e == ".tiff" || e == ".tif"
		},
		Subtype: "image",
	})

	// Audio
	RegisterContentTypeHandler(Handler{
		MIMECheck: func(m string) bool { return strings.HasPrefix(m, "audio/") },
		ExtCheck: func(e string) bool {
			return e == ".mp3" || e == ".wav" || e == ".ogg" || e == ".flac" || e == ".m4a"
		},
		Subtype: "audio",
	})

	// Video
	RegisterContentTypeHandler(Handler{
		MIMECheck: func(m string) bool { return strings.HasPrefix(m, "video/") },
		ExtCheck: func(e string) bool {
			return e == ".mp4" || e == ".mov" || e == ".avi" || e == ".mkv" || e == ".webm"
		},
		Subtype: "video",
	})

	// Plain text — last resort; catches text/plain, source files, etc.
	RegisterContentTypeHandler(Handler{
		MIMECheck: func(m string) bool { return strings.HasPrefix(m, "text/") },
		ExtCheck: func(e string) bool {
			return e == ".txt" || e == ".go" || e == ".py" || e == ".js" || e == ".ts" ||
				e == ".rs" || e == ".java" || e == ".rb" || e == ".cpp" || e == ".c" || e == ".cs"
		},
		Subtype: "text",
	})
}

// ContentTypeRouter promotes the HTTP Content-Type from Metadata into
// draft.ContentType, then resolves draft.Subtype via the handler registry.
// Unknown types are logged for future support rather than failed.
type ContentTypeRouter struct {
	pipeline.BaseContract
}

func NewContentTypeRouter() *ContentTypeRouter {
	return &ContentTypeRouter{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"Metadata"},
			Produces: []string{"ContentType", "Subtype"},
		}),
	}
}

func (s *ContentTypeRouter) Name() string { return "content_type_router" }

func (s *ContentTypeRouter) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		return draft, nil
	}

	// Promote HTTP Content-Type header into the typed field.
	if draft.ContentType == "" {
		if ct, ok := draft.Metadata["content_type"].(string); ok && ct != "" {
			// Strip charset and parameters: "text/html; charset=utf-8" → "text/html"
			draft.ContentType = strings.ToLower(strings.TrimSpace(strings.SplitN(ct, ";", 2)[0]))
		}
	}

	mime := draft.ContentType
	ext := strings.ToLower(filepath.Ext(draft.Source))
	src := draft.Source

	for _, h := range handlers {
		if h.MIMECheck != nil && mime != "" && mime != "application/octet-stream" && h.MIMECheck(mime) {
			draft.Subtype = h.Subtype
			return draft, nil
		}
		if h.ExtCheck != nil && ext != "" && h.ExtCheck(ext) {
			draft.Subtype = h.Subtype
			return draft, nil
		}
		if h.URLCheck != nil && src != "" && h.URLCheck(src) {
			draft.Subtype = h.Subtype
			return draft, nil
		}
	}

	if mime == "" && ext == "" {
		return draft, nil
	}

	log.Printf("content_type_router: no handler for mime=%q ext=%q source=%s — skipping extraction", mime, ext, src)
	draft.Subtype = "unsupported"
	return draft, nil
}
