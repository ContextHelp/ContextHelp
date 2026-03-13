package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	gohttp "net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	httpserver "github.com/ideacrafterslabs/ctxt/internal/server/http"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// testEnv bundles a running in-process dpkms server for E2E testing.
type testEnv struct {
	URL    string
	svc    *service.Service
	cancel context.CancelFunc
	done   chan error
}

func startTestEnv(t *testing.T) *testEnv {
	t.Helper()

	driver := storageutil.NewTestDriver(t)
	queue := jobs.NewQueue(driver.Jobs())
	pipes := builtins.Registry()
	engine := search.NewEngine(driver)
	svc := service.New(driver, queue, pipes, engine, "", nil)
	router := httpserver.NewRouter(svc, false, nil)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()

	httpSrv := &gohttp.Server{Handler: router}
	pool := jobs.NewWorkerPool(queue, pipes, driver, 2, nil, config.JobsConfig{PollInterval: 50 * time.Millisecond, StaleTimeout: 30 * time.Minute, MaxRetries: 3, MaxHops: 5})

	ctx, cancel := context.WithCancel(context.Background())
	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		if err := httpSrv.Serve(ln); err != nil && err != gohttp.ErrServerClosed {
			return err
		}
		return nil
	})
	g.Go(func() error { return pool.Start(ctx) })
	g.Go(func() error {
		<-ctx.Done()
		return httpSrv.Shutdown(context.Background())
	})

	// Wait for server ready.
	for i := 0; i < 50; i++ {
		resp, err := gohttp.Get(fmt.Sprintf("http://%s/health", addr))
		if err == nil {
			resp.Body.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	done := make(chan error, 1)
	go func() { done <- g.Wait() }()

	return &testEnv{
		URL:    "http://" + addr,
		svc:    svc,
		cancel: cancel,
		done:   done,
	}
}

func (e *testEnv) stop(t *testing.T) {
	t.Helper()
	e.cancel()
	select {
	case err := <-e.done:
		if err != nil {
			t.Errorf("shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("server did not shut down in time")
	}
}

func postAnalyze(t *testing.T, url, content string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"content": content,
		"type":    "text",
		"source":  "e2e-test",
	})

	resp, err := gohttp.Post(url+"/api/v1/analyze", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /analyze: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != gohttp.StatusAccepted {
		t.Fatalf("analyze status: got %d, want 202", resp.StatusCode)
	}

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	return result["job_id"]
}

func waitForJob(t *testing.T, url, jobID string, wantStatus storage.JobStatus) *storage.Job {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/jobs/%s", url, jobID))
		if err != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}

		var job storage.Job
		json.NewDecoder(resp.Body).Decode(&job)
		resp.Body.Close()

		if job.Status == wantStatus {
			return &job
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("job %s never reached status %q", jobID, wantStatus)
	return nil
}

// TestFullWritePath: POST /analyze with text → wait for job completion → GET /objects returns the ingested object.
func TestFullWritePath(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	jobID := postAnalyze(t, env.URL, "short text content for testing")
	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)

	if job.ResultID == "" {
		t.Fatal("result_id should be set on completed job")
	}

	// Verify object exists via API.
	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	if err != nil {
		t.Fatalf("GET object: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != gohttp.StatusOK {
		t.Fatalf("object status: got %d, want 200", resp.StatusCode)
	}

	var obj storage.KnowledgeObject
	json.NewDecoder(resp.Body).Decode(&obj)
	if obj.ID != job.ResultID {
		t.Errorf("object ID: got %q, want %q", obj.ID, job.ResultID)
	}
	if obj.Pipeline != "text.short" {
		t.Errorf("pipeline: got %q, want text.short", obj.Pipeline)
	}
	if obj.Source != "e2e-test" {
		t.Errorf("source: got %q, want e2e-test", obj.Source)
	}
}

// TestFullReadPath: seed objects, GET /search?q=type==article returns correct results.
func TestFullReadPath(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	env.svc.Store.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "obj-1", Type: "article", CreatedAt: now, UpdatedAt: now,
	})
	env.svc.Store.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "obj-2", Type: "note", CreatedAt: now, UpdatedAt: now,
	})
	env.svc.Store.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "obj-3", Type: "article", CreatedAt: now, UpdatedAt: now,
	})

	resp, err := gohttp.Get(env.URL + "/api/v1/search?q=type==article")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	defer resp.Body.Close()

	var body struct {
		Data  []storage.KnowledgeObject `json:"data"`
		Total int                       `json:"total"`
	}
	json.NewDecoder(resp.Body).Decode(&body)

	if body.Total != 2 {
		t.Errorf("total: got %d, want 2", body.Total)
	}
	if len(body.Data) != 2 {
		t.Errorf("data len: got %d, want 2", len(body.Data))
	}
}

// TestJobRetry: POST /analyze with content that uses a nonexistent pipeline → job fails → retry → re-processing.
func TestJobRetry(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	// Enqueue a job with a nonexistent pipeline directly via service.
	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "test retry content",
		Type:     "text",
		Pipeline: "nonexistent.pipeline",
		Source:   "e2e-test",
	})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}

	// Wait for the job to fail.
	job := waitForJob(t, env.URL, jobID, storage.JobFailed)
	if job.Error == "" {
		t.Error("expected error message on failed job")
	}

	// Retry via API.
	req, _ := gohttp.NewRequest(gohttp.MethodPost, fmt.Sprintf("%s/api/v1/jobs/%s/retry", env.URL, jobID), nil)
	resp, err := gohttp.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != gohttp.StatusOK {
		t.Fatalf("retry status: got %d, want 200", resp.StatusCode)
	}

	// Job should go back to pending, then fail again (same bad pipeline).
	waitForJob(t, env.URL, jobID, storage.JobFailed)
}

