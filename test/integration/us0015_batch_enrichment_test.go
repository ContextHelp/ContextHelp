package integration

import (
	"context"
	"encoding/json"
	"fmt"
	gohttp "net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ---------------------------------------------------------------------------
// Mock pipeline steps for batch enrichment tests.
// ---------------------------------------------------------------------------

// batchEnrichStep simulates a single enrichment step that marks objects enriched.
type batchEnrichStep struct {
	pipeline.BaseContract
	stepName string
}

func (s *batchEnrichStep) Name() string { return s.stepName }
func (s *batchEnrichStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Metadata["enriched"] = true
	draft.Metadata["enrichment_step"] = s.stepName
	draft.Metadata["enriched_at"] = time.Now().UTC().Format(time.RFC3339)
	return draft, nil
}

// ---------------------------------------------------------------------------
// US-0015 Tests
// ---------------------------------------------------------------------------

// TestUS0015_NObjectsEnqueuedAndAllComplete verifies enqueueing N objects for
// enrichment produces N completed jobs.
func TestUS0015_NObjectsEnqueuedAndAllComplete(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	const n = 5

	env.svc.Pipes.Upsert("text.batch-enrich", &pipeline.Pipeline{
		PipelineName: "text.batch-enrich",
		Steps: []pipeline.PipelineStep{
			&batchEnrichStep{stepName: "test-batch-enrich"},
		},
	})

	jobIDs := make([]string, n)
	for i := 0; i < n; i++ {
		jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
			Content:  fmt.Sprintf("Batch enrichment object %d: content about distributed systems and architecture", i),
			Type:     "text",
			Pipeline: "text.batch-enrich",
			Source:   "e2e-test",
		})
		require.NoError(t, err, "enqueue object %d", i)
		jobIDs[i] = jobID
	}

	// Wait for all N jobs to complete.
	resultIDs := make([]string, n)
	for i, jobID := range jobIDs {
		job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
		require.NotEmpty(t, job.ResultID, "job %d must have result_id", i)
		resultIDs[i] = job.ResultID
	}

	// Verify all N result objects are enriched.
	for i, resultID := range resultIDs {
		resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, resultID))
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, gohttp.StatusOK, resp.StatusCode, "object %d must be retrievable", i)

		var obj storage.KnowledgeObject
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))
		assert.Equal(t, true, obj.Metadata["enriched"],
			"object %d must be marked enriched", i)
	}
}

// TestUS0015_AllEnrichedObjectsHaveEnrichmentMetadata verifies each object enriched
// in a batch has enrichment metadata populated.
func TestUS0015_AllEnrichedObjectsHaveEnrichmentMetadata(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	const n = 3

	env.svc.Pipes.Upsert("text.batch-meta", &pipeline.Pipeline{
		PipelineName: "text.batch-meta",
		Steps: []pipeline.PipelineStep{
			&batchEnrichStep{stepName: "test-batch-meta"},
		},
	})

	resultIDs := make([]string, n)
	for i := 0; i < n; i++ {
		jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
			Content:  fmt.Sprintf("Document %d with metadata verification content", i),
			Type:     "text",
			Pipeline: "text.batch-meta",
			Source:   "e2e-test",
		})
		require.NoError(t, err)

		job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
		require.NotEmpty(t, job.ResultID)
		resultIDs[i] = job.ResultID
	}

	for i, resultID := range resultIDs {
		resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, resultID))
		require.NoError(t, err)
		defer resp.Body.Close()

		var obj storage.KnowledgeObject
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

		assert.NotNil(t, obj.Metadata, "object %d must have metadata", i)
		assert.Equal(t, true, obj.Metadata["enriched"], "object %d: enriched=true", i)
		assert.Equal(t, "test-batch-meta", obj.Metadata["enrichment_step"],
			"object %d: enrichment_step must record which step enriched it", i)
		assert.NotEmpty(t, obj.Metadata["enriched_at"],
			"object %d: enriched_at timestamp must be set", i)
	}
}

// TestUS0015_DistinctObjectIDsForBatchItems verifies each batch item produces
// a unique result object ID.
func TestUS0015_DistinctObjectIDsForBatchItems(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	const n = 4

	env.svc.Pipes.Upsert("text.batch-ids", &pipeline.Pipeline{
		PipelineName: "text.batch-ids",
		Steps: []pipeline.PipelineStep{
			&batchEnrichStep{stepName: "test-batch-ids"},
		},
	})

	resultIDSet := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
			Content:  fmt.Sprintf("Unique batch object %d with distinct content", i),
			Type:     "text",
			Pipeline: "text.batch-ids",
			Source:   "e2e-test",
		})
		require.NoError(t, err)

		job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
		require.NotEmpty(t, job.ResultID)
		resultIDSet[job.ResultID] = struct{}{}
	}

	assert.Len(t, resultIDSet, n, "all %d batch objects must have distinct result IDs", n)
}

// TestUS0015_JobsProcessedIndependently verifies each job in the batch is
// independently tracked and can be queried individually via GET /jobs/{id}.
func TestUS0015_JobsProcessedIndependently(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	const n = 3

	env.svc.Pipes.Upsert("text.batch-independent", &pipeline.Pipeline{
		PipelineName: "text.batch-independent",
		Steps: []pipeline.PipelineStep{
			&batchEnrichStep{stepName: "test-batch-independent"},
		},
	})

	jobIDs := make([]string, n)
	for i := 0; i < n; i++ {
		jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
			Content:  fmt.Sprintf("Independent batch job %d content", i),
			Type:     "text",
			Pipeline: "text.batch-independent",
			Source:   "e2e-test",
		})
		require.NoError(t, err)
		jobIDs[i] = jobID
	}

	// Each job must be independently queryable.
	for i, jobID := range jobIDs {
		require.NotEmpty(t, jobID, "job %d must have an ID", i)

		job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
		assert.Equal(t, storage.JobCompleted, job.Status,
			"job %d must reach completed status independently", i)
		assert.NotEmpty(t, job.ResultID,
			"job %d must have a result_id after completion", i)

		// Verify job is queryable via API.
		resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/jobs/%s", env.URL, jobID))
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, gohttp.StatusOK, resp.StatusCode,
			"job %d must be individually queryable via GET /jobs/{id}", i)
	}
}

// TestUS0015_BatchProgressCountsConsistent verifies total objects equal the number
// submitted and all eventually reach completed status.
func TestUS0015_BatchProgressCountsConsistent(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	const n = 6

	env.svc.Pipes.Upsert("text.batch-progress", &pipeline.Pipeline{
		PipelineName: "text.batch-progress",
		Steps: []pipeline.PipelineStep{
			&batchEnrichStep{stepName: "test-batch-progress"},
		},
	})

	jobIDs := make([]string, n)
	for i := 0; i < n; i++ {
		jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
			Content:  fmt.Sprintf("Progress tracking content for object %d", i),
			Type:     "text",
			Pipeline: "text.batch-progress",
			Source:   "e2e-test",
		})
		require.NoError(t, err)
		jobIDs[i] = jobID
	}

	completedCount := 0
	for _, jobID := range jobIDs {
		job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
		if job.Status == storage.JobCompleted {
			completedCount++
		}
	}

	assert.Equal(t, n, completedCount,
		"all %d objects in the batch must complete enrichment", n)
}
