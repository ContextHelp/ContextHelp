package integration

// US-0208: Temporal Watch and Change Detection
//
// Verifies: configure watch on mock URL, trigger mock content change,
// new version ingested and delta recorded.
// All external HTTP calls use httptest.NewServer — no real network.
// Gate: INTEGRATION=1 env var required.

import (
	"context"
	"encoding/json"
	"fmt"
	gohttp "net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ---------------------------------------------------------------------------
// Pipeline steps
// ---------------------------------------------------------------------------

// watchedURLFetchStep fetches a URL and records a content hash for change detection.
type watchedURLFetchStep struct {
	pipeline.BaseContract
	fetchURL string
}

func (s *watchedURLFetchStep) Name() string { return "test-watched-url-fetch" }
func (s *watchedURLFetchStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	resp, err := gohttp.Get(s.fetchURL)
	if err != nil {
		return draft, err
	}
	defer resp.Body.Close()

	var payload struct {
		Content     string `json:"content"`
		ContentHash string `json:"content_hash"`
		Version     int    `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return draft, err
	}

	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	draft.Type = "web.snapshot"
	draft.TextContent = payload.Content
	draft.Metadata["content_hash"] = payload.ContentHash
	draft.Metadata["version"] = payload.Version
	draft.Metadata["watch_url"] = s.fetchURL
	return draft, nil
}

// changeDeltaStep records the delta between the current and previous snapshot.
type changeDeltaStep struct {
	pipeline.BaseContract
	previousHash string
}

func (s *changeDeltaStep) Name() string { return "test-change-delta" }
func (s *changeDeltaStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	currentHash, _ := draft.Metadata["content_hash"].(string)
	changed := currentHash != s.previousHash
	draft.Metadata["changed"] = changed
	draft.Metadata["previous_hash"] = s.previousHash
	draft.Metadata["current_hash"] = currentHash
	return draft, nil
}

// ---------------------------------------------------------------------------
// US-0208 Tests
// ---------------------------------------------------------------------------

// TestUS0208_InitialSnapshotIngested verifies the first watch capture stores a
// snapshot with content hash.
func TestUS0208_InitialSnapshotIngested(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}

	const contentHash = "sha256:abc123"

	srv := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"content":      "Original page content about distributed systems.",
			"content_hash": contentHash,
			"version":      1,
		})
	}))
	defer srv.Close()

	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("web.watch", &pipeline.Pipeline{
		PipelineName: "web.watch",
		Steps: []pipeline.PipelineStep{
			&watchedURLFetchStep{fetchURL: srv.URL},
			&changeDeltaStep{previousHash: ""},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  srv.URL,
		Type:     "url",
		Pipeline: "web.watch",
		Source:   srv.URL,
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

	assert.Equal(t, "web.snapshot", obj.Type)
	assert.Equal(t, contentHash, obj.Metadata["content_hash"], "content hash must be stored")
	assert.EqualValues(t, 1, obj.Metadata["version"])
}

// TestUS0208_ChangeDetectedOnNewVersion verifies that when the content changes,
// the delta step records the change and stores the new version.
func TestUS0208_ChangeDetectedOnNewVersion(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}

	const v1Hash = "sha256:abc111"
	const v2Hash = "sha256:def222"

	// Two separate mock servers for v1 and v2 — avoids shared state issues.
	srvV1 := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"content":      "Version 1 content.",
			"content_hash": v1Hash,
			"version":      1,
		})
	}))
	defer srvV1.Close()

	srvV2 := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"content":      "Version 2 content — significantly changed.",
			"content_hash": v2Hash,
			"version":      2,
		})
	}))
	defer srvV2.Close()

	env := startTestEnv(t)
	defer env.stop(t)

	// First capture — baseline snapshot (no previous hash).
	env.svc.Pipes.Upsert("web.watch", &pipeline.Pipeline{
		PipelineName: "web.watch",
		Steps: []pipeline.PipelineStep{
			&watchedURLFetchStep{fetchURL: srvV1.URL},
			&changeDeltaStep{previousHash: ""},
		},
	})

	jobID1, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  srvV1.URL,
		Type:     "url",
		Pipeline: "web.watch",
		Source:   srvV1.URL,
	})
	require.NoError(t, err)
	job1 := waitForJob(t, env.URL, jobID1, storage.JobCompleted)
	require.NotEmpty(t, job1.ResultID)

	// Second capture — content has changed; previous hash = v1Hash.
	env.svc.Pipes.Upsert("web.watch.v2", &pipeline.Pipeline{
		PipelineName: "web.watch.v2",
		Steps: []pipeline.PipelineStep{
			&watchedURLFetchStep{fetchURL: srvV2.URL},
			&changeDeltaStep{previousHash: v1Hash},
		},
	})

	jobID2, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  srvV2.URL,
		Type:     "url",
		Pipeline: "web.watch.v2",
		Source:   srvV2.URL,
	})
	require.NoError(t, err)
	job2 := waitForJob(t, env.URL, jobID2, storage.JobCompleted)
	require.NotEmpty(t, job2.ResultID)

	// Retrieve second snapshot.
	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job2.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

	assert.Equal(t, "web.snapshot", obj.Type)
	assert.Equal(t, true, obj.Metadata["changed"], "change must be detected")
	assert.Equal(t, v1Hash, obj.Metadata["previous_hash"])
	assert.Equal(t, v2Hash, obj.Metadata["current_hash"])
	assert.EqualValues(t, 2, obj.Metadata["version"])
}

// TestUS0208_NoChangeWhenContentIdentical verifies that unchanged content is
// detected as not changed.
func TestUS0208_NoChangeWhenContentIdentical(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}

	const stableHash = "sha256:stable999"

	srv := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"content":      "Stable content that never changes.",
			"content_hash": stableHash,
			"version":      1,
		})
	}))
	defer srv.Close()

	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("web.watch.stable", &pipeline.Pipeline{
		PipelineName: "web.watch.stable",
		Steps: []pipeline.PipelineStep{
			&watchedURLFetchStep{fetchURL: srv.URL},
			// Previous hash matches current — no change expected.
			&changeDeltaStep{previousHash: stableHash},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  srv.URL,
		Type:     "url",
		Pipeline: "web.watch.stable",
		Source:   srv.URL,
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

	assert.Equal(t, false, obj.Metadata["changed"], "no change must be detected for identical content")
}