// TestEdgesCreated: analyze content with mentions → verify edges → verify /entities/{slug}/backlinks.
func TestEdgesCreated(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	// Register a custom pipeline that adds mentions.
	env.svc.Pipes.Upsert("test.mentions", &pipeline.Pipeline{
		PipelineName: "test.mentions",
		Steps:        []pipeline.PipelineStep{&mentionStep{}},
	})

	// Enqueue with the custom pipeline.
	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "content with mentions",
		Type:     "text",
		Pipeline: "test.mentions",
		Source:   "e2e-test",
	})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	if job.ResultID == "" {
		t.Fatal("result_id should be set")
	}

	// Verify backlinks via API.
	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/entities/@test.entity/backlinks", env.URL))
	if err != nil {
		t.Fatalf("backlinks: %v", err)
	}
	defer resp.Body.Close()

	var body struct {
		Data []storage.KnowledgeObject `json:"data"`
	}
	json.NewDecoder(resp.Body).Decode(&body)

	if len(body.Data) != 1 {
		t.Errorf("backlinks: got %d, want 1", len(body.Data))
	}
	if len(body.Data) > 0 && body.Data[0].ID != job.ResultID {
		t.Errorf("backlink ID: got %q, want %q", body.Data[0].ID, job.ResultID)
	}
}

// TestIngestThenSearch: POST /analyze → wait → GET /search?q=type==text → verify ingested object appears.
func TestIngestThenSearch(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	jobID := postAnalyze(t, env.URL, "search test content")
	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID, "completed job must have a result_id")

	resp, err := gohttp.Get(env.URL + "/api/v1/search?q=type==text")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var body struct {
		Data  []storage.KnowledgeObject `json:"data"`
		Total int                       `json:"total"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))

	assert.GreaterOrEqual(t, body.Total, 1, "search should return at least one result")

	found := false
	for _, obj := range body.Data {
		if obj.ID == job.ResultID {
			found = true
			break
		}
	}
	assert.True(t, found, "ingested object %s should appear in search results", job.ResultID)
}

// TestConcurrentIngestion: rapid-fire 5 POST /analyze calls → workers process in parallel → verify 5 distinct objects.
func TestConcurrentIngestion(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	const n = 5
	jobIDs := make([]string, n)

	// Rapid-fire sequential requests — workers process them concurrently.
	for i := 0; i < n; i++ {
		jobIDs[i] = postAnalyze(t, env.URL, fmt.Sprintf("concurrent content %d", i))
	}

	// Wait for all jobs to complete.
	resultIDs := make(map[string]struct{})
	for i := 0; i < n; i++ {
		require.NotEmpty(t, jobIDs[i], "job_id[%d] must not be empty", i)
		job := waitForJob(t, env.URL, jobIDs[i], storage.JobCompleted)
		require.NotEmpty(t, job.ResultID, "job %s must have result_id", jobIDs[i])
		resultIDs[job.ResultID] = struct{}{}
	}
	assert.Len(t, resultIDs, n, "all jobs should produce distinct object IDs")

	// Verify via GET /api/v1/objects.
	resp, err := gohttp.Get(env.URL + "/api/v1/objects?limit=100")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var body struct {
		Data  []*storage.KnowledgeObject `json:"data"`
		Total int                        `json:"total"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.GreaterOrEqual(t, body.Total, n, "total objects should be >= %d", n)
}

// TestObjectDeleteCascade: analyze with mentions → verify edges → DELETE object → verify edges removed.
func TestObjectDeleteCascade(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	// Register a custom pipeline that adds mentions.
	env.svc.Pipes.Upsert("test.mentions", &pipeline.Pipeline{
		PipelineName: "test.mentions",
		Steps:        []pipeline.PipelineStep{&mentionStep{}},
	})

	// Enqueue with the custom pipeline.
	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "cascade delete content with mentions",
		Type:     "text",
		Pipeline: "test.mentions",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID, "completed job must have result_id")

	// Verify edges exist for the result object.
	edges, err := env.svc.Store.Edges().ListFrom(context.Background(), "object", job.ResultID)
	require.NoError(t, err)
	assert.Greater(t, len(edges), 0, "edges should exist after ingestion with mentions")

	// DELETE the object via API.
	req, err := gohttp.NewRequest(gohttp.MethodDelete, fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID), nil)
	require.NoError(t, err)
	resp, err := gohttp.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, gohttp.StatusNoContent, resp.StatusCode)

	// Verify edges are now removed.
	edgesAfter, err := env.svc.Store.Edges().ListFrom(context.Background(), "object", job.ResultID)
	require.NoError(t, err)
	assert.Len(t, edgesAfter, 0, "edges should be removed after object deletion")

	// Also verify the object itself is gone.
	respGet, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer respGet.Body.Close()
	assert.Equal(t, gohttp.StatusNotFound, respGet.StatusCode)
}

// mentionStep adds a test mention to the draft.
type mentionStep struct {
	pipeline.BaseContract
}

func (s *mentionStep) Name() string { return "test-mention" }
func (s *mentionStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	draft.Mentions = []string{"@test.entity"}
	return draft, nil
}
