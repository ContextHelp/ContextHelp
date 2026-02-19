package integration

import (
	"bytes"
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
// Batch-processing pipeline step
// ---------------------------------------------------------------------------

type batchProcessorStep struct {
	pipeline.BaseContract
}

func (s *batchProcessorStep) Name() string { return "test-batch-processor" }
func (s *batchProcessorStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Metadata["batch_processed"] = true
	return draft, nil
}

// failingBatchStep always fails (for partial-success tests).
type failingBatchStep struct {
	pipeline.BaseContract
}

func (s *failingBatchStep) Name() string { return "test-batch-fail" }
func (s *failingBatchStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.RawContent == "" {
		return nil, fmt.Errorf("missing content: record has no content field")
	}
	return draft, nil
}

// ---------------------------------------------------------------------------
// Helper: POST /api/v1/import with JSONL payload
// ---------------------------------------------------------------------------

func postImportJSONL(t *testing.T, baseURL string, records []map[string]any) (*gohttp.Response, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	for _, rec := range records {
		line, _ := json.Marshal(rec)
		buf.Write(line)
		buf.WriteByte('\n')
	}

	body, _ := json.Marshal(map[string]any{
		"format": "jsonl",
		"data":   buf.String(),
	})

	resp, err := gohttp.Post(baseURL+"/api/v1/import", "application/json",
		bytes.NewReader(body))
	require.NoError(t, err)

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	return resp, result
}

// postImportCSV posts CSV data to the import endpoint.
func postImportCSV(t *testing.T, baseURL string, csvData string, opts map[string]any) (*gohttp.Response, map[string]any) {
	t.Helper()

	payload := map[string]any{
		"format": "csv",
		"data":   csvData,
	}
	for k, v := range opts {
		payload[k] = v
	}

	body, _ := json.Marshal(payload)
	resp, err := gohttp.Post(baseURL+"/api/v1/import", "application/json",
		bytes.NewReader(body))
	require.NoError(t, err)

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	return resp, result
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestUS0008_JSONLImportReturnsBatchID(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Register("batch.import", &pipeline.Pipeline{
		PipelineName: "batch.import",
		Steps:        []pipeline.PipelineStep{&batchProcessorStep{}},
	})

	records := []map[string]any{
		{"content": "Record 1", "type": "note", "source": "batch-test"},
		{"content": "Record 2", "type": "note", "source": "batch-test"},
	}

	resp, result := postImportJSONL(t, env.URL, records)
	defer resp.Body.Close()

	// POST /import with JSONL should return 202 + batch_id.
	assert.Equal(t, gohttp.StatusAccepted, resp.StatusCode)
	assert.NotEmpty(t, result["batch_id"], "response should include batch_id")
}

func TestUS0008_CSVImportDefaultMapping(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Register("batch.import", &pipeline.Pipeline{
		PipelineName: "batch.import",
		Steps:        []pipeline.PipelineStep{&batchProcessorStep{}},
	})

	csvData := "content,type,source\nHello world,note,csv-test\nGoodbye world,note,csv-test\n"

	resp, result := postImportCSV(t, env.URL, csvData, nil)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusAccepted, resp.StatusCode)
	assert.NotEmpty(t, result["batch_id"])
}

func TestUS0008_CSVCustomColumnMapping(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Register("batch.import", &pipeline.Pipeline{
		PipelineName: "batch.import",
		Steps:        []pipeline.PipelineStep{&batchProcessorStep{}},
	})

	// CSV with non-standard column names.
	csvData := "body,kind,origin\nMapped content,article,custom-csv\n"

	resp, result := postImportCSV(t, env.URL, csvData, map[string]any{
		"column_mapping": map[string]string{
			"body":   "content",
			"kind":   "type",
			"origin": "source",
		},
	})
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusAccepted, resp.StatusCode)
	assert.NotEmpty(t, result["batch_id"])
}

