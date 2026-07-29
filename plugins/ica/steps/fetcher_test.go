package steps

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestICAFetcherSuccess(t *testing.T) {
	items := []map[string]string{
		{"title": "Post 1", "url": "https://example.com/1"},
		{"title": "Post 2", "url": "https://example.com/2"},
	}
	body, _ := json.Marshal(items)

	srv := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/feeds/sync" {
				t.Errorf("path: got %s", r.URL.Path)
			}
			if got := r.URL.Query().Get("url"); got == "" {
				t.Error("missing url query param")
			}
			w.Header().Set("ETag", `"feed-etag"`)
			w.Header().Set(
				"Last-Modified",
				"Mon, 01 Jan 2024 00:00:00 GMT",
			)
			w.WriteHeader(http.StatusOK)
			w.Write(body)
		}),
	)
	defer srv.Close()

	step := NewICAFetcher(srv.URL, srv.Client())
	draft := &storage.KnowledgeObject{
		Source: "https://example.com/feed.xml",
	}

	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.RawContent == "" {
		t.Error("expected non-empty RawContent")
	}
	if got.Metadata["etag"] != `"feed-etag"` {
		t.Errorf("etag: got %v", got.Metadata["etag"])
	}
	if got.Metadata["last_modified"] !=
		"Mon, 01 Jan 2024 00:00:00 GMT" {
		t.Errorf(
			"last_modified: got %v",
			got.Metadata["last_modified"],
		)
	}
	fi, ok := got.Metadata["feed_items"].([]any)
	if !ok || len(fi) != 2 {
		t.Errorf("feed_items: got %v", got.Metadata["feed_items"])
	}
}

func TestICAFetcherNotModified(t *testing.T) {
	srv := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("If-None-Match") != `"old-etag"` {
				t.Error("expected If-None-Match header")
			}
			w.WriteHeader(http.StatusNotModified)
		}),
	)
	defer srv.Close()

	step := NewICAFetcher(srv.URL, srv.Client())
	draft := &storage.KnowledgeObject{
		Source:   "https://example.com/feed.xml",
		Metadata: map[string]any{"etag": `"old-etag"`},
	}

	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Metadata["not_modified"] != true {
		t.Errorf(
			"not_modified: got %v",
			got.Metadata["not_modified"],
		)
	}
}

func TestICAFetcherFeedGone(t *testing.T) {
	srv := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}),
	)
	defer srv.Close()

	step := NewICAFetcher(srv.URL, srv.Client())
	draft := &storage.KnowledgeObject{
		Source: "https://example.com/feed.xml",
	}

	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Metadata["feed_gone"] != true {
		t.Errorf("feed_gone: got %v", got.Metadata["feed_gone"])
	}
}

func TestICAFetcherServerError(t *testing.T) {
	srv := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}),
	)
	defer srv.Close()

	step := NewICAFetcher(srv.URL, srv.Client())
	draft := &storage.KnowledgeObject{
		Source: "https://example.com/feed.xml",
	}

	_, err := step.Run(context.Background(), draft)
	if err == nil {
		t.Fatal("expected error for 500 status")
	}
}

func TestICAFetcherEmptySource(t *testing.T) {
	step := NewICAFetcher("http://localhost", nil)
	draft := &storage.KnowledgeObject{}

	_, err := step.Run(context.Background(), draft)
	if err == nil {
		t.Fatal("expected error for empty source")
	}
}
