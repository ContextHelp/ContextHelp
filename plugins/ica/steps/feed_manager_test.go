package steps

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestICAFeedManagerExistingFeed(t *testing.T) {
	feed := feedResponse{
		ID:         "feed-42",
		URL:        "https://example.com/feed.xml",
		Status:     "active",
		LastSyncAt: "2024-06-01T12:00:00Z",
		ItemCount:  10,
	}

	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				t.Errorf("method: got %s", r.Method)
			}
			if r.URL.Path != "/api/v1/feeds" {
				t.Errorf("path: got %s", r.URL.Path)
			}
			if got := r.URL.Query().Get("url"); got == "" {
				t.Error("missing url query param")
			}
			w.Header().Set(
				"Content-Type", "application/json",
			)
			json.NewEncoder(w).Encode([]feedResponse{feed})
		},
	))
	defer srv.Close()

	step := NewICAFeedManager(srv.URL, srv.Client())
	draft := &storage.KnowledgeObject{
		Source: "https://example.com/feed.xml",
	}

	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Metadata["feed_id"] != "feed-42" {
		t.Errorf(
			"feed_id: got %v", got.Metadata["feed_id"],
		)
	}
	if got.Metadata["feed_status"] != "active" {
		t.Errorf(
			"feed_status: got %v",
			got.Metadata["feed_status"],
		)
	}
	if got.Metadata["feed_last_sync_at"] !=
		"2024-06-01T12:00:00Z" {
		t.Errorf(
			"feed_last_sync_at: got %v",
			got.Metadata["feed_last_sync_at"],
		)
	}
	if got.Metadata["feed_item_count"] != 10 {
		t.Errorf(
			"feed_item_count: got %v",
			got.Metadata["feed_item_count"],
		)
	}
}

func TestICAFeedManagerCreatesFeed(t *testing.T) {
	feed := feedResponse{
		ID:     "feed-new",
		URL:    "https://example.com/new.xml",
		Status: "active",
	}

	var createCalled bool
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set(
				"Content-Type", "application/json",
			)
			switch {
			case r.Method == http.MethodGet &&
				r.URL.Path == "/api/v1/feeds":
				// empty list → feed not found
				json.NewEncoder(w).Encode(
					[]feedResponse{},
				)

			case r.Method == http.MethodPost &&
				r.URL.Path == "/api/v1/feeds":
				createCalled = true
				ct := r.Header.Get("Content-Type")
				if ct != "application/json" {
					t.Errorf(
						"content-type: got %s", ct,
					)
				}
				var body map[string]string
				json.NewDecoder(r.Body).Decode(&body)
				if body["status"] != "active" {
					t.Errorf(
						"status: got %s",
						body["status"],
					)
				}
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(feed)

			default:
				t.Errorf(
					"unexpected %s %s",
					r.Method, r.URL.Path,
				)
				w.WriteHeader(
					http.StatusInternalServerError,
				)
			}
		},
	))
	defer srv.Close()

	step := NewICAFeedManager(srv.URL, srv.Client())
	draft := &storage.KnowledgeObject{
		Source: "https://example.com/new.xml",
	}

	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !createCalled {
		t.Error("expected POST to create feed")
	}
	if got.Metadata["feed_id"] != "feed-new" {
		t.Errorf(
			"feed_id: got %v", got.Metadata["feed_id"],
		)
	}
	if got.Metadata["feed_status"] != "active" {
		t.Errorf(
			"feed_status: got %v",
			got.Metadata["feed_status"],
		)
	}
}

func TestICAFeedManagerAPIUnreachable(t *testing.T) {
	step := NewICAFeedManager(
		"http://127.0.0.1:1", nil,
	)
	draft := &storage.KnowledgeObject{
		Source: "https://example.com/feed.xml",
	}

	_, err := step.Run(context.Background(), draft)
	if err == nil {
		t.Fatal("expected error for unreachable API")
	}
}

func TestICAFeedManagerEmptySource(t *testing.T) {
	step := NewICAFeedManager("http://localhost", nil)
	draft := &storage.KnowledgeObject{}

	_, err := step.Run(context.Background(), draft)
	if err == nil {
		t.Fatal("expected error for empty source")
	}
}