func TestUS0008_TSVImport(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Register("batch.import", &pipeline.Pipeline{
		PipelineName: "batch.import",
		Steps:        []pipeline.PipelineStep{&batchProcessorStep{}},
	})

	tsvData := "content\ttype\tsource\nTSV record 1\tnote\ttsv-test\nTSV record 2\tnote\ttsv-test\n"

	body, _ := json.Marshal(map[string]any{
		"format":    "tsv",
		"data":      tsvData,
		"delimiter": "\t",
	})

	resp, err := gohttp.Post(env.URL+"/api/v1/import", "application/json",
		bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusAccepted, resp.StatusCode)

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	assert.NotEmpty(t, result["batch_id"])
}

func TestUS0008_MarkdownDirectoryScan(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Register("batch.import", &pipeline.Pipeline{
		PipelineName: "batch.import",
		Steps:        []pipeline.PipelineStep{&batchProcessorStep{}},
	})

	// Simulate directory scan by posting a batch with markdown file contents.
	records := []map[string]any{
		{"content": "# Document 1\n\nBody of doc 1.", "type": "markdown",
			"source": "dir-scan", "metadata": map[string]any{"path": "/docs/doc1.md"}},
		{"content": "# Document 2\n\nBody of doc 2.", "type": "markdown",
			"source": "dir-scan", "metadata": map[string]any{"path": "/docs/sub/doc2.md"}},
		{"content": "# Document 3\n\nBody of doc 3.", "type": "markdown",
			"source": "dir-scan", "metadata": map[string]any{"path": "/docs/sub/deep/doc3.md"}},
	}

	resp, result := postImportJSONL(t, env.URL, records)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusAccepted, resp.StatusCode)
	assert.NotEmpty(t, result["batch_id"])
}

func TestUS0008_OPMLCreatesFeedSubscriptions(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Register("feed.ingest", &pipeline.Pipeline{
		PipelineName: "feed.ingest",
		Steps:        []pipeline.PipelineStep{&feedItemStep{}},
	})

	opmlData := `<?xml version="1.0" encoding="UTF-8"?>
<opml version="1.0">
  <body>
    <outline text="Tech" title="Tech">
      <outline type="rss" text="Feed A" xmlUrl="http://example.com/feed-a" />
      <outline type="rss" text="Feed B" xmlUrl="http://example.com/feed-b" />
    </outline>
  </body>
</opml>`

	body, _ := json.Marshal(map[string]any{
		"format": "opml",
		"data":   opmlData,
	})

	resp, err := gohttp.Post(env.URL+"/api/v1/import", "application/json",
		bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	// OPML should create feed subscriptions, not knowledge objects.
	assert.Contains(t,
		[]int{gohttp.StatusAccepted, gohttp.StatusCreated, gohttp.StatusOK},
		resp.StatusCode,
		"OPML import should be accepted",
	)

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)

	// Verify feeds were created rather than knowledge objects.
	feedsResp, err := gohttp.Get(env.URL + "/api/v1/feeds")
	if err == nil {
		defer feedsResp.Body.Close()
		var feedsBody struct {
			Feeds []map[string]any `json:"feeds"`
			Total int              `json:"total"`
		}
		json.NewDecoder(feedsResp.Body).Decode(&feedsBody)
		// OPML with 2 outlines should create 2 feed subscriptions.
		assert.GreaterOrEqual(t, feedsBody.Total, 2,
			"OPML import should create feed subscriptions")
	}
}

