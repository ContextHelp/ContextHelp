package integration

// US-0019: Federated Registry Search
// Configures multiple (mock) registries, verifies search fan-out via
// SearchRemoteBookmarks and result merging.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	registry "github.com/ideacrafterslabs/ctxt/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockBookmarkServer starts an httptest server that returns bookmarks for the
// given object IDs as RemoteBookmark JSON on the /bookmarks/search path.
func mockBookmarkServer(t *testing.T, bookmarks []registry.RemoteBookmark) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/bookmarks/search", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(bookmarks); err != nil {
			t.Logf("mock server encode error: %v", err)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestUS0019_FederatedSearchFansOutToRegistries verifies that SearchRemoteBookmarks
// contacts all configured registries and returns merged results.
func TestUS0019_FederatedSearchFansOutToRegistries(t *testing.T) {
	now := nowTrunc()
	reg1Bookmarks := []registry.RemoteBookmark{
		{ID: "fed-r1-01", Title: "distributed tracing opentelemetry", URL: "https://r1.example/1", CreatedAt: now},
	}
	reg2Bookmarks := []registry.RemoteBookmark{
		{ID: "fed-r2-01", Title: "service mesh istio sidecar proxy", URL: "https://r2.example/1", CreatedAt: now},
	}

	srv1 := mockBookmarkServer(t, reg1Bookmarks)
	srv2 := mockBookmarkServer(t, reg2Bookmarks)

	env := startTestEnv(t)
	defer env.stop(t)

	// Inject two registry URLs into the service config.
	env.svc.Cfg.Registries = []config.RegistryConfig{
		{Name: "reg1", URL: srv1.URL},
		{Name: "reg2", URL: srv2.URL},
	}

	ctx := context.Background()
	merged, sources, err := env.svc.SearchRemoteBookmarks(ctx, "tracing", 5*time.Second)
	require.NoError(t, err)

	// Both registries must have been contacted.
	assert.Len(t, sources, 2, "expected results from two registries")

	// Merged results contain one entry per registry.
	assert.Len(t, merged, 2, "federated results must contain entries from all reachable registries")
	bookmarkIDs := make([]string, 0, len(merged))
	for _, o := range merged {
		if id, ok := o.Metadata["registry_bookmark_id"].(string); ok {
			bookmarkIDs = append(bookmarkIDs, id)
		}
	}
	assert.Contains(t, bookmarkIDs, "fed-r1-01")
	assert.Contains(t, bookmarkIDs, "fed-r2-01")
}

// TestUS0019_FederatedSearchOneRegistryDown verifies graceful degradation when
// one registry is unreachable.
func TestUS0019_FederatedSearchOneRegistryDown(t *testing.T) {
	now := nowTrunc()
	aliveBookmarks := []registry.RemoteBookmark{
		{ID: "fed-alive-01", Title: "alive registry result", URL: "https://alive.example/1", CreatedAt: now},
	}

	aliveSrv := mockBookmarkServer(t, aliveBookmarks)

	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Cfg.Registries = []config.RegistryConfig{
		{Name: "alive", URL: aliveSrv.URL},
		{Name: "dead", URL: "http://127.0.0.1:1"}, // nothing listening
	}

	ctx := context.Background()
	merged, sources, err := env.svc.SearchRemoteBookmarks(ctx, "alive", 2*time.Second)
	require.NoError(t, err)

	// Two source records (one failed, one succeeded).
	assert.Len(t, sources, 2)

	// Alive registry result must be present (check bookmark ID in metadata).
	aliveFound := false
	for _, o := range merged {
		if id, ok := o.Metadata["registry_bookmark_id"].(string); ok && id == "fed-alive-01" {
			aliveFound = true
		}
	}
	assert.True(t, aliveFound, "alive registry result must be included")

	// Check that the dead registry error is recorded in sources.
	var foundErr bool
	for _, src := range sources {
		if src.RegistryURL == "http://127.0.0.1:1" && src.Err != nil {
			foundErr = true
		}
	}
	assert.True(t, foundErr, "dead registry must record an error in source results")
}

// TestUS0019_FederatedSearchNoRegistries verifies that an empty registry list
// returns nil slices without error.
func TestUS0019_FederatedSearchNoRegistries(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Cfg.Registries = nil

	ctx := context.Background()
	merged, sources, err := env.svc.SearchRemoteBookmarks(ctx, "anything", time.Second)
	require.NoError(t, err)
	assert.Nil(t, merged)
	assert.Nil(t, sources)
}

// TestUS0019_FederatedSearchDeduplicatesSameID verifies that the same ID
// returned by two registries is kept as two entries (different registry source).
func TestUS0019_FederatedSearchDeduplicatesSameID(t *testing.T) {
	now := nowTrunc()
	bm := registry.RemoteBookmark{
		ID: "shared-id", Title: "common bookmark", URL: "https://common.example/1", CreatedAt: now,
	}

	srv1 := mockBookmarkServer(t, []registry.RemoteBookmark{bm})
	srv2 := mockBookmarkServer(t, []registry.RemoteBookmark{bm})

	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Cfg.Registries = []config.RegistryConfig{
		{Name: "r1", URL: srv1.URL},
		{Name: "r2", URL: srv2.URL},
	}

	ctx := context.Background()
	merged, _, err := env.svc.SearchRemoteBookmarks(ctx, "common", 5*time.Second)
	require.NoError(t, err)
	// MergeResults keys on registry_url + id, so same ID from two different
	// registries produces two distinct KnowledgeObjects.
	assert.Len(t, merged, 2, "same ID from different registries must produce two distinct results")
}
