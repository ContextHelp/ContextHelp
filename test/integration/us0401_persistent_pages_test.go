package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/uri"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// newTestServiceWithConfig creates a service with custom config for tests
// that don't need the full HTTP env.
func newTestServiceWithConfig(t *testing.T, cfg config.Config) *service.Service {
	t.Helper()
	driver := storageutil.NewTestDriver(t)
	queue := jobs.NewQueue(driver.Jobs())
	pipes := builtins.Registry()
	engine := search.NewEngine(driver)
	return service.New(driver, queue, pipes, engine, "", nil, cfg)
}

// TestUS0401_IngestCreatesEntityPage verifies that ingesting 3 objects
// mentioning the same entity creates a persistent entity page.
func TestUS0401_IngestCreatesEntityPage(t *testing.T) {
	env := startFanOutEnv(t)
	defer env.stop(t)

	entityURI := uri.URI{Scheme: "ctxt", Space: "person", ID: "bob"}
	mentions := []uri.URI{entityURI}

	env.svc.Pipes.Upsert("text.page-test", &pipeline.Pipeline{
		PipelineName: "text.page-test",
		Steps: []pipeline.PipelineStep{
			&entityExtractionStep{mentions: mentions},
		},
	})

	ctx := context.Background()

	// Ingest 3 objects mentioning the same entity.
	for i := 1; i <= 3; i++ {
		req := service.AnalyzeRequest{
			Content:  "Knowledge about Bob #" + string(rune('0'+i)),
			Type:     "text",
			Pipeline: "text.page-test",
		}
		_, err := env.svc.Analyze(ctx, req)
		require.NoError(t, err)
	}

	// Wait for jobs to complete.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		allJobs, _, _ := env.svc.ListJobs(ctx, storage.JobFilter{Limit: 100})
		done := 0
		for _, j := range allJobs {
			if j.Status == storage.JobCompleted || j.Status == storage.JobFailed {
				done++
			}
		}
		if done >= 3 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Verify entity page exists.
	page, err := env.svc.GetEntityPage(ctx, entityURI.String())
	require.NoError(t, err)
	require.NotNil(t, page, "entity page should exist after ingestion")
	assert.Equal(t, service.EntityPageType, page.Type)

	meta, ok := service.ExtractPageMeta(page)
	require.True(t, ok)
	assert.Equal(t, entityURI.String(), meta.EntitySlug)
	assert.GreaterOrEqual(t, len(meta.SourceIDs), 1)
	assert.GreaterOrEqual(t, meta.RevisionCount, 1)
}

// TestUS0401_IncrementalUpdate verifies a 4th ingest increments revision.
func TestUS0401_IncrementalUpdate(t *testing.T) {
	svc := newTestServiceWithConfig(t, config.Config{
		FanOut: config.DefaultFanOutConfig(),
	})
	ctx := context.Background()

	slug := "concept.testing"

	// Seed 3 source objects and upsert pages.
	for i := 1; i <= 3; i++ {
		id := "page-src-" + string(rune('0'+i))
		obj := &storage.KnowledgeObject{
			ID:         id,
			Type:       "text",
			RawContent: "Content about testing #" + string(rune('0'+i)),
			Status:     "active",
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}
		require.NoError(t, svc.Store.Objects().Create(ctx, obj))
		_, err := svc.PageUpsert(ctx, slug, id)
		require.NoError(t, err)
	}

	page1, err := svc.GetEntityPage(ctx, slug)
	require.NoError(t, err)
	meta1, _ := service.ExtractPageMeta(page1)
	assert.Equal(t, 3, meta1.RevisionCount)

	// 4th ingest.
	obj4 := &storage.KnowledgeObject{
		ID:         "page-src-4",
		Type:       "text",
		RawContent: "Fourth source about testing",
		Status:     "active",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	require.NoError(t, svc.Store.Objects().Create(ctx, obj4))

	result, err := svc.PageUpsert(ctx, slug, "page-src-4")
	require.NoError(t, err)
	assert.Equal(t, 4, result.Revision)
	assert.False(t, result.Created)

	page2, err := svc.GetEntityPage(ctx, slug)
	require.NoError(t, err)
	meta2, _ := service.ExtractPageMeta(page2)
	assert.Equal(t, 4, meta2.RevisionCount)
	assert.Len(t, meta2.RevisionLog, 4)
}

// TestUS0401_PageSearchable verifies entity pages appear in FTS search.
func TestUS0401_PageSearchable(t *testing.T) {
	svc := newTestServiceWithConfig(t, config.Config{
		FanOut: config.DefaultFanOutConfig(),
	})
	ctx := context.Background()

	obj := &storage.KnowledgeObject{
		ID:         "search-src-1",
		Type:       "text",
		RawContent: "Unique searchable xylophone content",
		Status:     "active",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	require.NoError(t, svc.Store.Objects().Create(ctx, obj))

	_, err := svc.PageUpsert(ctx, "instrument.xylophone", "search-src-1")
	require.NoError(t, err)

	results, err := svc.FindByText(ctx, "xylophone", 10)
	require.NoError(t, err)

	found := false
	for _, r := range results {
		if r.Type == service.EntityPageType {
			found = true
			break
		}
	}
	assert.True(t, found, "entity page should be searchable via FTS")
}

// TestUS0401_RevisionLogShowsAllUpdates verifies the revision log
// records each source contribution.
func TestUS0401_RevisionLogShowsAllUpdates(t *testing.T) {
	svc := newTestServiceWithConfig(t, config.Config{
		FanOut: config.DefaultFanOutConfig(),
	})
	ctx := context.Background()

	slug := "project.revlog"
	sourceIDs := []string{"rl-1", "rl-2", "rl-3"}

	for _, id := range sourceIDs {
		obj := &storage.KnowledgeObject{
			ID:         id,
			Type:       "text",
			RawContent: "Log content from " + id,
			Status:     "active",
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}
		require.NoError(t, svc.Store.Objects().Create(ctx, obj))
		_, err := svc.PageUpsert(ctx, slug, id)
		require.NoError(t, err)
	}

	page, err := svc.GetEntityPage(ctx, slug)
	require.NoError(t, err)

	meta, ok := service.ExtractPageMeta(page)
	require.True(t, ok)

	assert.Len(t, meta.RevisionLog, 3)
	for i, entry := range meta.RevisionLog {
		assert.Equal(t, sourceIDs[i], entry.SourceID)
		assert.False(t, entry.Date.IsZero())
	}
}
