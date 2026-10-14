package http_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// Entity pages, the topic index and stats: GET /api/v1/pages,
// /pages/{slug}, POST /pages/{slug}/refresh, GET /api/v1/topic-index,
// /topic-index/{topic}, POST /topic-index and GET /api/v1/stats. The
// suite runs on SQLite here and on Postgres under -tags integration.

func TestPagesIndexStats_SQLite(t *testing.T) {
	RunPagesIndexStatsSuite(t, storageutil.NewTestDriver)
}

// RunPagesIndexStatsSuite runs every case against a fresh instance over
// a driver from newDriver.
func RunPagesIndexStatsSuite(t *testing.T, newDriver func(*testing.T) storage.StorageDriver) {
	fresh := func(t *testing.T) *dpkmstest.Server {
		t.Helper()
		driver := newDriver(t)
		srv := dpkmstest.Start(t, driver, dpkmstest.WithStaticTokens())
		seedPagesIndexStatsCorpus(t, driver)
		return srv
	}
	t.Run("Stats", func(t *testing.T) {
		driver := newDriver(t)
		srv := dpkmstest.Start(t, driver, dpkmstest.WithStaticTokens())
		testStats(t, srv, seedPagesIndexStatsCorpus(t, driver))
	})
	t.Run("StatsRejectsBadParams", func(t *testing.T) { testStatsRejectsBadParams(t, fresh(t)) })
	t.Run("PagesRead", func(t *testing.T) { testPagesRead(t, fresh(t)) })
	t.Run("PagesRefresh", func(t *testing.T) { testPagesRefresh(t, fresh(t)) })
	t.Run("TopicIndex", func(t *testing.T) { testTopicIndex(t, fresh(t)) })
	t.Run("Roles", func(t *testing.T) { testPagesIndexStatsRoles(t, fresh(t)) })
}

// seedPagesIndexStatsCorpus writes:
//
//	objects  s-1 note active (topics go, teams; reminder), s-2 article
//	         inbox (topic go), s-3 note discarded (no topics), and the
//	         entity pages p-alice (person.alice from s-1, s-2) and p-go
//	         (lang.go from s-1)
//	jobs     2 pending, 1 running, 3 completed, 1 failed
//	entities ui.cart, ui.checkout, zz.late
//	feeds    one active, one paused
//	queue    unseen: work→s-1, work→s-2, home→s-1; work→s-3 dismissed
//
// It reports whether the driver has a resurfacing queue (Postgres has
// none yet), in which case the queue stays empty.
func seedPagesIndexStatsCorpus(t *testing.T, driver storage.StorageDriver) (resurfacing bool) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	topics := func(ts ...string) map[string]any {
		list := make([]any, 0, len(ts))
		for _, s := range ts {
			list = append(list, s)
		}
		return map[string]any{"enrichment.structured_metadata": map[string]any{"topics": list}}
	}
	page := func(slug string, sources ...string) map[string]any {
		ids := make([]any, 0, len(sources))
		for _, s := range sources {
			ids = append(ids, s)
		}
		return map[string]any{"entity_page": map[string]any{
			"entity_slug": slug, "source_ids": ids, "revision_count": len(sources), "revision_log": []any{},
		}}
	}
	for _, o := range []*storage.KnowledgeObject{
		{ID: "s-1", Type: "note", Status: "active", RawContent: "Alice leads the Go team", Summaries: []string{"Alice leads Go"}, Metadata: topics("go", "teams")},
		{ID: "s-2", Type: "article", Status: "inbox", RawContent: "Go generics in practice", Summaries: []string{"Go generics"}, Metadata: topics("go")},
		{ID: "s-3", Type: "note", Status: "discarded", RawContent: "unrelated"},
		{ID: "p-alice", Type: "entity_page", Status: "active", RawContent: "stale alice", Source: "entity_page:person.alice", Metadata: page("person.alice", "s-1", "s-2")},
		{ID: "p-go", Type: "entity_page", Status: "active", RawContent: "stale go", Source: "entity_page:lang.go", Metadata: page("lang.go", "s-1")},
	} {
		o.CreatedAt, o.UpdatedAt = now, now
		require.NoError(t, driver.Objects().Create(ctx, o), o.ID)
	}
	require.NoError(t, driver.Objects().SetReminder(ctx, "s-1", now.Add(24*time.Hour)))

	jobs := map[storage.JobStatus]int{storage.JobPending: 2, storage.JobRunning: 1, storage.JobCompleted: 3, storage.JobFailed: 1}
	n := 0
	for status, count := range jobs {
		for range count {
			n++
			require.NoError(t, driver.Jobs().Create(ctx, &storage.Job{
				ID: "job-" + string(status) + "-" + string(rune('a'+n)), Type: "analyze", Status: status,
				Pipeline: "default", MaxRetries: 3, CreatedAt: now, UpdatedAt: now,
			}))
		}
	}

	for _, e := range []*storage.Entity{
		{Slug: "ui.cart", Title: "Cart", Namespace: "ui"},
		{Slug: "ui.checkout", Title: "Checkout", Namespace: "ui"},
		{Slug: "zz.late", Title: "Late", Namespace: "zz"},
	} {
		e.CreatedAt, e.UpdatedAt = now, now
		require.NoError(t, driver.Entities().Upsert(ctx, e))
	}

	for _, f := range []*storage.Feed{
		{ID: "feed-a", URL: "https://a.example/rss", Title: "A", Status: "active"},
		{ID: "feed-b", URL: "https://b.example/rss", Title: "B", Status: "paused"},
	} {
		f.CreatedAt, f.UpdatedAt = now, now
		require.NoError(t, driver.Feeds().Create(ctx, f))
	}

	for _, e := range []*storage.ResurfacingEntry{
		{ID: "rq-1", ObjectID: "s-1", ProfileID: "work", Score: 0.9},
		{ID: "rq-2", ObjectID: "s-2", ProfileID: "work", Score: 0.8},
		{ID: "rq-3", ObjectID: "s-1", ProfileID: "home", Score: 0.7},
		{ID: "rq-4", ObjectID: "s-3", ProfileID: "work", Score: 0.6},
	} {
		e.CreatedAt = now
		err := driver.Resurfacing().Upsert(ctx, e)
		if errors.Is(err, storage.ErrNotImplemented) {
			return false
		}
		require.NoError(t, err)
	}
	require.NoError(t, driver.Resurfacing().Dismiss(ctx, "rq-4", now))
	return true
}

