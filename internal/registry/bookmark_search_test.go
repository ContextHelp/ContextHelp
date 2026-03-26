package registry_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- helpers ---

func makeBookmarkServer(t *testing.T, statusCode int, bookmarks []registry.RemoteBookmark) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bookmarks/search" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		if statusCode == http.StatusOK {
			_ = json.NewEncoder(w).Encode(bookmarks)
		}
	}))
}

func sampleBookmarks() []registry.RemoteBookmark {
	return []registry.RemoteBookmark{
		{
			ID:          "bm-1",
			Title:       "Design Patterns",
			URL:         "https://example.com/design-patterns",
			Description: "A guide to GoF patterns",
			Tags:        []string{"design", "patterns"},
			Mentions:    []string{"@oop.principle"},
			EntityIDs:   []string{"oop.principle"},
			CreatedAt:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			ID:        "bm-2",
			Title:     "Clean Architecture",
			URL:       "https://example.com/clean-arch",
			Tags:      []string{"architecture"},
			CreatedAt: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		},
	}
}

// --- BookmarkSearchClient.Search ---

func TestBookmarkSearchClient_Search_OK(t *testing.T) {
	bms := sampleBookmarks()
	srv := makeBookmarkServer(t, http.StatusOK, bms)
	defer srv.Close()

	client := registry.NewBookmarkSearchClient(2 * time.Second)
	got, err := client.Search(context.Background(), srv.URL, "design")

	require.NoError(t, err)
	require.Len(t, got, 2)

	assert.Equal(t, "bm-1", got[0].ID)
	assert.Equal(t, "Design Patterns", got[0].Title)
	assert.Equal(t, srv.URL, got[0].RegistryURL, "RegistryURL must be stamped by client")
	assert.Equal(t, srv.URL, got[1].RegistryURL)
}

