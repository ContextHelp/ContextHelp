package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// TestArchivePipeline_MissingReturns404 confirms the handler maps the
// service's "pipeline %q not found" error to HTTP 404 instead of 500.
// Regression guard for the contract change introduced when T-1294
// added a Get-before-Update inside service.ArchivePipeline (the prior
// storage path silently no-op'd on missing rows).
func TestArchivePipeline_MissingReturns404(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/pipelines/does-not-exist/archive", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set(HeaderCtxtNote, "ops review")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", resp.StatusCode)
	}
}

// TestUnarchivePipeline_MissingReturns404 mirrors the archive guard
// for the unarchive path. Same root cause (T-1294 Get-before-Update),
// same contract preservation.
func TestUnarchivePipeline_MissingReturns404(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/pipelines/does-not-exist/unarchive", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", resp.StatusCode)
	}
}

// enqueueRequest posts to the enqueue endpoint and returns status + envelope.
func enqueueRequest(t *testing.T, ts *testServerBundle, body string) (int, ErrorEnvelope) {
	t.Helper()
	resp, err := http.Post(ts.URL+"/api/v1/pipelines/enqueue", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	var env ErrorEnvelope
	// A 202 body is not an envelope; decode errors are irrelevant then.
	_ = json.NewDecoder(resp.Body).Decode(&env)
	return resp.StatusCode, env
}

// newEnqueueTestServer builds a server whose service registry is empty, so an
// unknown pipeline trips the service's own pre-flight check.
func newEnqueueTestServer(t *testing.T) *testServerBundle {
	t.Helper()
	driver := storageutil.NewTestDriver(t)
	q := jobs.NewQueue(driver.Jobs())
	engine := search.NewEngine(driver)
	svc := service.New(driver, q, pipeline.DefaultRegistry(), engine, "", nil)
	return &testServerBundle{
		Server: httptest.NewServer(NewRouter(svc, false, nil)),
		svc:    svc,
	}
}

// TestEnqueue_ServiceSentinelReturns400 covers the regression: an unknown
// pipeline raised by the service layer must map to 400. The handler
// previously matched only jobs.ErrPipelineNotFound, so errors.Is never fired
// on the service sentinel and a plainly invalid request fell through to 500.
func TestEnqueue_ServiceSentinelReturns400(t *testing.T) {
	ts := newEnqueueTestServer(t)
	defer ts.Close()

	status, env := enqueueRequest(t, ts, `{"content":"hello","type":"text","pipeline":"does.not.exist"}`)
	if status != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", status)
	}
	if env.Error.Code != "INVALID_REQUEST" {
		t.Errorf("code: got %q, want INVALID_REQUEST", env.Error.Code)
	}
	if !strings.Contains(env.Error.Message, "does.not.exist") {
		t.Errorf("message should name the pipeline, got %q", env.Error.Message)
	}
}

// TestEnqueue_JobsSentinelReturns400 covers the other sentinel: the pipeline
// resolves at the service layer but the queue validator rejects it deeper in.
// Both sentinels must produce 400, never 500.
func TestEnqueue_JobsSentinelReturns400(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	q := jobs.NewQueue(driver.Jobs())
	// The name resolves in the registry, so the service pre-flight passes and
	// only the queue validator objects — isolating jobs.ErrPipelineNotFound.
	q.SetPipelineValidator(func(name string) error {
		return fmt.Errorf("pipeline %q not routable", name)
	})
	pipes := pipeline.DefaultRegistry()
	pipes.Upsert("text.short", &pipeline.Pipeline{PipelineName: "text.short"})
	engine := search.NewEngine(driver)
	svc := service.New(driver, q, pipes, engine, "", nil)
	ts := &testServerBundle{
		Server: httptest.NewServer(NewRouter(svc, false, nil)),
		svc:    svc,
	}
	defer ts.Close()

	status, env := enqueueRequest(t, ts, `{"content":"hello","type":"text","pipeline":"text.short"}`)
	if status != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", status)
	}
	if env.Error.Code != "INVALID_REQUEST" {
		t.Errorf("code: got %q, want INVALID_REQUEST", env.Error.Code)
	}
}

// TestEnqueue_StoreBackedPipelineAccepted is the user-facing shape of the
// service bug: a pipeline created through the API (store-only, never in the
// registry) must enqueue rather than 400.
func TestEnqueue_StoreBackedPipelineAccepted(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	q := jobs.NewQueue(driver.Jobs())
	engine := search.NewEngine(driver)
	svc := service.New(driver, q, pipeline.DefaultRegistry(), engine, "", nil)
	now := time.Now().Truncate(time.Second)
	if err := driver.Pipelines().Create(context.Background(), &storage.Pipeline{
		ID:        "pipeline-user-custom",
		Name:      "user.custom",
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed pipeline: %v", err)
	}
	ts := &testServerBundle{
		Server: httptest.NewServer(NewRouter(svc, false, nil)),
		svc:    svc,
	}
	defer ts.Close()

	status, env := enqueueRequest(t, ts, `{"content":"hello","type":"text","pipeline":"user.custom"}`)
	if status != http.StatusAccepted {
		t.Fatalf("status: got %d, want 202 (envelope: %+v)", status, env.Error)
	}
}