func getJSON(t *testing.T, srv *dpkmstest.Server, method, path, role string, out any) int {
	t.Helper()
	resp := srv.Request(t, method, path, role, nil)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	if out != nil && resp.StatusCode/100 == 2 {
		require.NoError(t, json.Unmarshal(body, out), string(body))
	}
	return resp.StatusCode
}

type statsBody struct {
	KnowledgeObjects int            `json:"knowledge_objects"`
	ObjectsByType    map[string]int `json:"objects_by_type"`
	Jobs             map[string]int `json:"jobs"`
	Entities         int            `json:"entities"`
	Feeds            int            `json:"feeds"`
	ActiveFeeds      int            `json:"feeds_active"`
	Reminders        int            `json:"reminders_pending"`
	Resurfacing      *int           `json:"resurfacing_candidates"`
}

func intp(n int) *int { return &n }

// Every object counts, whatever its status, by type; jobs by status;
// resurfacing counts the unseen entries of the requested profile, or of
// every profile without one; it is null on a backend without a queue.
func testStats(t *testing.T, srv *dpkmstest.Server, resurfacing bool) {
	count := func(n int) *int {
		if !resurfacing {
			return nil
		}
		return intp(n)
	}
	var got statsBody
	require.Equal(t, http.StatusOK, getJSON(t, srv, http.MethodGet, "/api/v1/stats", dpkmstest.RoleReader, &got))
	want := statsBody{
		KnowledgeObjects: 5,
		ObjectsByType:    map[string]int{"note": 2, "article": 1, "entity_page": 2},
		Jobs:             map[string]int{"total": 7, "pending": 2, "running": 1, "completed": 3, "failed": 1},
		Entities:         3,
		Feeds:            2,
		ActiveFeeds:      1,
		Reminders:        1,
		Resurfacing:      count(3),
	}
	assert.Equal(t, want, got)

	var work statsBody
	require.Equal(t, http.StatusOK, getJSON(t, srv, http.MethodGet, "/api/v1/stats?profile=work", dpkmstest.RoleReader, &work))
	assert.Equal(t, count(2), work.Resurfacing, "work profile: two unseen, one dismissed")
	var none statsBody
	require.Equal(t, http.StatusOK, getJSON(t, srv, http.MethodGet, "/api/v1/stats?profile=nobody", dpkmstest.RoleReader, &none))
	assert.Equal(t, count(0), none.Resurfacing)
	assert.Equal(t, 5, none.KnowledgeObjects, "the profile scopes resurfacing only")
}