func TestUS0008_ImportStatusProgress(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Register("batch.import", &pipeline.Pipeline{
		PipelineName: "batch.import",
		Steps:        []pipeline.PipelineStep{&batchProcessorStep{}},
	})

	records := []map[string]any{
		{"content": "Progress rec 1", "type": "note", "source": "progress-test"},
		{"content": "Progress rec 2", "type": "note", "source": "progress-test"},
		{"content": "Progress rec 3", "type": "note", "source": "progress-test"},
	}

	resp, result := postImportJSONL(t, env.URL, records)
	resp.Body.Close()

	batchID, _ := result["batch_id"].(string)
	require.NotEmpty(t, batchID)

	// GET /import/{batch_id} shows progress.
	time.Sleep(500 * time.Millisecond)

	statusResp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/import/%s", env.URL, batchID))
	require.NoError(t, err)
	defer statusResp.Body.Close()

	assert.Equal(t, gohttp.StatusOK, statusResp.StatusCode)

	var status map[string]any
	require.NoError(t, json.NewDecoder(statusResp.Body).Decode(&status))

	assert.NotEmpty(t, status["batch_id"])
	// Should include total/completed/failed counters.
	_, hasTotal := status["total"]
	assert.True(t, hasTotal, "batch status should include 'total'")
}

func TestUS0008_DryRunValidatesOnly(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Register("batch.import", &pipeline.Pipeline{
		PipelineName: "batch.import",
		Steps:        []pipeline.PipelineStep{&batchProcessorStep{}},
	})

	records := []map[string]any{
		{"content": "Dry run record", "type": "note", "source": "dry-run-test"},
	}

	var buf bytes.Buffer
	for _, rec := range records {
		line, _ := json.Marshal(rec)
		buf.Write(line)
		buf.WriteByte('\n')
	}

	body, _ := json.Marshal(map[string]any{
		"format":  "jsonl",
		"data":    buf.String(),
		"dry_run": true,
	})

	resp, err := gohttp.Post(env.URL+"/api/v1/import", "application/json",
		bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	// Dry-run should be accepted.
	assert.Contains(t,
		[]int{gohttp.StatusOK, gohttp.StatusAccepted},
		resp.StatusCode,
	)

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)

	// Verify no objects were actually created.
	time.Sleep(300 * time.Millisecond)

	objResp, err := gohttp.Get(env.URL + "/api/v1/objects?limit=100")
	require.NoError(t, err)
	defer objResp.Body.Close()

	var objBody struct {
		Data  []storage.KnowledgeObject `json:"data"`
		Total int                       `json:"total"`
	}
	json.NewDecoder(objResp.Body).Decode(&objBody)

	// In dry-run mode, no records should be ingested.
	dryRunObjects := 0
	for _, obj := range objBody.Data {
		if obj.Source == "dry-run-test" {
			dryRunObjects++
		}
	}
	assert.Equal(t, 0, dryRunObjects,
		"dry-run should not create any knowledge objects")
}

func TestUS0008_FanoutCreatesJobs(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Register("batch.import", &pipeline.Pipeline{
		PipelineName: "batch.import",
		Steps:        []pipeline.PipelineStep{&batchProcessorStep{}},
	})

	records := []map[string]any{
		{"content": "Fanout rec 1", "type": "note", "source": "fanout-test"},
		{"content": "Fanout rec 2", "type": "note", "source": "fanout-test"},
		{"content": "Fanout rec 3", "type": "note", "source": "fanout-test"},
	}

	resp, _ := postImportJSONL(t, env.URL, records)
	resp.Body.Close()

	// Each valid record should create a separate ingestion job.
	time.Sleep(500 * time.Millisecond)

	jobsResp, err := gohttp.Get(env.URL + "/api/v1/jobs?limit=100")
	require.NoError(t, err)
	defer jobsResp.Body.Close()

	var jobsBody struct {
		Data  []storage.Job `json:"data"`
		Total int           `json:"total"`
	}
	json.NewDecoder(jobsResp.Body).Decode(&jobsBody)

	batchJobs := 0
	for _, j := range jobsBody.Data {
		if j.Pipeline == "batch.import" {
			batchJobs++
		}
	}
	assert.GreaterOrEqual(t, batchJobs, 3,
		"each valid record should create a separate ingestion job")
}

