package steps

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestFeedFetcherSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"abc123"`)
		w.Header().Set("Last-Modified", "Mon, 01 Jan 2024 00:00:00 GMT")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<rss><channel><title>Test</title></channel></rss>"))
	}))
	defer srv.Close()

	step := NewFeedFetcher(WithHTTPClient(srv.Client()))
	draft := &storage.KnowledgeObject{Source: srv.URL}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.RawContent == "" {
		t.Error("expected non-empty RawContent")
	}
	if got.Metadata["feed_url"] != srv.URL {
		t.Errorf("feed_url: got %v", got.Metadata["feed_url"])
	}
	if got.Metadata["etag"] != `"abc123"` {
		t.Errorf("etag: got %v", got.Metadata["etag"])
	}
}

func TestFeedFetcherNotModified(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotModified)
	}))
	defer srv.Close()

	step := NewFeedFetcher(WithHTTPClient(srv.Client()))
	draft := &storage.KnowledgeObject{Source: srv.URL}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Metadata["not_modified"] != true {
		t.Errorf("not_modified: got %v", got.Metadata["not_modified"])
	}
}

func TestFeedFetcherGone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGone)
	}))
	defer srv.Close()

	step := NewFeedFetcher(WithHTTPClient(srv.Client()))
	draft := &storage.KnowledgeObject{Source: srv.URL}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Metadata["feed_gone"] != true {
		t.Errorf("feed_gone: got %v", got.Metadata["feed_gone"])
	}
}

func TestFeedFetcherNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	step := NewFeedFetcher(WithHTTPClient(srv.Client()))
	draft := &storage.KnowledgeObject{Source: srv.URL}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Metadata["feed_gone"] != true {
		t.Errorf("feed_gone: got %v", got.Metadata["feed_gone"])
	}
}

func TestFeedFetcherNoSource(t *testing.T) {
	step := NewFeedFetcher()
	draft := &storage.KnowledgeObject{}
	_, err := step.Run(context.Background(), draft)
	if err == nil {
		t.Fatal("expected error for missing source")
	}
}

func TestFeedFetcherServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	step := NewFeedFetcher(WithHTTPClient(srv.Client()))
	draft := &storage.KnowledgeObject{Source: srv.URL}
	_, err := step.Run(context.Background(), draft)
	if err == nil {
		t.Fatal("expected error for 500 status")
	}
}