func testStatsRejectsBadParams(t *testing.T, srv *dpkmstest.Server) {
	for _, q := range []string{"?limit=5", "?profile=", "?profile=a&profile=b"} {
		resp := srv.Request(t, http.MethodGet, "/api/v1/stats"+q, dpkmstest.RoleReader, nil)
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "%s: %s", q, body)
		assert.Contains(t, string(body), "INVALID_PARAM", q)
	}
}

type pageBody struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	RawContent string         `json:"raw_content"`
	Metadata   map[string]any `json:"metadata"`
}

func pageSlug(p pageBody) string {
	m, _ := p.Metadata["entity_page"].(map[string]any)
	s, _ := m["entity_slug"].(string)
	return s
}

func testPagesRead(t *testing.T, srv *dpkmstest.Server) {
	var list struct {
		Data []pageBody `json:"data"`
	}
	require.Equal(t, http.StatusOK, getJSON(t, srv, http.MethodGet, "/api/v1/pages", dpkmstest.RoleReader, &list))
	slugs := []string{}
	for _, p := range list.Data {
		assert.Equal(t, "entity_page", p.Type)
		slugs = append(slugs, pageSlug(p))
	}
	assert.ElementsMatch(t, []string{"person.alice", "lang.go"}, slugs)

	require.Equal(t, http.StatusOK, getJSON(t, srv, http.MethodGet, "/api/v1/pages?limit=1", dpkmstest.RoleReader, &list))
	assert.Len(t, list.Data, 1)

	for _, q := range []string{"?limit=0", "?limit=x", "?offset=1", "?limit="} {
		resp := srv.Request(t, http.MethodGet, "/api/v1/pages"+q, dpkmstest.RoleReader, nil)
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "%s: %s", q, body)
		assert.Contains(t, string(body), "INVALID_PARAM", q)
	}

	var one pageBody
	require.Equal(t, http.StatusOK, getJSON(t, srv, http.MethodGet, "/api/v1/pages/person.alice", dpkmstest.RoleReader, &one))
	assert.Equal(t, "p-alice", one.ID)
	assert.Equal(t, "stale alice", one.RawContent)

	resp := srv.Request(t, http.MethodGet, "/api/v1/pages/no.such", dpkmstest.RoleReader, nil)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode, string(body))
	assert.Contains(t, string(body), "NOT_FOUND")
}

// Refresh recompiles the page from its sources and persists it.
func testPagesRefresh(t *testing.T, srv *dpkmstest.Server) {
	var got pageBody
	require.Equal(t, http.StatusOK, getJSON(t, srv, http.MethodPost, "/api/v1/pages/person.alice/refresh", dpkmstest.RoleWriter, &got))
	assert.Equal(t, "p-alice", got.ID)
	assert.Contains(t, got.RawContent, "# Entity: person.alice")
	assert.Contains(t, got.RawContent, "Compiled from 2 sources.")
	assert.Contains(t, got.RawContent, "Alice leads Go")

	var stored pageBody
	require.Equal(t, http.StatusOK, getJSON(t, srv, http.MethodGet, "/api/v1/pages/person.alice", dpkmstest.RoleReader, &stored))
	assert.Equal(t, got.RawContent, stored.RawContent, "refresh persisted")

	resp := srv.Request(t, http.MethodPost, "/api/v1/pages/no.such/refresh", dpkmstest.RoleWriter, nil)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode, string(body))
	assert.Contains(t, string(body), "NOT_FOUND")
}

type topicIndexBody struct {
	Categories []struct {
		Topic   string `json:"topic"`
		Entries []struct {
			ObjectID string `json:"object_id"`
			Summary  string `json:"summary"`
		} `json:"entries"`
	} `json:"categories"`
	ObjectIDs []string `json:"object_ids"`
}

