package integration

// US-0314: Pinboard import (API client + JSON export fixture).

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/importer/pinboard"
)

type pinboardPost struct {
	Href        string `json:"href"`
	Description string `json:"description"`
	Extended    string `json:"extended"`
	Tags        string `json:"tags"`
	Time        string `json:"time"`
	Hash        string `json:"hash"`
	Shared      string `json:"shared"`
	ToRead      string `json:"toread"`
}

func makePinboardServer(t *testing.T, posts []pinboardPost) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/posts/all" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("auth_token") == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(posts)
	}))
}

// TestUS0314_PinboardFetchPosts verifies bookmarks from mocked Pinboard API.
func TestUS0314_PinboardFetchPosts(t *testing.T) {
	t.Parallel()

	posts := []pinboardPost{
		{
			Href:        "https://go.dev/doc",
			Description: "Go Documentation",
			Extended:    "Official Go docs",
			Tags:        "golang docs",
			Time:        "2026-01-15T10:00:00Z",
			Hash:        "abc123",
			Shared:      "yes",
			ToRead:      "no",
		},
		{
			Href:        "https://pkg.go.dev",
			Description: "Go Packages",
			Tags:        "golang packages",
			Time:        "2026-01-14T09:00:00Z",
			Hash:        "def456",
			Shared:      "no",
			ToRead:      "yes",
		},
	}

	srv := makePinboardServer(t, posts)
	defer srv.Close()

	client := pinboard.NewClient(nil, srv.URL, "user:test-token")
	bmarks, err := client.FetchPosts(context.Background(), pinboard.FetchOptions{})
	require.NoError(t, err)
	require.Len(t, bmarks, 2)

	assert.Equal(t, "https://go.dev/doc", bmarks[0].URL)
	assert.Equal(t, "Go Documentation", bmarks[0].Title)
	assert.Equal(t, "abc123", bmarks[0].Hash)
}

// TestUS0314_PinboardTagsExtracted verifies tag splitting.
func TestUS0314_PinboardTagsExtracted(t *testing.T) {
	t.Parallel()

	posts := []pinboardPost{
		{Href: "https://example.com", Description: "Test",
			Tags: "golang testing", Time: "2026-01-15T10:00:00Z", Hash: "h1"},
	}

	srv := makePinboardServer(t, posts)
	defer srv.Close()

	client := pinboard.NewClient(nil, srv.URL, "user:tok")
	bmarks, err := client.FetchPosts(context.Background(), pinboard.FetchOptions{})
	require.NoError(t, err)
	require.Len(t, bmarks, 1)

	assert.Contains(t, bmarks[0].Tags, "golang")
	assert.Contains(t, bmarks[0].Tags, "testing")
}

// TestUS0314_PinboardToReadFlagPreserved verifies ToRead field.
func TestUS0314_PinboardToReadFlagPreserved(t *testing.T) {
	t.Parallel()

	posts := []pinboardPost{
		{Href: "https://a.com", Description: "Read Later", Tags: "", Time: "2026-01-15T10:00:00Z",
			Hash: "h1", ToRead: "yes"},
		{Href: "https://b.com", Description: "Already Read", Tags: "", Time: "2026-01-15T11:00:00Z",
			Hash: "h2", ToRead: "no"},
	}

	srv := makePinboardServer(t, posts)
	defer srv.Close()

	client := pinboard.NewClient(nil, srv.URL, "user:tok")
	bmarks, err := client.FetchPosts(context.Background(), pinboard.FetchOptions{})
	require.NoError(t, err)
	require.Len(t, bmarks, 2)

	assert.True(t, bmarks[0].ToRead, "ToRead must be true")
	assert.False(t, bmarks[1].ToRead, "ToRead must be false")
}

// TestUS0314_PinboardFilterSince verifies FilterBookmarks since filter.
func TestUS0314_PinboardFilterSince(t *testing.T) {
	t.Parallel()

	cutoff := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	recent := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	old := time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC)

	all := []pinboard.Bookmark{
		{URL: "https://recent.com", Title: "Recent", Time: recent},
		{URL: "https://old.com", Title: "Old", Time: old},
	}

	filtered := pinboard.FilterBookmarks(all, &cutoff, nil, 0)
	require.Len(t, filtered, 1)
	assert.Equal(t, "https://recent.com", filtered[0].URL)
}

// TestUS0314_PinboardRenderContent verifies RenderContent output.
func TestUS0314_PinboardRenderContent(t *testing.T) {
	t.Parallel()

	b := pinboard.Bookmark{
		URL:    "https://go.dev",
		Title:  "Go Language",
		Notes:  "Official website",
		Tags:   []string{"golang"},
		Shared: true,
	}
	rendered := pinboard.RenderContent(b)
	assert.NotEmpty(t, rendered)
	assert.Contains(t, rendered, "https://go.dev")
	assert.Contains(t, rendered, "Go Language")
}
