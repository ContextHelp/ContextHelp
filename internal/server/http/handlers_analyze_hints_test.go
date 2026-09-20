package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// postAnalyze posts a JSON body to /api/v1/analyze and returns the job_id
// (object ID on the raw path).
func postAnalyze(t *testing.T, ts *testServerBundle, body map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	resp, err := http.Post(ts.URL+"/api/v1/analyze", "application/json", bytes.NewReader(raw))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusAccepted, resp.StatusCode)
	var result map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	require.NotEmpty(t, result["job_id"])
	return result["job_id"]
}

// runWorkerUntilDone drains the bundle's queue with a real WorkerPool until
// the given job reaches a terminal status.
func runWorkerUntilDone(t *testing.T, ts *testServerBundle, jobID string) *storage.Job {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool := jobs.NewWorkerPool(ts.svc.Queue, ts.svc.Pipes, ts.svc.Store, 1, nil, config.JobsConfig{
		PollInterval: 50 * time.Millisecond,
		StaleTimeout: 30 * time.Minute,
		MaxRetries:   3,
		MaxHops:      5,
	})
	go func() {
		for {
			got, _ := ts.svc.Queue.Get(ctx, jobID)
			if got != nil && (got.Status == storage.JobCompleted || got.Status == storage.JobFailed) {
				cancel()
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(50 * time.Millisecond):
			}
		}
	}()
	pool.Start(ctx)
	got, err := ts.svc.Queue.Get(context.Background(), jobID)
	require.NoError(t, err)
	require.NotNil(t, got)
	return got
}

func userAndAutoTags(tags []storage.Tag) (user map[string]bool, auto int) {
	user = map[string]bool{}
	for _, tag := range tags {
		if tag.Source == "user" {
			user[tag.Label] = true
		} else {
			auto++
		}
	}
	return user, auto
}

// TestAnalyzeRawHintsStoredAsUserTagsAndFTSIndexed covers the raw ingest path
// over HTTP: `hints` must decode into AnalyzeRequest.Hints and land on the
// stored object as Source:"user" tags, and the object must be FTS-indexed.
func TestAnalyzeRawHintsStoredAsUserTagsAndFTSIndexed(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	objID := postAnalyze(t, ts, map[string]any{
		"content": "raw ingest about narwhal tusk sensory function",
		"raw":     true,
		"hints":   []string{"a", "b"},
		"source":  "test",
	})

	ctx := context.Background()
	obj, err := ts.svc.Store.Objects().Get(ctx, objID)
	require.NoError(t, err)
	require.NotNil(t, obj)

	assert.Equal(t, "raw", obj.Status)
	require.Len(t, obj.Tags, 2, "both hints must be stored as tags")
	assert.Equal(t, storage.Tag{Label: "a", Source: "user", Weight: 1}, obj.Tags[0])
	assert.Equal(t, storage.Tag{Label: "b", Source: "user", Weight: 1}, obj.Tags[1])

	assert.True(t, obj.FTSIndexed, "raw object must be fts_indexed=true")
	results, err := ts.svc.Store.Objects().FTSSearch(ctx, "narwhal", storage.ObjectFilter{Limit: 10})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, objID, results[0].ID)

	// tag=="a" scoping must work: the tag filter on List sees the user hint.
	objs, _, err := ts.svc.Store.Objects().List(ctx, storage.ObjectFilter{Tag: "a", Status: "raw"})
	require.NoError(t, err)
	require.Len(t, objs, 1)
	assert.Equal(t, objID, objs[0].ID)
}

// TestAnalyzePipelineHintsSurviveWorkerAndFTSIndexed covers the pipeline
// ingest path over HTTP: hints ride on the job, the worker seeds them as user
// tags, the auto-tagger merges (not overwrites), and the final object is
// FTS-indexed and findable.
func TestAnalyzePipelineHintsSurviveWorkerAndFTSIndexed(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	jobID := postAnalyze(t, ts, map[string]any{
		"content": "pipeline ingest about axolotl limb regeneration and axolotl genomes",
		"hints":   []string{"a", "b"},
		"source":  "test",
	})

	job := runWorkerUntilDone(t, ts, jobID)
	require.Equal(t, storage.JobCompleted, job.Status, "job error: %s", job.Error)
	require.NotEmpty(t, job.ResultID)

	ctx := context.Background()
	obj, err := ts.svc.Store.Objects().Get(ctx, job.ResultID)
	require.NoError(t, err)

	user, auto := userAndAutoTags(obj.Tags)
	assert.True(t, user["a"] && user["b"], "user hints must survive the pipeline: %+v", obj.Tags)
	assert.NotZero(t, auto, "auto tags must be merged alongside user hints: %+v", obj.Tags)

	assert.True(t, obj.FTSIndexed, "pipeline object must be fts_indexed=true")
	results, err := ts.svc.Store.Objects().FTSSearch(ctx, "axolotl", storage.ObjectFilter{Limit: 10})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, obj.ID, results[0].ID)

	objs, _, err := ts.svc.Store.Objects().List(ctx, storage.ObjectFilter{Tag: "a"})
	require.NoError(t, err)
	require.Len(t, objs, 1)
	assert.Equal(t, obj.ID, objs[0].ID)
}

// TestAnalyzeRawThenPipelineSameContentStaysFTSIndexed reproduces the
// raw-then-pipeline sequence: the second ingest dedups by content hash and
// takes the Reinforce path. The object must keep its user hints, gain auto
// tags, and still report fts_indexed=true (it remains in objects_fts).
func TestAnalyzeRawThenPipelineSameContentStaysFTSIndexed(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	const content = "capybara social behaviour observed near capybara wallows"
	objID := postAnalyze(t, ts, map[string]any{
		"content": content,
		"raw":     true,
		"hints":   []string{"a"},
		"source":  "test",
	})
	jobID := postAnalyze(t, ts, map[string]any{
		"content": content,
		"hints":   []string{"b"},
		"source":  "test",
	})
	job := runWorkerUntilDone(t, ts, jobID)
	require.Equal(t, storage.JobCompleted, job.Status, "job error: %s", job.Error)
	require.Equal(t, objID, job.ResultID, "same content must reinforce the raw object")

	ctx := context.Background()
	obj, err := ts.svc.Store.Objects().Get(ctx, objID)
	require.NoError(t, err)

	user, auto := userAndAutoTags(obj.Tags)
	assert.True(t, user["a"] && user["b"], "hints from both ingests must be kept: %+v", obj.Tags)
	assert.NotZero(t, auto, "auto tags must be merged: %+v", obj.Tags)

	assert.True(t, obj.FTSIndexed, "reinforced object must stay fts_indexed=true")
	results, err := ts.svc.Store.Objects().FTSSearch(ctx, "capybara", storage.ObjectFilter{Limit: 10})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, objID, results[0].ID)
}