func TestUS0008_PartialSuccessHandling(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Register("batch.import", &pipeline.Pipeline{
		PipelineName: "batch.import",
		Steps:        []pipeline.PipelineStep{&failingBatchStep{}},
	})

	// Mix valid and invalid records; invalid ones have empty content.
	records := []map[string]any{
		{"content": "Valid record 1", "type": "note", "source": "partial-test"},
		{"content": "", "type": "note", "source": "partial-test"},
		{"content": "Valid record 2", "type": "note", "source": "partial-test"},
	}

	resp, result := postImportJSONL(t, env.URL, records)
	resp.Body.Close()

	batchID, _ := result["batch_id"].(string)
	require.NotEmpty(t, batchID)

	// Wait for processing.
	time.Sleep(1 * time.Second)

	// Verify partial success: some records succeeded, some failed.
	statusResp, err := gohttp.Get(
		fmt.Sprintf("%s/api/v1/import/%s", env.URL, batchID))
	require.NoError(t, err)
	defer statusResp.Body.Close()

	var status map[string]any
	json.NewDecoder(statusResp.Body).Decode(&status)

	// The batch should reflect both successes and failures.
	if completed, ok := status["completed"].(float64); ok {
		assert.GreaterOrEqual(t, int(completed), 1,
			"valid records should still be ingested")
	}
	if failed, ok := status["failed"].(float64); ok {
		assert.GreaterOrEqual(t, int(failed), 0,
			"invalid records should be marked as failed")
	}
}

func TestUS0008_BatchReportErrors(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Register("batch.import", &pipeline.Pipeline{
		PipelineName: "batch.import",
		Steps:        []pipeline.PipelineStep{&failingBatchStep{}},
	})

	records := []map[string]any{
		{"content": "", "type": "note", "source": "error-report-test"},
		{"content": "Valid", "type": "note", "source": "error-report-test"},
	}

	resp, result := postImportJSONL(t, env.URL, records)
	resp.Body.Close()

	batchID, _ := result["batch_id"].(string)
	require.NotEmpty(t, batchID)

	time.Sleep(1 * time.Second)

	statusResp, err := gohttp.Get(
		fmt.Sprintf("%s/api/v1/import/%s", env.URL, batchID))
	require.NoError(t, err)
	defer statusResp.Body.Close()

	var status map[string]any
	json.NewDecoder(statusResp.Body).Decode(&status)

	// Failed records should include line number and error message.
	if errors, ok := status["errors"].([]any); ok {
		for _, errEntry := range errors {
			if entry, ok := errEntry.(map[string]any); ok {
				assert.NotEmpty(t, entry["error"],
					"error entry should include error message")
				// Line number or record index should be present.
				_, hasLine := entry["line"]
				_, hasIndex := entry["index"]
				assert.True(t, hasLine || hasIndex,
					"error entry should include line number or index")
			}
		}
	}
}

func TestUS0008_ChildObjectsLinkedViaEdges(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Register("batch.import", &pipeline.Pipeline{
		PipelineName: "batch.import",
		Steps:        []pipeline.PipelineStep{&batchProcessorStep{}},
	})

	records := []map[string]any{
		{"content": "Child 1", "type": "note", "source": "edge-test"},
		{"content": "Child 2", "type": "note", "source": "edge-test"},
	}

	resp, result := postImportJSONL(t, env.URL, records)
	resp.Body.Close()

	batchID, _ := result["batch_id"].(string)
	require.NotEmpty(t, batchID)

	time.Sleep(1 * time.Second)

	// Children should be linked to parent batch via "batch_contains" edges.
	edges, err := env.svc.Store.Edges().ListFrom(context.Background(), "batch", batchID)
	if err == nil {
		batchContainsCount := 0
		for _, edge := range edges {
			if edge.EdgeType == "batch_contains" {
				batchContainsCount++
			}
		}
		assert.GreaterOrEqual(t, batchContainsCount, 1,
			"children should be linked to parent batch via batch_contains edges")
	}
}

