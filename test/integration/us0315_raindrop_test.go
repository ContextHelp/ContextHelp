package integration

// US-0315: Raindrop import (API client mocked).

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/importer/raindrop"
)

func makeRaindropItem(id int64, title, link string, tags []string, updatedAt string) map[string]any {
	return map[string]any{
		"_id":        id,
		"title":      title,
		"link":       link,
		"type":       "link",
		"domain":     strings.Split(link, "/")[2],
		"tags":       tags,
		"lastUpdate": updatedAt,
		"collection": map[string]any{"$id": int64(100)},
	}
}

func makeRaindropServer(t *testing.T, items []map[string]any, collectionID int64) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/rest/v1/collections":
			json.NewEncoder(w).Encode(map[string]any{
				"result": true,
				"items": []map[string]any{
					{"_id": collectionID, "title": "Test Collection"},
				},
			})
		case strings.HasPrefix(r.URL.Path, "/rest/v1/raindrops/"):
			json.NewEncoder(w).Encode(map[string]any{
				"result": true,
				"items":  items,
				"count":  len(items),
			})
		default:
			http.NotFound(w, r)
		}
	}))
}

// TestUS0315_RaindropListCollections verifies collection list from mocked API.
func TestUS0315_RaindropListCollections(t *testing.T) {
	t.Parallel()

	srv := makeRaindropServer(t, nil, 100)
	defer srv.Close()

	client := raindrop.NewClient(nil, srv.URL, "test-token")
	collections, err := client.ListCollections(context.Background())
	require.NoError(t, err)
	require.Len(t, collections, 1)
	assert.Equal(t, int64(100), collections[0].ID)
	assert.Equal(t, "Test Collection", collections[0].Title)
}

// TestUS0315_RaindropListItems verifies bookmark items from mocked API.
func TestUS0315_RaindropListItems(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Format(time.RFC3339)
	items := []map[string]any{
		makeRaindropItem(1001, "Go Documentation", "https://go.dev/doc", []string{"golang"}, now),
		makeRaindropItem(1002, "Hacker News", "https://news.ycombinator.com", []string{"news"}, now),
	}

	srv := makeRaindropServer(t, items, 100)
	defer srv.Close()

	client := raindrop.NewClient(nil, srv.URL, "test-token")
	got, err := client.ListItems(context.Background(), raindrop.ListOptions{
		CollectionIDs: []int64{100},
	})
	require.NoError(t, err)
	require.Len(t, got, 2)

	assert.Equal(t, int64(1001), got[0].ID)
	assert.Equal(t, "Go Documentation", got[0].Title)
	assert.Equal(t, "https://go.dev/doc", got[0].Link)
}

// TestUS0315_RaindropRequiresToken verifies token validation.
func TestUS0315_RaindropRequiresToken(t *testing.T) {
	t.Parallel()

	srv := makeRaindropServer(t, nil, 100)
	defer srv.Close()

	client := raindrop.NewClient(nil, srv.URL, "")
	_, err := client.ListCollections(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "token")
}

// TestUS0315_RaindropRequiresCollectionID verifies collection ID validation.
func TestUS0315_RaindropRequiresCollectionID(t *testing.T) {
	t.Parallel()

	srv := makeRaindropServer(t, nil, 100)
	defer srv.Close()

	client := raindrop.NewClient(nil, srv.URL, "tok")
	_, err := client.ListItems(context.Background(), raindrop.ListOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "collection")
}

// TestUS0315_RaindropRenderContent verifies RenderContent output.
func TestUS0315_RaindropRenderContent(t *testing.T) {
	t.Parallel()

	item := raindrop.Item{
		ID:      1001,
		Title:   "Go Documentation",
		Link:    "https://go.dev/doc",
		Type:    "link",
		Domain:  "go.dev",
		Excerpt: "Official Go docs",
		Tags:    []string{"golang", "docs"},
	}
	rendered := raindrop.RenderContent(item, "Engineering")
	assert.NotEmpty(t, rendered)
	assert.Contains(t, rendered, "Go Documentation")
	assert.Contains(t, rendered, "https://go.dev/doc")
}
