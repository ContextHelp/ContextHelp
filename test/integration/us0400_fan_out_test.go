package integration

import (
	"context"
	"fmt"
	"net"
	gohttp "net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	uri "hop.top/cite/scheme"

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

// startFanOutEnv creates an E2E env with fan-out enrichment enabled.
func startFanOutEnv(t *testing.T) *testEnv {
	t.Helper()

	driver := storageutil.NewTestDriver(t)
	queue := jobs.NewQueue(driver.Jobs())
	pipes := builtins.Registry()
	engine := search.NewEngine(driver)
	svc := service.New(driver, queue, pipes, engine, "", nil,
		config.Config{FanOut: config.DefaultFanOutConfig()})
	router := httpserver.NewRouter(svc, false, nil)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()

	httpSrv := &gohttp.Server{Handler: router}
	pool := jobs.NewWorkerPool(queue, pipes, driver, 2, nil,
		config.JobsConfig{
			PollInterval: 50 * time.Millisecond,
			StaleTimeout: 30 * time.Minute,
			MaxRetries:   3,
			MaxHops:      5,
		})
	pool.SetFanOut(func(ctx context.Context, objectID string) error {
		_, err := svc.FanOut(ctx, objectID)
		return err
	})

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

// TestUS0400_FanOutCreatesEdges verifies that ingesting text with 3 entities
// produces bidirectional edges for each entity.
func TestUS0400_FanOutCreatesEdges(t *testing.T) {
	env := startFanOutEnv(t)
	defer env.stop(t)

	wantMentions := []uri.URI{
		{Scheme: "ctxt", Namespace: "person", ID: "alice"},
		{Scheme: "ctxt", Namespace: "project", ID: "mobile"},
		{Scheme: "ctxt", Namespace: "org", ID: "acme"},
	}

	env.svc.Pipes.Upsert("text.fanout-test", &pipeline.Pipeline{
		PipelineName: "text.fanout-test",
		Steps: []pipeline.PipelineStep{
			&entityExtractionStep{mentions: wantMentions},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "Alice from Acme discussed the mobile project",
		Type:     "text",
		Pipeline: "text.fanout-test",
		Source:   "e2e-fanout",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	ctx := context.Background()

	// Fan-out runs after job completion; poll for reverse edges.
	var rev []*storage.Edge
	pollUntil(t, func() bool {
		rev, _ = env.svc.Store.Edges().ListTo(ctx, "object", job.ResultID)
		return len(rev) >= 3
	})

	// Forward edges: object -> entity (3 from worker + fan-out dedup).
	fwd, err := env.svc.Store.Edges().ListFrom(ctx, "object", job.ResultID)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(fwd), 3,
		"should have at least 3 forward edges")

	// Reverse edges: entity -> object
	assert.Len(t, rev, 3,
		"should have 3 reverse mentioned_in edges")
	for _, e := range rev {
		assert.Equal(t, "mentioned_in", e.EdgeType)
	}
}

// TestUS0400_FanOutIdempotent verifies re-ingesting same content doesn't
// duplicate edges.
func TestUS0400_FanOutIdempotent(t *testing.T) {
	env := startFanOutEnv(t)
	defer env.stop(t)

	mentions := []uri.URI{
		{Scheme: "ctxt", Namespace: "person", ID: "bob"},
	}

	env.svc.Pipes.Upsert("text.fanout-idem", &pipeline.Pipeline{
		PipelineName: "text.fanout-idem",
		Steps: []pipeline.PipelineStep{
			&entityExtractionStep{mentions: mentions},
		},
	})

	content := "Bob wrote the fan-out design doc"

	// First ingest.
	jobID1, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  content,
		Type:     "text",
		Pipeline: "text.fanout-idem",
		Source:   "e2e-fanout-idem",
	})
	require.NoError(t, err)
	job1 := waitForJob(t, env.URL, jobID1, storage.JobCompleted)
	require.NotEmpty(t, job1.ResultID)

	ctx := context.Background()

	// Wait for fan-out to complete.
	var rev1 []*storage.Edge
	pollUntil(t, func() bool {
		rev1, _ = env.svc.Store.Edges().ListTo(ctx, "object", job1.ResultID)
		return len(rev1) > 0
	})
	initialCount := len(rev1)

	// Run fan-out again on the same object.
	_, err = env.svc.FanOut(ctx, job1.ResultID)
	require.NoError(t, err)

	rev2, err := env.svc.Store.Edges().ListTo(ctx, "object", job1.ResultID)
	require.NoError(t, err)
	assert.Equal(t, initialCount, len(rev2),
		"second fan-out should not create duplicate edges")
	assert.Greater(t, initialCount, 0, "fan-out should have created edges")
}

// TestUS0400_FanOutAuditLog verifies that fan-out records an audit log entry.
func TestUS0400_FanOutAuditLog(t *testing.T) {
	env := startFanOutEnv(t)
	defer env.stop(t)

	mentions := []uri.URI{
		{Scheme: "ctxt", Namespace: "concept", ID: "event-sourcing"},
	}

	env.svc.Pipes.Upsert("text.fanout-audit", &pipeline.Pipeline{
		PipelineName: "text.fanout-audit",
		Steps: []pipeline.PipelineStep{
			&entityExtractionStep{mentions: mentions},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "Event sourcing is a powerful pattern",
		Type:     "text",
		Pipeline: "text.fanout-audit",
		Source:   "e2e-fanout-audit",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	ctx := context.Background()

	// Wait for fan-out to complete (runs after job completion).
	found := pollUntil(t, func() bool {
		history, err := env.svc.Store.AuditLog().GetObjectHistory(ctx, job.ResultID)
		require.NoError(t, err)
		for _, entry := range history {
			if entry.EventType == "fanout.completed" {
				assert.Equal(t, "system", entry.Actor)
				return true
			}
		}
		return false
	})
	assert.True(t, found, "should have a fanout.completed audit entry")
}

// TestUS0400_NoFanoutFlag verifies --no-fanout skips downstream effects.
func TestUS0400_NoFanoutFlag(t *testing.T) {
	env := startFanOutEnv(t)
	defer env.stop(t)

	mentions := []uri.URI{
		{Scheme: "ctxt", Namespace: "person", ID: "carol"},
	}

	env.svc.Pipes.Upsert("text.fanout-skip", &pipeline.Pipeline{
		PipelineName: "text.fanout-skip",
		Steps: []pipeline.PipelineStep{
			&entityExtractionStep{mentions: mentions},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "Carol should not trigger fan-out",
		Type:     "text",
		Pipeline: "text.fanout-skip",
		Source:   "e2e-fanout-nofanout",
		NoFanout: true,
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	// Brief pause to let any (incorrectly triggered) fan-out finish.
	time.Sleep(200 * time.Millisecond)

	ctx := context.Background()

	// Worker still creates forward edges, but fan-out reverse edges should not exist.
	rev, err := env.svc.Store.Edges().ListTo(ctx, "object", job.ResultID)
	require.NoError(t, err)
	assert.Empty(t, rev, "no-fanout should skip reverse edge creation")

	// No audit log entries from fan-out.
	history, err := env.svc.Store.AuditLog().GetObjectHistory(ctx, job.ResultID)
	require.NoError(t, err)
	for _, entry := range history {
		assert.NotEqual(t, "fanout.completed", entry.EventType,
			"no-fanout should skip audit log entries")
	}
}

// TestUS0400_FanOutCompletesInTime verifies fan-out completes within
// a reasonable timeout.
func TestUS0400_FanOutCompletesInTime(t *testing.T) {
	env := startFanOutEnv(t)
	defer env.stop(t)

	mentions := []uri.URI{
		{Scheme: "ctxt", Namespace: "person", ID: "dave"},
		{Scheme: "ctxt", Namespace: "project", ID: "widget"},
	}

	env.svc.Pipes.Upsert("text.fanout-time", &pipeline.Pipeline{
		PipelineName: "text.fanout-time",
		Steps: []pipeline.PipelineStep{
			&entityExtractionStep{mentions: mentions},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "Dave completed the widget project",
		Type:     "text",
		Pipeline: "text.fanout-time",
		Source:   "e2e-fanout-time",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	// Verify fan-out completed by checking reverse edges.
	ctx := context.Background()
	var rev []*storage.Edge
	pollUntil(t, func() bool {
		rev, _ = env.svc.Store.Edges().ListTo(ctx, "object", job.ResultID)
		return len(rev) >= 2
	})
	assert.Len(t, rev, 2)
}