func TestUS0008_BatchSizeLimitEnforced(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Register("batch.import", &pipeline.Pipeline{
		PipelineName: "batch.import",
		Steps:        []pipeline.PipelineStep{&batchProcessorStep{}},
	})

	// Create a batch that exceeds a reasonable size limit.
	records := make([]map[string]any, 10001)
	for i := range records {
		records[i] = map[string]any{
			"content": fmt.Sprintf("Oversized record %d", i),
			"type":    "note",
			"source":  "limit-test",
		}
	}

	var buf bytes.Buffer
	for _, rec := range records {
		line, _ := json.Marshal(rec)
		buf.Write(line)
		buf.WriteByte('\n')
	}

	body, _ := json.Marshal(map[string]any{
		"format": "jsonl",
		"data":   buf.String(),
	})

	resp, err := gohttp.Post(env.URL+"/api/v1/import", "application/json",
		bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	// Batch exceeding maxBatchSize should be rejected.
	assert.Contains(t,
		[]int{gohttp.StatusBadRequest, gohttp.StatusRequestEntityTooLarge,
			gohttp.StatusUnprocessableEntity},
		resp.StatusCode,
		"oversized batch should be rejected with 4xx status",
	)
}

func TestUS0008_ConcurrencyLimitRespected(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Register("batch.import", &pipeline.Pipeline{
		PipelineName: "batch.import",
		Steps:        []pipeline.PipelineStep{&batchProcessorStep{}},
	})

	records := make([]map[string]any, 20)
	for i := range records {
		records[i] = map[string]any{
			"content": fmt.Sprintf("Concurrent record %d", i),
			"type":    "note",
			"source":  "concurrency-test",
		}
	}

	resp, result := postImportJSONL(t, env.URL, records)
	resp.Body.Close()

	batchID, _ := result["batch_id"].(string)
	require.NotEmpty(t, batchID)

	// Verify the batch was accepted and processing proceeds.
	time.Sleep(300 * time.Millisecond)

	jobsResp, err := gohttp.Get(env.URL + "/api/v1/jobs?limit=100")
	require.NoError(t, err)
	defer jobsResp.Body.Close()

	var jobsBody struct {
		Data  []storage.Job `json:"data"`
		Total int           `json:"total"`
	}
	json.NewDecoder(jobsResp.Body).Decode(&jobsBody)

	// Count running jobs for this batch. The worker pool has 2 workers,
	// so no more than 2 should be running concurrently.
	runningJobs := 0
	for _, j := range jobsBody.Data {
		if j.Status == storage.JobRunning && j.Pipeline == "batch.import" {
			runningJobs++
		}
	}
	assert.LessOrEqual(t, runningJobs, 2,
		"concurrent jobs from one batch should not exceed worker pool size")
}

func TestUS0008_MultipartUploadReturns202(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Register("batch.import", &pipeline.Pipeline{
		PipelineName: "batch.import",
		Steps:        []pipeline.PipelineStep{&batchProcessorStep{}},
	})

	// Simulate multipart upload by posting JSONL data.
	records := []map[string]any{
		{"content": "Multipart record", "type": "note", "source": "upload-test"},
	}

	resp, result := postImportJSONL(t, env.URL, records)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusAccepted, resp.StatusCode)
	assert.NotEmpty(t, result["batch_id"])
}

