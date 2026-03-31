package steps

// E2E tests derived from real production failures.
//
// Each test runs url_fetcher → content_type_router together (the first two
// steps of url.generic) against a local httptest server, asserting that the
// router correctly classifies the response and that format-specific extractors
// skip or run as appropriate.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// runURLPipeline runs url_fetcher → content_type_router against srv for url.
func runURLPipeline(t *testing.T, srv *httptest.Server, rawURL string) *storage.KnowledgeObject {
	t.Helper()
	draft := &storage.KnowledgeObject{Source: rawURL}

	fetcher := NewURLFetcher(WithURLHTTPClient(srv.Client()))
	out, err := fetcher.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("url_fetcher: %v", err)
	}

	router := NewContentTypeRouter()
	out, err = router.Run(context.Background(), out)
	if err != nil {
		t.Fatalf("content_type_router: %v", err)
	}
	return out
}

// TestE2E_HTMLPage_SkipsPDFExtractor reproduces the most common prod failure:
// url.generic ran pdf_extractor on plain HTML pages, causing pdftotext errors.
func TestE2E_HTMLPage_SkipsPDFExtractor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<!DOCTYPE html><html><body><h1>AWS MCP Servers</h1></body></html>`))
	}))
	defer srv.Close()

	out := runURLPipeline(t, srv, srv.URL+"/")

	if out.Subtype != "html" {
		t.Errorf("Subtype: got %q, want %q", out.Subtype, "html")
	}
	if out.ContentType != "text/html" {
		t.Errorf("ContentType: got %q, want %q", out.ContentType, "text/html")
	}

	// pdf_extractor must skip when Subtype != "pdf".
	pdf := NewPDFExtractor()
	result, err := pdf.Run(context.Background(), out)
	if err != nil {
		t.Fatalf("pdf_extractor returned error on HTML: %v", err)
	}
	if result.RawContent != out.RawContent {
		t.Error("pdf_extractor must not modify RawContent when skipping")
	}
}

// TestE2E_PDFUrl_RunsPDFExtractor verifies that a URL serving application/pdf
// correctly routes to subtype=pdf and pdf_extractor runs (not skipped).
func TestE2E_PDFUrl_RunsPDFExtractor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		// Minimal valid-looking PDF header — enough for routing; stub provider handles extraction.
		w.Write([]byte("%PDF-1.4 fake content"))
	}))
	defer srv.Close()

	out := runURLPipeline(t, srv, srv.URL+"/paper.pdf")

	if out.Subtype != "pdf" {
		t.Errorf("Subtype: got %q, want %q", out.Subtype, "pdf")
	}

	// pdf_extractor should not skip (stub provider returns empty result, not error).
	pdf := NewPDFExtractor()
	_, err := pdf.Run(context.Background(), out)
	if err != nil {
		t.Errorf("pdf_extractor errored on pdf subtype: %v", err)
	}
}

// TestE2E_OctetStream_FallsBackToExtension covers application/octet-stream
// responses where the URL extension is the only signal (common with raw file servers).
func TestE2E_OctetStream_FallsBackToExtension(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write([]byte("%PDF-1.4 content"))
	}))
	defer srv.Close()

	out := runURLPipeline(t, srv, srv.URL+"/report.pdf")

	if out.Subtype != "pdf" {
		t.Errorf("Subtype: got %q, want %q — ext fallback should have matched", out.Subtype, "pdf")
	}
}

// TestE2E_OfficeDoc_SkipsPDFExtractor verifies .docx URLs don't trigger pdf_extractor.
func TestE2E_OfficeDoc_SkipsPDFExtractor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
		w.Write([]byte("PK fake docx content"))
	}))
	defer srv.Close()

	out := runURLPipeline(t, srv, srv.URL+"/doc.docx")

	if out.Subtype != "office" {
		t.Errorf("Subtype: got %q, want %q", out.Subtype, "office")
	}

	pdf := NewPDFExtractor()
	result, err := pdf.Run(context.Background(), out)
	if err != nil {
		t.Fatalf("pdf_extractor returned error on office doc: %v", err)
	}
	if result.Sections != nil {
		t.Error("pdf_extractor must not produce sections when skipping")
	}
}

// TestE2E_UnknownContentType_MarkedUnsupported verifies that unknown MIME types
// are logged and marked unsupported rather than failing the job.
func TestE2E_UnknownContentType_MarkedUnsupported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-proprietary-format")
		w.Write([]byte("binary blob"))
	}))
	defer srv.Close()

	out := runURLPipeline(t, srv, srv.URL+"/data.bin")

	if out.Subtype != "unsupported" {
		t.Errorf("Subtype: got %q, want %q", out.Subtype, "unsupported")
	}

	// pdf_extractor must still not error — it skips on anything that isn't "pdf".
	pdf := NewPDFExtractor()
	_, err := pdf.Run(context.Background(), out)
	if err != nil {
		t.Fatalf("pdf_extractor must not error on unsupported type: %v", err)
	}
}

// TestE2E_ImageURL_SkipsPDFExtractor covers image responses that previously
// would have been passed to pdf_extractor (e.g. linked images in GitHub repos).
func TestE2E_ImageURL_SkipsPDFExtractor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("\x89PNG\r\n\x1a\n")) // PNG magic bytes
	}))
	defer srv.Close()

	out := runURLPipeline(t, srv, srv.URL+"/logo.png")

	if out.Subtype != "image" {
		t.Errorf("Subtype: got %q, want %q", out.Subtype, "image")
	}

	pdf := NewPDFExtractor()
	_, err := pdf.Run(context.Background(), out)
	if err != nil {
		t.Fatalf("pdf_extractor must not error on image: %v", err)
	}
}

// TestE2E_CharsetStripped verifies Content-Type params (charset, boundary) don't
// confuse the router — "text/html; charset=utf-8" must route as "html".
func TestE2E_CharsetStripped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<html><body>content</body></html>`))
	}))
	defer srv.Close()

	out := runURLPipeline(t, srv, srv.URL+"/page")

	if out.ContentType != "text/html" {
		t.Errorf("ContentType: got %q — charset must be stripped", out.ContentType)
	}
	if out.Subtype != "html" {
		t.Errorf("Subtype: got %q, want %q", out.Subtype, "html")
	}
}
