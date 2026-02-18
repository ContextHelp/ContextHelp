package notion

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSearchPagesPaginationAndSinceFilter(t *testing.T) {
	t.Parallel()

	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/search" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("authorization header = %q", got)
		}
		if got := r.Header.Get("Notion-Version"); got != defaultNotionVersion {
			t.Fatalf("notion version header = %q", got)
		}

		var reqBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}

		requests++
		switch requests {
		case 1:
			if _, hasCursor := reqBody["start_cursor"]; hasCursor {
				t.Fatalf("unexpected start_cursor in first request")
			}
			writeJSON(t, w, http.StatusOK, map[string]any{
				"results": []map[string]any{
					notionPageResult("page-recent", "Recent Page", "2026-02-15T10:00:00Z"),
					notionPageResult("page-old", "Old Page", "2024-01-01T09:00:00Z"),
				},
				"has_more":    true,
				"next_cursor": "cursor-2",
			})
		case 2:
			if got, _ := reqBody["start_cursor"].(string); got != "cursor-2" {
				t.Fatalf("second request start_cursor = %q", got)
			}
			writeJSON(t, w, http.StatusOK, map[string]any{
				"results": []map[string]any{
					notionPageResult("page-new", "New Page", "2026-02-18T12:00:00Z"),
				},
				"has_more":    false,
				"next_cursor": "",
			})
		default:
			t.Fatalf("unexpected request count: %d", requests)
		}
	}))
	defer srv.Close()

	client := NewClient(nil, srv.URL, "test-token")
	since := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	pages, err := client.SearchPages(context.Background(), "", &since, 0)
	if err != nil {
		t.Fatalf("SearchPages returned error: %v", err)
	}
	if requests != 2 {
		t.Fatalf("expected 2 search requests, got %d", requests)
	}
	if len(pages) != 2 {
		t.Fatalf("expected 2 filtered pages, got %d", len(pages))
	}
	if pages[0].ID != "page-recent" || pages[1].ID != "page-new" {
		t.Fatalf("unexpected page IDs: %#v", pages)
	}
}

func TestQueryDatabasePagesRespectsMaxItems(t *testing.T) {
	t.Parallel()

	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/databases/db-1/query" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}

		requests++
		var reqBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if got, _ := reqBody["page_size"].(float64); got != 1 {
			t.Fatalf("page_size = %v, want 1", got)
		}

		writeJSON(t, w, http.StatusOK, map[string]any{
			"results": []map[string]any{
				notionPageResult("db-page-1", "DB Page 1", "2026-02-18T10:00:00Z"),
				notionPageResult("db-page-2", "DB Page 2", "2026-02-18T09:00:00Z"),
			},
			"has_more":    true,
			"next_cursor": "cursor-2",
		})
	}))
	defer srv.Close()

	client := NewClient(nil, srv.URL, "test-token")
	pages, err := client.QueryDatabasePages(context.Background(), "db-1", nil, 1)
	if err != nil {
		t.Fatalf("QueryDatabasePages returned error: %v", err)
	}
	if requests != 1 {
		t.Fatalf("expected one request, got %d", requests)
	}
	if len(pages) != 1 {
		t.Fatalf("expected one page, got %d", len(pages))
	}
	if pages[0].ID != "db-page-1" {
		t.Fatalf("unexpected page id: %s", pages[0].ID)
	}
}

func TestPageContentPaginationAndRender(t *testing.T) {
	t.Parallel()

	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/blocks/page-1/children" || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}

		requests++
		switch requests {
		case 1:
			if got := r.URL.Query().Get("start_cursor"); got != "" {
				t.Fatalf("unexpected start_cursor in first request: %q", got)
			}
			writeJSON(t, w, http.StatusOK, map[string]any{
				"results": []map[string]any{
					{
						"type": "paragraph",
						"paragraph": map[string]any{
							"rich_text": []map[string]any{
								{"plain_text": "Hello"},
							},
						},
					},
				},
				"has_more":    true,
				"next_cursor": "cursor-2",
			})
		case 2:
			if got := r.URL.Query().Get("start_cursor"); got != "cursor-2" {
				t.Fatalf("second request start_cursor = %q", got)
			}
			writeJSON(t, w, http.StatusOK, map[string]any{
				"results": []map[string]any{
					{
						"type": "to_do",
						"to_do": map[string]any{
							"rich_text": []map[string]any{
								{"plain_text": "From page two"},
							},
						},
					},
				},
				"has_more":    false,
				"next_cursor": "",
			})
		default:
			t.Fatalf("unexpected request count: %d", requests)
		}
	}))
	defer srv.Close()

	client := NewClient(nil, srv.URL, "test-token")
	body, err := client.PageContent(context.Background(), "page-1")
	if err != nil {
		t.Fatalf("PageContent returned error: %v", err)
	}
	if requests != 2 {
		t.Fatalf("expected 2 requests, got %d", requests)
	}
	if body != "Hello\n\nFrom page two" {
		t.Fatalf("unexpected body: %q", body)
	}

	rendered := RenderContent(Page{
		ID:             "page-1",
		Title:          "Imported Page",
		URL:            "https://notion.so/page-1",
		LastEditedTime: time.Date(2026, 2, 18, 12, 0, 0, 0, time.UTC),
	}, body)
	if !strings.Contains(rendered, "# Imported Page") {
		t.Fatalf("rendered content missing title: %q", rendered)
	}
	if !strings.Contains(rendered, "Source: https://notion.so/page-1") {
		t.Fatalf("rendered content missing source: %q", rendered)
	}
	if !strings.Contains(rendered, "Last Edited: 2026-02-18T12:00:00Z") {
		t.Fatalf("rendered content missing timestamp: %q", rendered)
	}
	if !strings.Contains(rendered, "From page two") {
		t.Fatalf("rendered content missing body: %q", rendered)
	}
}

func TestGetPageNonPageObject(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/pages/db-like-id" || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		writeJSON(t, w, http.StatusOK, map[string]any{
			"object":           "database",
			"id":               "db-like-id",
			"url":              "https://notion.so/db-like-id",
			"last_edited_time": "2026-02-18T10:00:00Z",
			"properties":       map[string]any{},
		})
	}))
	defer srv.Close()

	client := NewClient(nil, srv.URL, "test-token")
	_, err := client.GetPage(context.Background(), "db-like-id")
	if err == nil {
		t.Fatal("expected error for non-page object")
	}
	if !strings.Contains(err.Error(), "not a page") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func notionPageResult(id, title, edited string) map[string]any {
	return map[string]any{
		"object":           "page",
		"id":               id,
		"url":              "https://notion.so/" + id,
		"last_edited_time": edited,
		"properties": map[string]any{
			"title": map[string]any{
				"type": "title",
				"title": []map[string]any{
					{"plain_text": title},
				},
			},
		},
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}
