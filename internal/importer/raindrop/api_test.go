package raindrop

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestListItemsPaginationAndFilters(t *testing.T) {
	t.Parallel()

	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/v1/raindrops/0" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("authorization header = %q", got)
		}

		page := r.URL.Query().Get("page")
		perPage := r.URL.Query().Get("perpage")
		requests++

		switch requests {
		case 1:
			if page != "0" || perPage != "50" {
				t.Fatalf("unexpected pagination params page=%s perpage=%s", page, perPage)
			}
			items := make([]map[string]any, 0, 50)
			items = append(items, map[string]any{
				"_id":        1001,
				"title":      "Alpha",
				"link":       "https://example.com/a",
				"tags":       []string{"work"},
				"lastUpdate": "2026-02-15T10:00:00Z",
				"collection": map[string]any{"$id": 55},
			})
			items = append(items, map[string]any{
				"_id":        1002,
				"title":      "Old",
				"link":       "https://example.com/old",
				"tags":       []string{"work"},
				"lastUpdate": "2025-01-01T10:00:00Z",
				"collection": map[string]any{"$id": 55},
			})
			for i := 0; i < 48; i++ {
				items = append(items, map[string]any{
					"_id":        2000 + i,
					"title":      "Filler",
					"link":       "https://example.com/filler",
					"tags":       []string{"personal"},
					"lastUpdate": "2026-02-18T10:00:00Z",
					"collection": map[string]any{"$id": 55},
				})
			}
			writeRaindropJSON(t, w, http.StatusOK, map[string]any{
				"result": true,
				"items":  items,
				"count":  60,
			})
		case 2:
			if page != "1" || perPage != "50" {
				t.Fatalf("unexpected pagination params page=%s perpage=%s", page, perPage)
			}
			items := make([]map[string]any, 0, 10)
			items = append(items, map[string]any{
				"_id":        1003,
				"title":      "Beta",
				"link":       "https://example.com/b",
				"tags":       []string{"work"},
				"lastUpdate": "2026-02-18T10:00:00Z",
				"collection": map[string]any{"$id": 55},
				"highlights": []map[string]any{
					{"text": "Important line"},
				},
			})
			for i := 0; i < 9; i++ {
				items = append(items, map[string]any{
					"_id":        3000 + i,
					"title":      "Filler 2",
					"link":       "https://example.com/filler2",
					"tags":       []string{"personal"},
					"lastUpdate": "2026-02-18T09:00:00Z",
					"collection": map[string]any{"$id": 55},
				})
			}
			writeRaindropJSON(t, w, http.StatusOK, map[string]any{
				"result": true,
				"items":  items,
				"count":  60,
			})
		default:
			t.Fatalf("unexpected request count %d", requests)
		}
	}))
	defer srv.Close()

	since := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	client := NewClient(nil, srv.URL, "test-token")
	items, err := client.ListItems(context.Background(), ListOptions{
		CollectionIDs: []int64{0},
		Tag:           "work",
		Since:         &since,
		MaxItems:      0,
	})
	if err != nil {
		t.Fatalf("ListItems returned error: %v", err)
	}
	if requests != 2 {
		t.Fatalf("expected 2 requests, got %d", requests)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 filtered items, got %d", len(items))
	}
	if items[0].ID != 1001 || items[1].ID != 1003 {
		t.Fatalf("unexpected item ids: %#v", items)
	}
	if len(items[1].Highlights) != 1 || items[1].Highlights[0] != "Important line" {
		t.Fatalf("unexpected highlights: %#v", items[1].Highlights)
	}
}

func TestListCollectionsAndBuildCollectionPathMap(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/v1/collections" {
			http.NotFound(w, r)
			return
		}
		writeRaindropJSON(t, w, http.StatusOK, map[string]any{
			"result": true,
			"items": []map[string]any{
				{"_id": 1, "title": "Root"},
				{"_id": 2, "title": "Research", "parent": map[string]any{"$id": 1}},
				{"_id": 3, "title": "AI", "parent": map[string]any{"$id": 2}},
			},
		})
	}))
	defer srv.Close()

	client := NewClient(nil, srv.URL, "test-token")
	collections, err := client.ListCollections(context.Background())
	if err != nil {
		t.Fatalf("ListCollections returned error: %v", err)
	}
	if len(collections) != 3 {
		t.Fatalf("expected 3 collections, got %d", len(collections))
	}

	paths := BuildCollectionPathMap(collections)
	if got := paths[3]; got != "Root/Research/AI" {
		t.Fatalf("collection path = %q, want %q", got, "Root/Research/AI")
	}
}

func TestRenderContentIncludesMetadata(t *testing.T) {
	t.Parallel()

	item := Item{
		ID:           42,
		Title:        "Test Bookmark",
		Link:         "https://example.com",
		Type:         "link",
		Domain:       "example.com",
		Excerpt:      "Short summary",
		Note:         "User note",
		Tags:         []string{"work", "ai"},
		Highlights:   []string{"Line 1", "Line 2"},
		CollectionID: 12,
		Created:      time.Date(2026, 2, 10, 10, 0, 0, 0, time.UTC),
		LastUpdate:   time.Date(2026, 2, 18, 11, 0, 0, 0, time.UTC),
	}

	got := RenderContent(item, "Research/AI")
	for _, expected := range []string{
		"# Test Bookmark",
		"Source: https://example.com",
		"Provider: Raindrop.io",
		"Collection: Research/AI",
		"Tags: work, ai",
		"Excerpt:",
		"Note:",
		"Highlights:",
		"- Line 1",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("rendered content missing %q:\n%s", expected, got)
		}
	}
}

func writeRaindropJSON(t *testing.T, w http.ResponseWriter, status int, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}
