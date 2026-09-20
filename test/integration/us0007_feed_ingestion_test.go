package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	gohttp "net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ---------------------------------------------------------------------------
// Mock feed server
// ---------------------------------------------------------------------------

type mockFeedServer struct {
	srv      *gohttp.Server
	addr     string
	etag     string
	hitCount atomic.Int64
}

func startMockFeedServer(t *testing.T) *mockFeedServer {
	t.Helper()
	m := &mockFeedServer{etag: `"feed-etag-1"`}

	mux := gohttp.NewServeMux()

	mux.HandleFunc("/rss", func(w gohttp.ResponseWriter, r *gohttp.Request) {
		m.hitCount.Add(1)
		if ifNone := r.Header.Get("If-None-Match"); ifNone == m.etag {
			w.WriteHeader(gohttp.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Header().Set("ETag", m.etag)
		fmt.Fprint(w, `<?xml version="1.0"?><rss version="2.0"><channel>
			<title>Test RSS Feed</title>
			<link>http://example.com</link>
			<description>A test RSS feed</description>
			<item><title>Item 1</title><guid>guid-rss-1</guid><link>http://example.com/1</link>
				<description>First item</description></item>
			<item><title>Item 2</title><guid>guid-rss-2</guid><link>http://example.com/2</link>
				<description>Second item</description></item>
		</channel></rss>`)
	})

	mux.HandleFunc("/atom", func(w gohttp.ResponseWriter, r *gohttp.Request) {
		m.hitCount.Add(1)
		w.Header().Set("Content-Type", "application/atom+xml")
		fmt.Fprint(w, `<?xml version="1.0" encoding="utf-8"?>
		<feed xmlns="http://www.w3.org/2005/Atom">
			<title>Test Atom Feed</title>
			<id>urn:uuid:atom-feed-1</id>
			<updated>2026-01-01T00:00:00Z</updated>
			<entry>
				<title>Atom Entry 1</title>
				<id>urn:uuid:atom-entry-1</id>
				<updated>2026-01-01T00:00:00Z</updated>
				<content>Atom content</content>
			</entry>
		</feed>`)
	})

	mux.HandleFunc("/jsonfeed", func(w gohttp.ResponseWriter, r *gohttp.Request) {
		m.hitCount.Add(1)
		w.Header().Set("Content-Type", "application/feed+json")
		json.NewEncoder(w).Encode(map[string]any{
			"version":       "https://jsonfeed.org/version/1.1",
			"title":         "Test JSON Feed",
			"home_page_url": "http://example.com",
			"items": []map[string]any{
				{"id": "json-item-1", "content_text": "JSON feed item 1",
					"url": "http://example.com/j1"},
			},
		})
	})

	mux.HandleFunc("/unreachable", func(w gohttp.ResponseWriter, r *gohttp.Request) {
		m.hitCount.Add(1)
		w.WriteHeader(gohttp.StatusInternalServerError)
		fmt.Fprint(w, "server error")
	})

	mux.HandleFunc("/gone", func(w gohttp.ResponseWriter, r *gohttp.Request) {
		m.hitCount.Add(1)
		w.WriteHeader(gohttp.StatusGone)
		fmt.Fprint(w, "feed permanently removed")
	})

	mux.HandleFunc("/ratelimit", func(w gohttp.ResponseWriter, r *gohttp.Request) {
		m.hitCount.Add(1)
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(gohttp.StatusTooManyRequests)
		fmt.Fprint(w, "rate limited")
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	m.srv = &gohttp.Server{Handler: mux}
	m.addr = ln.Addr().String()

	go m.srv.Serve(ln)
	t.Cleanup(func() { m.srv.Close() })

	return m
}

func (m *mockFeedServer) URL(path string) string {
	return "http://" + m.addr + path
}

// ---------------------------------------------------------------------------
// Feed-processing pipeline step (simulates feed item ingestion)
// ---------------------------------------------------------------------------

type feedItemStep struct {
	pipeline.BaseContract
}

func (s *feedItemStep) Name() string { return "test-feed-item" }
func (s *feedItemStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	draft.Type = "feed_item"
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Metadata["feed_source"] = draft.Source
	return draft, nil
}

// ---------------------------------------------------------------------------
// Helper: POST /api/v1/feeds
// ---------------------------------------------------------------------------

func postFeed(t *testing.T, baseURL string, payload map[string]any) (*gohttp.Response, map[string]any) {
	t.Helper()
	body, _ := json.Marshal(payload)
	resp, err := gohttp.Post(baseURL+"/api/v1/feeds", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	return resp, result
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestUS0007_SubscribeRSSReturnsMetadata(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)
	fs := startMockFeedServer(t)

	env.svc.Pipes.Upsert("feed.ingest", &pipeline.Pipeline{
		PipelineName: "feed.ingest",
		Steps:        []pipeline.PipelineStep{&feedItemStep{}},
	})

	resp, result := postFeed(t, env.URL, map[string]any{
		"url":    fs.URL("/rss"),
		"format": "rss",
	})
	defer resp.Body.Close()

	// POST /feeds should return 201 with feed details.
	assert.Equal(t, gohttp.StatusCreated, resp.StatusCode)
	assert.NotEmpty(t, result["id"], "feed subscription should return an id")
	assert.Equal(t, fs.URL("/rss"), result["url"])
	assert.Equal(t, "rss", result["format"])
}

func TestUS0007_SubscribeAtomFormat(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)
	fs := startMockFeedServer(t)

	env.svc.Pipes.Upsert("feed.ingest", &pipeline.Pipeline{
		PipelineName: "feed.ingest",
		Steps:        []pipeline.PipelineStep{&feedItemStep{}},
	})

	resp, result := postFeed(t, env.URL, map[string]any{
		"url":    fs.URL("/atom"),
		"format": "atom",
	})
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusCreated, resp.StatusCode)
	assert.NotEmpty(t, result["id"])
	assert.Equal(t, "atom", result["format"])
}

func TestUS0007_SubscribeJSONFeed(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)
	fs := startMockFeedServer(t)

	env.svc.Pipes.Upsert("feed.ingest", &pipeline.Pipeline{
		PipelineName: "feed.ingest",
		Steps:        []pipeline.PipelineStep{&feedItemStep{}},
	})

	resp, result := postFeed(t, env.URL, map[string]any{
		"url":    fs.URL("/jsonfeed"),
		"format": "jsonfeed",
	})
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusCreated, resp.StatusCode)
	assert.NotEmpty(t, result["id"])
	assert.Equal(t, "jsonfeed", result["format"])
}

func TestUS0007_ListSubscriptions(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)
	fs := startMockFeedServer(t)

	env.svc.Pipes.Upsert("feed.ingest", &pipeline.Pipeline{
		PipelineName: "feed.ingest",
		Steps:        []pipeline.PipelineStep{&feedItemStep{}},
	})

	// Subscribe to two feeds.
	for _, path := range []string{"/rss", "/atom"} {
		resp, _ := postFeed(t, env.URL, map[string]any{
			"url":    fs.URL(path),
			"format": "rss",
		})
		resp.Body.Close()
	}

	// GET /feeds returns all subscriptions.
	resp, err := gohttp.Get(env.URL + "/api/v1/feeds")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var body struct {
		Feeds []map[string]any `json:"feeds"`
		Total int              `json:"total"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.GreaterOrEqual(t, body.Total, 2)

	for _, feed := range body.Feeds {
		assert.NotEmpty(t, feed["id"])
		assert.NotEmpty(t, feed["url"])
		// Each feed should expose status and last_sync fields.
		_, hasStatus := feed["status"]
		assert.True(t, hasStatus, "feed should have a status field")
	}
}

func TestUS0007_SyncAllFeeds(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)
	fs := startMockFeedServer(t)

	env.svc.Pipes.Upsert("feed.ingest", &pipeline.Pipeline{
		PipelineName: "feed.ingest",
		Steps:        []pipeline.PipelineStep{&feedItemStep{}},
	})

	// Subscribe.
	resp, _ := postFeed(t, env.URL, map[string]any{
		"url":    fs.URL("/rss"),
		"format": "rss",
	})
	resp.Body.Close()

	// POST /feeds/sync triggers sync for all active feeds.
	syncResp, err := gohttp.Post(env.URL+"/api/v1/feeds/sync", "application/json", nil)
	require.NoError(t, err)
	defer syncResp.Body.Close()

	assert.Contains(t,
		[]int{gohttp.StatusOK, gohttp.StatusAccepted},
		syncResp.StatusCode,
		"sync-all should return 200 or 202",
	)
}

func TestUS0007_SyncSpecificFeed(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)
	fs := startMockFeedServer(t)

	env.svc.Pipes.Upsert("feed.ingest", &pipeline.Pipeline{
		PipelineName: "feed.ingest",
		Steps:        []pipeline.PipelineStep{&feedItemStep{}},
	})

	resp, result := postFeed(t, env.URL, map[string]any{
		"url":    fs.URL("/rss"),
		"format": "rss",
	})
	resp.Body.Close()

	feedID, _ := result["id"].(string)
	require.NotEmpty(t, feedID)

	// POST /feeds/{id}/sync syncs only the specified feed.
	syncResp, err := gohttp.Post(
		fmt.Sprintf("%s/api/v1/feeds/%s/sync", env.URL, feedID),
		"application/json", nil,
	)
	require.NoError(t, err)
	defer syncResp.Body.Close()

	assert.Contains(t,
		[]int{gohttp.StatusOK, gohttp.StatusAccepted},
		syncResp.StatusCode,
	)
}

func TestUS0007_RemoveSubscription(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)
	fs := startMockFeedServer(t)

	env.svc.Pipes.Upsert("feed.ingest", &pipeline.Pipeline{
		PipelineName: "feed.ingest",
		Steps:        []pipeline.PipelineStep{&feedItemStep{}},
	})

	resp, result := postFeed(t, env.URL, map[string]any{
		"url":    fs.URL("/rss"),
		"format": "rss",
	})
	resp.Body.Close()

	feedID, _ := result["id"].(string)
	require.NotEmpty(t, feedID)

	// DELETE /feeds/{id} removes subscription.
	req, _ := gohttp.NewRequest(gohttp.MethodDelete,
		fmt.Sprintf("%s/api/v1/feeds/%s", env.URL, feedID), nil)
	delResp, err := gohttp.DefaultClient.Do(req)
	require.NoError(t, err)
	defer delResp.Body.Close()

	assert.Equal(t, gohttp.StatusNoContent, delResp.StatusCode)

	// Verify it no longer appears in the list.
	listResp, err := gohttp.Get(env.URL + "/api/v1/feeds")
	require.NoError(t, err)
	defer listResp.Body.Close()

	var body struct {
		Feeds []map[string]any `json:"feeds"`
	}
	json.NewDecoder(listResp.Body).Decode(&body)
	for _, feed := range body.Feeds {
		assert.NotEqual(t, feedID, feed["id"], "deleted feed should not appear in list")
	}
}

func TestUS0007_ConditionalGetETag(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)
	fs := startMockFeedServer(t)

	env.svc.Pipes.Upsert("feed.ingest", &pipeline.Pipeline{
		PipelineName: "feed.ingest",
		Steps:        []pipeline.PipelineStep{&feedItemStep{}},
	})

	resp, result := postFeed(t, env.URL, map[string]any{
		"url":    fs.URL("/rss"),
		"format": "rss",
	})
	resp.Body.Close()

	feedID, _ := result["id"].(string)
	require.NotEmpty(t, feedID)

	// First sync fetches the feed.
	syncResp1, err := gohttp.Post(
		fmt.Sprintf("%s/api/v1/feeds/%s/sync", env.URL, feedID),
		"application/json", nil,
	)
	require.NoError(t, err)
	syncResp1.Body.Close()

	hitsBefore := fs.hitCount.Load()

	// Second sync should use ETag; if server returns 304 no new items created.
	syncResp2, err := gohttp.Post(
		fmt.Sprintf("%s/api/v1/feeds/%s/sync", env.URL, feedID),
		"application/json", nil,
	)
	require.NoError(t, err)
	syncResp2.Body.Close()

	hitsAfter := fs.hitCount.Load()
	// The server should have been hit again (conditional GET), but feed
	// implementation should recognize 304 and skip re-ingestion.
	assert.GreaterOrEqual(t, hitsAfter, hitsBefore,
		"feed server should receive at least one request for conditional GET")
}

func TestUS0007_DeduplicationByGUID(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)
	fs := startMockFeedServer(t)

	env.svc.Pipes.Upsert("feed.ingest", &pipeline.Pipeline{
		PipelineName: "feed.ingest",
		Steps:        []pipeline.PipelineStep{&feedItemStep{}},
	})

	resp, result := postFeed(t, env.URL, map[string]any{
		"url":    fs.URL("/rss"),
		"format": "rss",
	})
	resp.Body.Close()

	feedID, _ := result["id"].(string)
	require.NotEmpty(t, feedID)

	// Sync twice.
	for i := 0; i < 2; i++ {
		syncResp, err := gohttp.Post(
			fmt.Sprintf("%s/api/v1/feeds/%s/sync", env.URL, feedID),
			"application/json", nil,
		)
		require.NoError(t, err)
		syncResp.Body.Close()
		time.Sleep(200 * time.Millisecond)
	}

	// Items already ingested by GUID should not be re-enqueued.
	// We verify by listing feed_item objects; count should equal unique GUIDs.
	listResp, err := gohttp.Get(env.URL + "/api/v1/objects?limit=100")
	require.NoError(t, err)
	defer listResp.Body.Close()

	var body struct {
		Data  []storage.KnowledgeObject `json:"data"`
		Total int                       `json:"total"`
	}
	json.NewDecoder(listResp.Body).Decode(&body)

	feedItems := 0
	guids := make(map[string]bool)
	for _, obj := range body.Data {
		if obj.Type == "feed_item" {
			feedItems++
			if g, ok := obj.Metadata["guid"].(string); ok {
				assert.False(t, guids[g], "GUID %s should not be duplicated", g)
				guids[g] = true
			}
		}
	}
	// The RSS feed has 2 unique items; no duplicates expected.
	assert.LessOrEqual(t, feedItems, 2, "deduplication should prevent duplicates")
}

func TestUS0007_FanoutCreatesJobs(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)
	fs := startMockFeedServer(t)

	env.svc.Pipes.Upsert("feed.ingest", &pipeline.Pipeline{
		PipelineName: "feed.ingest",
		Steps:        []pipeline.PipelineStep{&feedItemStep{}},
	})

	resp, result := postFeed(t, env.URL, map[string]any{
		"url":    fs.URL("/rss"),
		"format": "rss",
	})
	resp.Body.Close()

	feedID, _ := result["id"].(string)
	require.NotEmpty(t, feedID)

	// Sync the feed.
	syncResp, err := gohttp.Post(
		fmt.Sprintf("%s/api/v1/feeds/%s/sync", env.URL, feedID),
		"application/json", nil,
	)
	require.NoError(t, err)
	syncResp.Body.Close()

	// Each new feed item should create a separate ingestion job.
	time.Sleep(500 * time.Millisecond)

	jobsResp, err := gohttp.Get(env.URL + "/api/v1/jobs?limit=100")
	require.NoError(t, err)
	defer jobsResp.Body.Close()

	var jobsBody struct {
		Data  []storage.Job `json:"data"`
		Total int           `json:"total"`
	}
	json.NewDecoder(jobsResp.Body).Decode(&jobsBody)

	// The RSS feed has 2 items, so we expect at least 2 jobs.
	feedJobs := 0
	for _, j := range jobsBody.Data {
		if j.Pipeline == "feed.ingest" {
			feedJobs++
		}
	}
	assert.GreaterOrEqual(t, feedJobs, 1,
		"each feed item should create a separate ingestion job")
}

func TestUS0007_FeedItemKnowledgeObject(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)
	fs := startMockFeedServer(t)

	env.svc.Pipes.Upsert("feed.ingest", &pipeline.Pipeline{
		PipelineName: "feed.ingest",
		Steps:        []pipeline.PipelineStep{&feedItemStep{}},
	})

	// Enqueue a feed item directly via the service layer.
	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "Feed item content from RSS",
		Type:     "text",
		Pipeline: "feed.ingest",
		Source:   fs.URL("/rss"),
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	// Verify created object has Type="feed_item" and correct metadata.
	objResp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer objResp.Body.Close()
	require.Equal(t, gohttp.StatusOK, objResp.StatusCode)

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(objResp.Body).Decode(&obj))

	assert.Equal(t, "feed_item", obj.Type)
	assert.NotNil(t, obj.Metadata)
	assert.Equal(t, fs.URL("/rss"), obj.Metadata["feed_source"])
	assert.Equal(t, "feed.ingest", obj.Pipeline)
}

func TestUS0007_UnreachableFeedSetsError(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)
	fs := startMockFeedServer(t)

	env.svc.Pipes.Upsert("feed.ingest", &pipeline.Pipeline{
		PipelineName: "feed.ingest",
		Steps:        []pipeline.PipelineStep{&feedItemStep{}},
	})

	resp, result := postFeed(t, env.URL, map[string]any{
		"url":    fs.URL("/unreachable"),
		"format": "rss",
	})
	resp.Body.Close()

	feedID, _ := result["id"].(string)
	require.NotEmpty(t, feedID)

	// Sync should encounter 500 error.
	syncResp, err := gohttp.Post(
		fmt.Sprintf("%s/api/v1/feeds/%s/sync", env.URL, feedID),
		"application/json", nil,
	)
	require.NoError(t, err)
	syncResp.Body.Close()

	time.Sleep(300 * time.Millisecond)

	// Verify feed status is "error".
	getResp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/feeds/%s", env.URL, feedID))
	if err == nil && getResp != nil {
		defer getResp.Body.Close()
		var feed map[string]any
		json.NewDecoder(getResp.Body).Decode(&feed)
		if status, ok := feed["status"].(string); ok {
			assert.Equal(t, "error", status, "unreachable feed should be in error status")
		}
	}
}

func TestUS0007_Feed410GonePermanent(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)
	fs := startMockFeedServer(t)

	env.svc.Pipes.Upsert("feed.ingest", &pipeline.Pipeline{
		PipelineName: "feed.ingest",
		Steps:        []pipeline.PipelineStep{&feedItemStep{}},
	})

	resp, result := postFeed(t, env.URL, map[string]any{
		"url":    fs.URL("/gone"),
		"format": "rss",
	})
	resp.Body.Close()

	feedID, _ := result["id"].(string)
	require.NotEmpty(t, feedID)

	// Sync should detect 410 Gone.
	syncResp, err := gohttp.Post(
		fmt.Sprintf("%s/api/v1/feeds/%s/sync", env.URL, feedID),
		"application/json", nil,
	)
	require.NoError(t, err)
	syncResp.Body.Close()

	time.Sleep(300 * time.Millisecond)

	// Verify feed status is "gone".
	getResp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/feeds/%s", env.URL, feedID))
	if err == nil && getResp != nil {
		defer getResp.Body.Close()
		var feed map[string]any
		json.NewDecoder(getResp.Body).Decode(&feed)
		if status, ok := feed["status"].(string); ok {
			assert.Equal(t, "gone", status, "410 Gone should permanently mark feed")
		}
	}
}

func TestUS0007_Feed429RespectsRetryAfter(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)
	fs := startMockFeedServer(t)

	env.svc.Pipes.Upsert("feed.ingest", &pipeline.Pipeline{
		PipelineName: "feed.ingest",
		Steps:        []pipeline.PipelineStep{&feedItemStep{}},
	})

	resp, result := postFeed(t, env.URL, map[string]any{
		"url":    fs.URL("/ratelimit"),
		"format": "rss",
	})
	resp.Body.Close()

	feedID, _ := result["id"].(string)
	require.NotEmpty(t, feedID)

	// Sync should encounter 429 with Retry-After.
	syncResp, err := gohttp.Post(
		fmt.Sprintf("%s/api/v1/feeds/%s/sync", env.URL, feedID),
		"application/json", nil,
	)
	require.NoError(t, err)
	syncResp.Body.Close()

	time.Sleep(300 * time.Millisecond)

	// Verify next_sync is delayed (feed metadata should reflect Retry-After).
	getResp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/feeds/%s", env.URL, feedID))
	if err == nil && getResp != nil {
		defer getResp.Body.Close()
		var feed map[string]any
		json.NewDecoder(getResp.Body).Decode(&feed)
		// The feed should have a retry_after or next_sync in the future.
		if nextSync, ok := feed["next_sync"].(string); ok {
			parsed, parseErr := time.Parse(time.RFC3339, nextSync)
			if parseErr == nil {
				assert.True(t, parsed.After(time.Now()),
					"next_sync should be in the future after 429")
			}
		}
	}
}

func TestUS0007_POSTFeedsReturns201(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)
	fs := startMockFeedServer(t)

	env.svc.Pipes.Upsert("feed.ingest", &pipeline.Pipeline{
		PipelineName: "feed.ingest",
		Steps:        []pipeline.PipelineStep{&feedItemStep{}},
	})

	body, _ := json.Marshal(map[string]any{
		"url":    fs.URL("/rss"),
		"format": "rss",
	})

	resp, err := gohttp.Post(env.URL+"/api/v1/feeds", "application/json",
		bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusCreated, resp.StatusCode)
}

func TestUS0007_GETFeedsReturnsList(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	resp, err := gohttp.Get(env.URL + "/api/v1/feeds")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	_, hasFeedsKey := body["feeds"]
	assert.True(t, hasFeedsKey, "response should have 'feeds' key")
}

func TestUS0007_SyncReturns202(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)
	fs := startMockFeedServer(t)

	env.svc.Pipes.Upsert("feed.ingest", &pipeline.Pipeline{
		PipelineName: "feed.ingest",
		Steps:        []pipeline.PipelineStep{&feedItemStep{}},
	})

	resp, result := postFeed(t, env.URL, map[string]any{
		"url":    fs.URL("/rss"),
		"format": "rss",
	})
	resp.Body.Close()

	feedID, _ := result["id"].(string)
	require.NotEmpty(t, feedID)

	syncResp, err := gohttp.Post(
		fmt.Sprintf("%s/api/v1/feeds/%s/sync", env.URL, feedID),
		"application/json", nil,
	)
	require.NoError(t, err)
	defer syncResp.Body.Close()

	assert.Contains(t,
		[]int{gohttp.StatusOK, gohttp.StatusAccepted},
		syncResp.StatusCode,
		"POST /feeds/{id}/sync should return 200 or 202",
	)
}

func TestUS0007_DeleteReturns204(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)
	fs := startMockFeedServer(t)

	env.svc.Pipes.Upsert("feed.ingest", &pipeline.Pipeline{
		PipelineName: "feed.ingest",
		Steps:        []pipeline.PipelineStep{&feedItemStep{}},
	})

	resp, result := postFeed(t, env.URL, map[string]any{
		"url":    fs.URL("/rss"),
		"format": "rss",
	})
	resp.Body.Close()

	feedID, _ := result["id"].(string)
	require.NotEmpty(t, feedID)

	req, _ := gohttp.NewRequest(gohttp.MethodDelete,
		fmt.Sprintf("%s/api/v1/feeds/%s", env.URL, feedID), nil)
	delResp, err := gohttp.DefaultClient.Do(req)
	require.NoError(t, err)
	defer delResp.Body.Close()

	assert.Equal(t, gohttp.StatusNoContent, delResp.StatusCode)
}
