package integration

// US-0307: Notion import via mocked API.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/importer/notion"
)

func makeNotionPage(id, title, editedAt string) map[string]any {
	return map[string]any{
		"id":     id,
		"object": "page",
		"url":    "https://notion.so/" + id,
		"last_edited_time": editedAt,
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

func makeNotionServer(t *testing.T, pages []map[string]any, hasMore bool, cursor string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/search" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"results":     pages,
			"has_more":    hasMore,
			"next_cursor": cursor,
		}
		json.NewEncoder(w).Encode(resp)
	}))
}

// TestUS0307_NotionSearchPages verifies page list from mocked Notion API.
func TestUS0307_NotionSearchPages(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	pages := []map[string]any{
		makeNotionPage("page-001", "Project Roadmap", now.Format(time.RFC3339)),
		makeNotionPage("page-002", "Meeting Notes", now.Format(time.RFC3339)),
	}

	srv := makeNotionServer(t, pages, false, "")
	defer srv.Close()

	client := notion.NewClient(nil, srv.URL, "test-token")
	got, err := client.SearchPages(context.Background(), "", nil, 0)
	require.NoError(t, err)
	require.Len(t, got, 2)

	assert.Equal(t, "page-001", got[0].ID)
	assert.Equal(t, "Project Roadmap", got[0].Title)
	assert.NotEmpty(t, got[0].URL)
}

// TestUS0307_NotionRequiresToken verifies token validation.
func TestUS0307_NotionRequiresToken(t *testing.T) {
	t.Parallel()

	srv := makeNotionServer(t, nil, false, "")
	defer srv.Close()

	client := notion.NewClient(nil, srv.URL, "")
	_, err := client.SearchPages(context.Background(), "", nil, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "token")
}

// TestUS0307_NotionMaxItemsRespected verifies MaxItems cap.
func TestUS0307_NotionMaxItemsRespected(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Format(time.RFC3339)
	pages := []map[string]any{
		makeNotionPage("p1", "Page 1", now),
		makeNotionPage("p2", "Page 2", now),
		makeNotionPage("p3", "Page 3", now),
	}

	srv := makeNotionServer(t, pages, false, "")
	defer srv.Close()

	client := notion.NewClient(nil, srv.URL, "tok")
	got, err := client.SearchPages(context.Background(), "", nil, 2)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(got), 2, "MaxItems=2 must be respected")
}

// TestUS0307_NotionLastEditedTimeParsed verifies timestamp decoding.
func TestUS0307_NotionLastEditedTimeParsed(t *testing.T) {
	t.Parallel()

	fixed := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	pages := []map[string]any{
		makeNotionPage("p1", "My Page", fixed.Format(time.RFC3339)),
	}

	srv := makeNotionServer(t, pages, false, "")
	defer srv.Close()

	client := notion.NewClient(nil, srv.URL, "tok")
	got, err := client.SearchPages(context.Background(), "", nil, 0)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, fixed, got[0].LastEditedTime)
}