// Before a rebuild the index is 404; the rebuild groups objects by
// topic, and a topic lookup matches a case-insensitive prefix.
func testTopicIndex(t *testing.T, srv *dpkmstest.Server) {
	for _, p := range []string{"/api/v1/topic-index", "/api/v1/topic-index/go"} {
		resp := srv.Request(t, http.MethodGet, p, dpkmstest.RoleReader, nil)
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		assert.Equal(t, http.StatusNotFound, resp.StatusCode, "%s: %s", p, body)
		assert.Contains(t, string(body), "NOT_FOUND", p)
	}

	var rebuilt struct {
		ID string `json:"id"`
	}
	require.Equal(t, http.StatusOK, getJSON(t, srv, http.MethodPost, "/api/v1/topic-index", dpkmstest.RoleWriter, &rebuilt))
	assert.NotEmpty(t, rebuilt.ID)

	var idx topicIndexBody
	require.Equal(t, http.StatusOK, getJSON(t, srv, http.MethodGet, "/api/v1/topic-index", dpkmstest.RoleReader, &idx))
	topics := map[string][]string{}
	for _, c := range idx.Categories {
		for _, e := range c.Entries {
			topics[c.Topic] = append(topics[c.Topic], e.ObjectID)
		}
	}
	assert.ElementsMatch(t, []string{"s-1", "s-2"}, topics["go"])
	assert.Equal(t, []string{"s-1"}, topics["teams"])
	assert.ElementsMatch(t, []string{"s-1", "s-2"}, idx.ObjectIDs)

	var entries struct {
		Data []struct {
			ObjectID string `json:"object_id"`
			Summary  string `json:"summary"`
		} `json:"data"`
	}
	require.Equal(t, http.StatusOK, getJSON(t, srv, http.MethodGet, "/api/v1/topic-index/TEA", dpkmstest.RoleReader, &entries))
	require.Len(t, entries.Data, 1)
	assert.Equal(t, "s-1", entries.Data[0].ObjectID)
	assert.Equal(t, "Alice leads Go", entries.Data[0].Summary)

	require.Equal(t, http.StatusOK, getJSON(t, srv, http.MethodGet, "/api/v1/topic-index/rust", dpkmstest.RoleReader, &entries))
	assert.NotNil(t, entries.Data)
	assert.Empty(t, entries.Data)

	// A rebuild replaces the one index object; it never adds another.
	require.Equal(t, http.StatusOK, getJSON(t, srv, http.MethodPost, "/api/v1/topic-index", dpkmstest.RoleAdmin, &rebuilt))
	var st statsBody
	require.Equal(t, http.StatusOK, getJSON(t, srv, http.MethodGet, "/api/v1/stats", dpkmstest.RoleReader, &st))
	assert.Equal(t, 1, st.ObjectsByType["topic_index"])
}

// Reads need read:objects (every role); refresh and rebuild need
// write:objects (writer, admin). No token is 401.
func testPagesIndexStatsRoles(t *testing.T, srv *dpkmstest.Server) {
	reads := []string{"/api/v1/stats", "/api/v1/pages", "/api/v1/pages/lang.go"}
	for _, p := range reads {
		for _, role := range dpkmstest.Roles {
			assert.Equal(t, http.StatusOK, getJSON(t, srv, http.MethodGet, p, role, nil), "%s as %s", p, role)
		}
		assert.Equal(t, http.StatusUnauthorized, getJSON(t, srv, http.MethodGet, p, "none", nil), "%s without a token", p)
	}
	assert.Equal(t, http.StatusUnauthorized, getJSON(t, srv, http.MethodGet, "/api/v1/topic-index", "none", nil))

	writes := []string{"/api/v1/pages/lang.go/refresh", "/api/v1/topic-index"}
	for _, p := range writes {
		resp := srv.Request(t, http.MethodPost, p, dpkmstest.RoleReader, nil)
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		assert.Equal(t, http.StatusForbidden, resp.StatusCode, "%s as reader: %s", p, body)
		assert.True(t, strings.Contains(string(body), "INSUFFICIENT_SCOPE") && strings.Contains(string(body), "write:objects"),
			"%s as reader: %s", p, body)
		assert.Equal(t, http.StatusUnauthorized, getJSON(t, srv, http.MethodPost, p, "none", nil), "%s without a token", p)
		for _, role := range []string{dpkmstest.RoleWriter, dpkmstest.RoleAdmin} {
			assert.Equal(t, http.StatusOK, getJSON(t, srv, http.MethodPost, p, role, nil), "%s as %s", p, role)
		}
	}
	// Once built, the index reads for every role.
	for _, role := range dpkmstest.Roles {
		assert.Equal(t, http.StatusOK, getJSON(t, srv, http.MethodGet, "/api/v1/topic-index", role, nil), role)
		assert.Equal(t, http.StatusOK, getJSON(t, srv, http.MethodGet, "/api/v1/topic-index/go", role, nil), role)
	}
}