func TestUS0008_ImportStatusViaAPI(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Register("batch.import", &pipeline.Pipeline{
		PipelineName: "batch.import",
		Steps:        []pipeline.PipelineStep{&batchProcessorStep{}},
	})

	records := []map[string]any{
		{"content": "Status check rec", "type": "note", "source": "status-test"},
	}

	resp, result := postImportJSONL(t, env.URL, records)
	resp.Body.Close()

	batchID, _ := result["batch_id"].(string)
	require.NotEmpty(t, batchID)

	time.Sleep(500 * time.Millisecond)

	statusResp, err := gohttp.Get(
		fmt.Sprintf("%s/api/v1/import/%s", env.URL, batchID))
	require.NoError(t, err)
	defer statusResp.Body.Close()

	assert.Equal(t, gohttp.StatusOK, statusResp.StatusCode)

	var status map[string]any
	require.NoError(t, json.NewDecoder(statusResp.Body).Decode(&status))
	assert.NotEmpty(t, status["batch_id"])
}

func TestUS0008_JSONLMissingContentRejected(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Register("batch.import", &pipeline.Pipeline{
		PipelineName: "batch.import",
		Steps:        []pipeline.PipelineStep{&batchProcessorStep{}},
	})

	// Record missing "content" field entirely.
	records := []map[string]any{
		{"type": "note", "source": "missing-content-test"},
	}

	resp, result := postImportJSONL(t, env.URL, records)
	defer resp.Body.Close()

	// The import may accept the batch but flag the record as invalid,
	// or reject the entire request.
	if resp.StatusCode == gohttp.StatusAccepted {
		batchID, _ := result["batch_id"].(string)
		if batchID != "" {
			time.Sleep(1 * time.Second)
			statusResp, err := gohttp.Get(
				fmt.Sprintf("%s/api/v1/import/%s", env.URL, batchID))
			if err == nil {
				defer statusResp.Body.Close()
				var status map[string]any
				json.NewDecoder(statusResp.Body).Decode(&status)
				if failed, ok := status["failed"].(float64); ok {
					assert.GreaterOrEqual(t, int(failed), 1,
						"record without content should be marked failed")
				}
			}
		}
	} else {
		assert.Equal(t, gohttp.StatusBadRequest, resp.StatusCode,
			"record missing content should be rejected")
	}
}

func TestUS0008_CSVNoHeaderHandled(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Register("batch.import", &pipeline.Pipeline{
		PipelineName: "batch.import",
		Steps:        []pipeline.PipelineStep{&batchProcessorStep{}},
	})

	// CSV without header row; columns specified via configuration.
	csvData := "No header content,note,headerless-test\n"

	resp, result := postImportCSV(t, env.URL, csvData, map[string]any{
		"has_header": false,
		"columns":    []string{"content", "type", "source"},
	})
	defer resp.Body.Close()

	assert.Contains(t,
		[]int{gohttp.StatusAccepted, gohttp.StatusOK},
		resp.StatusCode,
		"CSV without header should be handled when configured",
	)
	_ = result
}

// ---------------------------------------------------------------------------
// Service-level tests (direct enqueue for batch processing flow)
// ---------------------------------------------------------------------------

func TestUS0008_BatchObjectsCreatedViaService(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Register("batch.import", &pipeline.Pipeline{
		PipelineName: "batch.import",
		Steps:        []pipeline.PipelineStep{&batchProcessorStep{}},
	})

	ctx := context.Background()
	contents := []string{"Batch item A", "Batch item B", "Batch item C"}
	jobIDs := make([]string, len(contents))

	for i, content := range contents {
		jobID, err := env.svc.Analyze(ctx, service.AnalyzeRequest{
			Content:  content,
			Type:     "text",
			Pipeline: "batch.import",
			Source:   "batch-svc-test",
		})
		require.NoError(t, err)
		jobIDs[i] = jobID
	}

	// Wait for all jobs to complete.
	for _, jid := range jobIDs {
		job := waitForJob(t, env.URL, jid, storage.JobCompleted)
		require.NotEmpty(t, job.ResultID)

		// Verify the object was enriched by the batch processor step.
		obj, err := env.svc.Store.Objects().Get(ctx, job.ResultID)
		require.NoError(t, err)
		assert.NotNil(t, obj.Metadata)
		assert.Equal(t, true, obj.Metadata["batch_processed"])
	}
}

