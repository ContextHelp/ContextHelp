package steps

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestURLFetcher_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html>hello</html>"))
	}))
	defer srv.Close()

	step := NewURLFetcher(WithURLHTTPClient(srv.Client()))
	draft := &storage.KnowledgeObject{Source: srv.URL}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.RawContent != "<html>hello</html>" {
		t.Errorf("RawContent: got %q", got.RawContent)
	}
	if got.Metadata["content_type"] != "text/html" {
		t.Errorf("content_type: got %v", got.Metadata["content_type"])
	}
}

func TestURLFetcher_WithClientBridge(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	bridge := NewClientBridge(
		WithBridgeHTTPClient(srv.Client()),
		WithBridgeUserAgent("TestAgent/3.0"),
	)
	step := NewURLFetcher(WithURLClientBridge(bridge))
	draft := &storage.KnowledgeObject{Source: srv.URL}
	if _, err := step.Run(context.Background(), draft); err != nil {
		t.Fatalf("run: %v", err)
	}
	if gotUA != "TestAgent/3.0" {
		t.Errorf("user-agent: got %q, want %q", gotUA, "TestAgent/3.0")
	}
}

func TestURLFetcher_RawContentFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("body"))
	}))
	defer srv.Close()

	step := NewURLFetcher(WithURLHTTPClient(srv.Client()))
	draft := &storage.KnowledgeObject{RawContent: srv.URL}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.RawContent != "body" {
		t.Errorf("RawContent: got %q", got.RawContent)
	}
}

func TestURLFetcher_NoURL(t *testing.T) {
	step := NewURLFetcher()
	_, err := step.Run(context.Background(), &storage.KnowledgeObject{})
	if err == nil {
		t.Fatal("expected error for missing URL")
	}
}

func TestURLFetcher_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	step := NewURLFetcher(WithURLHTTPClient(srv.Client()))
	draft := &storage.KnowledgeObject{Source: srv.URL}
	_, err := step.Run(context.Background(), draft)
	if err == nil {
		t.Fatal("expected error for 403 status")
	}
}

func TestURLFetcher_PermanentErrors(t *testing.T) {
	codes := []int{
		http.StatusBadRequest,        // 400
		http.StatusForbidden,         // 403
		http.StatusNotFound,          // 404
		http.StatusGone,              // 410
		http.StatusUnprocessableEntity, // 422
		451,                          // Unavailable For Legal Reasons
	}
	for _, code := range codes {
		t.Run(fmt.Sprintf("status_%d", code), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(code)
			}))
			defer srv.Close()

			step := NewURLFetcher(WithURLHTTPClient(srv.Client()))
			draft := &storage.KnowledgeObject{Source: srv.URL}
			_, err := step.Run(context.Background(), draft)
			if err == nil {
				t.Fatalf("expected error for status %d", code)
			}
			if !pipeline.IsPermanent(err) {
				t.Errorf("status %d: expected PermanentError, got %T: %v", code, err, err)
			}
		})
	}
}

func TestURLFetcher_RetryableErrors(t *testing.T) {
	codes := []int{
		http.StatusInternalServerError, // 500
		http.StatusBadGateway,          // 502
		http.StatusServiceUnavailable,  // 503
	}
	for _, code := range codes {
		t.Run(fmt.Sprintf("status_%d", code), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(code)
			}))
			defer srv.Close()

			step := NewURLFetcher(WithURLHTTPClient(srv.Client()))
			draft := &storage.KnowledgeObject{Source: srv.URL}
			_, err := step.Run(context.Background(), draft)
			if err == nil {
				t.Fatalf("expected error for status %d", code)
			}
			if pipeline.IsPermanent(err) {
				t.Errorf("status %d: should be retryable, got PermanentError", code)
			}
		})
	}
}

func TestURLFetcher_NonURLSourceFallsToRawContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("fetched"))
	}))
	defer srv.Close()

	// Source is a non-URL origin string (e.g. set by the CLI).  The fetcher must
	// not attempt GET on it; it must fall back to RawContent which holds the URL.
	nonURLSources := []string{"cli", "argument", "stdin", "clipboard", "import:pinboard", ""}
	for _, src := range nonURLSources {
		draft := &storage.KnowledgeObject{
			Source:     src,
			RawContent: srv.URL,
		}
		step := NewURLFetcher(WithURLHTTPClient(srv.Client()))
		got, err := step.Run(context.Background(), draft)
		if err != nil {
			t.Fatalf("source=%q: unexpected error: %v", src, err)
		}
		if got.RawContent != "fetched" {
			t.Errorf("source=%q: RawContent: got %q, want %q", src, got.RawContent, "fetched")
		}
	}
}

func TestIsHTTPURL(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"http://example.com", true},
		{"https://example.com/path?q=1", true},
		{"cli", false},
		{"argument", false},
		{"stdin", false},
		{"clipboard", false},
		{"import:pinboard", false},
		{"file", false},
		{"", false},
		{"ftp://example.com", false},
		{"//no-scheme.com", false},
	}
	for _, tc := range cases {
		got := isHTTPURL(tc.in)
		if got != tc.want {
			t.Errorf("isHTTPURL(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestURLFetcher_DomainCredential(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("secure"))
	}))
	defer srv.Close()

	bridge := NewClientBridge(
		WithBridgeHTTPClient(srv.Client()),
		WithBridgeDomainCredential(DomainCredential{
			Host:  "127.0.0.1",
			Value: "Bearer secret-tok",
		}),
	)
	step := NewURLFetcher(WithURLClientBridge(bridge))
	draft := &storage.KnowledgeObject{Source: srv.URL}
	if _, err := step.Run(context.Background(), draft); err != nil {
		t.Fatalf("run: %v", err)
	}
	if gotAuth != "Bearer secret-tok" {
		t.Errorf("Authorization: got %q, want %q", gotAuth, "Bearer secret-tok")
	}
}