func TestBookmarkSearchClient_Search_NotFound_ReturnsNil(t *testing.T) {
	// 404 means registry doesn't expose the endpoint; should not error.
	srv := makeBookmarkServer(t, http.StatusNotFound, nil)
	defer srv.Close()

	client := registry.NewBookmarkSearchClient(2 * time.Second)
	got, err := client.Search(context.Background(), srv.URL, "anything")

	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestBookmarkSearchClient_Search_ServerError_ReturnsError(t *testing.T) {
	srv := makeBookmarkServer(t, http.StatusInternalServerError, nil)
	defer srv.Close()

	client := registry.NewBookmarkSearchClient(2 * time.Second)
	_, err := client.Search(context.Background(), srv.URL, "anything")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestBookmarkSearchClient_Search_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until client cancels.
		<-r.Context().Done()
		http.Error(w, "cancelled", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	client := registry.NewBookmarkSearchClient(2 * time.Second)
	_, err := client.Search(ctx, srv.URL, "anything")

	require.Error(t, err)
}

func TestBookmarkSearchClient_Search_InvalidURL_ReturnsError(t *testing.T) {
	client := registry.NewBookmarkSearchClient(2 * time.Second)
	_, err := client.Search(context.Background(), "://bad-url", "q")
	require.Error(t, err)
}

// --- ScatterGather ---

func TestScatterGather_AllSucceed(t *testing.T) {
	bms := sampleBookmarks()
	srv1 := makeBookmarkServer(t, http.StatusOK, bms[:1])
	srv2 := makeBookmarkServer(t, http.StatusOK, bms[1:])
	defer srv1.Close()
	defer srv2.Close()

	client := registry.NewBookmarkSearchClient(2 * time.Second)
	results := registry.ScatterGather(
		context.Background(), client,
		[]string{srv1.URL, srv2.URL},
		"design",
	)

	require.Len(t, results, 2)
	assert.NoError(t, results[0].Err)
	assert.NoError(t, results[1].Err)
	assert.Len(t, results[0].Bookmarks, 1)
	assert.Len(t, results[1].Bookmarks, 1)
}

func TestScatterGather_OneRegistryDown_OtherSucceeds(t *testing.T) {
	bms := sampleBookmarks()
	good := makeBookmarkServer(t, http.StatusOK, bms)
	defer good.Close()

	// Non-listening port to simulate unreachable registry.
	bad := "http://127.0.0.1:1" // port 1 is reserved; connection refused

	client := registry.NewBookmarkSearchClient(500 * time.Millisecond)
	results := registry.ScatterGather(
		context.Background(), client,
		[]string{good.URL, bad},
		"design",
	)

	require.Len(t, results, 2)

	var goodResult, badResult registry.SourceResult
	for _, r := range results {
		if r.RegistryURL == good.URL {
			goodResult = r
		} else {
			badResult = r
		}
	}

	assert.NoError(t, goodResult.Err)
	require.Len(t, goodResult.Bookmarks, 2)
	assert.Error(t, badResult.Err, "bad registry must record an error")
}

func TestScatterGather_Empty_ReturnsNil(t *testing.T) {
	client := registry.NewBookmarkSearchClient(0)
	results := registry.ScatterGather(context.Background(), client, nil, "q")
	assert.Nil(t, results)
}

// --- MergeResults ---

func TestMergeResults_DeduplicatesByRegistryAndID(t *testing.T) {
	bms := []registry.RemoteBookmark{
		{ID: "bm-1", URL: "https://a.example.com", Title: "A", CreatedAt: time.Now()},
	}
	results := []registry.SourceResult{
		{RegistryURL: "https://reg1.example.com", Bookmarks: bms},
		{RegistryURL: "https://reg1.example.com", Bookmarks: bms}, // same source+ID
	}

	objs := registry.MergeResults(results)
	require.Len(t, objs, 1, "duplicate (same registry+ID) must be deduplicated")
}

func TestMergeResults_SameIDDifferentRegistry_BothKept(t *testing.T) {
	bm := registry.RemoteBookmark{ID: "bm-1", URL: "https://a.example.com", Title: "A", CreatedAt: time.Now()}
	results := []registry.SourceResult{
		{RegistryURL: "https://reg1.example.com", Bookmarks: []registry.RemoteBookmark{bm}},
		{RegistryURL: "https://reg2.example.com", Bookmarks: []registry.RemoteBookmark{bm}},
	}

	objs := registry.MergeResults(results)
	assert.Len(t, objs, 2, "same ID from different registries must both be included")
}

func TestMergeResults_SkipsFailedSources(t *testing.T) {
	bm := registry.RemoteBookmark{ID: "bm-1", URL: "https://a.example.com", CreatedAt: time.Now()}
	results := []registry.SourceResult{
		{RegistryURL: "https://good.example.com", Bookmarks: []registry.RemoteBookmark{bm}},
		{RegistryURL: "https://bad.example.com", Err: assert.AnError},
	}

	objs := registry.MergeResults(results)
	require.Len(t, objs, 1)
	assert.Equal(t, "https://good.example.com", objs[0].Source)
}

func TestMergeResults_KOFields(t *testing.T) {
	created := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	bm := registry.RemoteBookmark{
		ID:          "bm-42",
		Title:       "Patterns",
		URL:         "https://example.com/p",
		Description: "GoF patterns",
		Tags:        []string{"design", "go"},
		Mentions:    []string{"@oop.principle"},
		EntityIDs:   []string{"oop.principle"},
		Metadata:    map[string]any{"foo": "bar"},
		CreatedAt:   created,
	}
	results := []registry.SourceResult{
		{RegistryURL: "https://reg.example.com", Bookmarks: []registry.RemoteBookmark{bm}},
	}

	objs := registry.MergeResults(results)
	require.Len(t, objs, 1)

	ko := objs[0]
	assert.Equal(t, "url", ko.Type)
	assert.Equal(t, "registry_bookmark", ko.Subtype)
	assert.Equal(t, "https://example.com/p", ko.RawContent)
	assert.Equal(t, "https://reg.example.com", ko.Source)
	assert.Equal(t, []string{"GoF patterns"}, ko.Summaries)
	assert.Equal(t, "registry", ko.Status)
	assert.Equal(t, created, ko.CreatedAt)

	require.Len(t, ko.Tags, 2)
	assert.Equal(t, "design", ko.Tags[0].Label)
	assert.Equal(t, "registry", ko.Tags[0].Source)

	assert.Equal(t, "https://reg.example.com", ko.Metadata["registry_source"])
	assert.Equal(t, "bm-42", ko.Metadata["registry_bookmark_id"])
	assert.Equal(t, []string{"oop.principle"}, ko.Metadata["registry_entity_ids"])
	assert.Equal(t, []string{"@oop.principle"}, ko.Metadata["registry_mentions"])
	assert.Equal(t, "bar", ko.Metadata["foo"])
}

func TestMergeResults_Empty_ReturnsNil(t *testing.T) {
	objs := registry.MergeResults(nil)
	assert.Nil(t, objs)
}
