package steps

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

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
