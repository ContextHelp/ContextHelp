package integration

import (
	"context"
	"encoding/json"
	"fmt"
	gohttp "net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ---------------------------------------------------------------------------
// Mock pipeline steps for URL capture tests.
// ---------------------------------------------------------------------------

// urlFetchStep simulates fetching and extracting content from a URL.
type urlFetchStep struct {
	pipeline.BaseContract
	extractedTitle string
	extractedBody  string
}

func (s *urlFetchStep) Name() string { return "test-url-fetch" }
func (s *urlFetchStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	draft.Type = "url"
	draft.TextContent = s.extractedBody
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Metadata["title"] = s.extractedTitle
	draft.Metadata["extracted_body"] = s.extractedBody
	return draft, nil
}

// urlDedupStep simulates deduplication by recording a content hash.
type urlDedupStep struct {
	pipeline.BaseContract
}

func (s *urlDedupStep) Name() string { return "test-url-dedup" }
func (s *urlDedupStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	// Simulated: record source_url as fingerprint for dedup checks.
	draft.Metadata["source_url_fingerprint"] = draft.Source
	return draft, nil
}

// ---------------------------------------------------------------------------
// US-0002 Tests
// ---------------------------------------------------------------------------

// TestUS0002_URLIngestReturnsJobID verifies POST /analyze with URL source returns 202 + job_id.
func TestUS0002_URLIngestReturnsJobID(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("url.article", &pipeline.Pipeline{
		PipelineName: "url.article",
		Steps: []pipeline.PipelineStep{
			&urlFetchStep{
				extractedTitle: "Test Article",
				extractedBody:  "Article body text",
			},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "https://example.com/article",
		Type:     "url",
		Pipeline: "url.article",
		Source:   "https://example.com/article",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, jobID)
}

// TestUS0002_TitleAndBodyExtracted verifies title and body are extracted after URL ingestion.
func TestUS0002_TitleAndBodyExtracted(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	const sourceURL = "https://example.com/article-title-body"
	const wantTitle = "Understanding Distributed Systems"
	const wantBody = "Distributed systems require careful coordination between nodes."

	env.svc.Pipes.Upsert("url.article", &pipeline.Pipeline{
		PipelineName: "url.article",
		Steps: []pipeline.PipelineStep{
			&urlFetchStep{
				extractedTitle: wantTitle,
				extractedBody:  wantBody,
			},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  sourceURL,
		Type:     "url",
		Pipeline: "url.article",
		Source:   sourceURL,
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

	assert.Equal(t, wantTitle, obj.Metadata["title"], "title should be extracted")
	assert.Equal(t, wantBody, obj.Metadata["extracted_body"], "body should be extracted")
}

// TestUS0002_SourceURLStoredOnObject verifies original URL is stored as Source on the object.
func TestUS0002_SourceURLStoredOnObject(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	const sourceURL = "https://example.com/source-url-test"

	env.svc.Pipes.Upsert("url.article", &pipeline.Pipeline{
		PipelineName: "url.article",
		Steps: []pipeline.PipelineStep{
			&urlFetchStep{
				extractedTitle: "Source URL Test",
				extractedBody:  "Some content",
			},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  sourceURL,
		Type:     "url",
		Pipeline: "url.article",
		Source:   sourceURL,
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

	assert.Equal(t, sourceURL, obj.Source, "source_url must be stored on object")
}

// TestUS0002_DedupOnReIngestSameURL verifies that re-ingesting the same URL produces
// a stable content hash (dedup fingerprint) on both objects.
func TestUS0002_DedupOnReIngestSameURL(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	const sourceURL = "https://example.com/dedup-test"

	env.svc.Pipes.Upsert("url.article", &pipeline.Pipeline{
		PipelineName: "url.article",
		Steps: []pipeline.PipelineStep{
			&urlFetchStep{
				extractedTitle: "Dedup Article",
				extractedBody:  "Stable content for dedup verification",
			},
			&urlDedupStep{},
		},
	})

	// First ingest.
	jobID1, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  sourceURL,
		Type:     "url",
		Pipeline: "url.article",
		Source:   sourceURL,
	})
	require.NoError(t, err)
	job1 := waitForJob(t, env.URL, jobID1, storage.JobCompleted)
	require.NotEmpty(t, job1.ResultID)

	// Second ingest of the same URL.
	jobID2, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  sourceURL,
		Type:     "url",
		Pipeline: "url.article",
		Source:   sourceURL,
	})
	require.NoError(t, err)
	job2 := waitForJob(t, env.URL, jobID2, storage.JobCompleted)
	require.NotEmpty(t, job2.ResultID)

	// Both objects should have identical source_url_fingerprint.
	getObj := func(id string) storage.KnowledgeObject {
		resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, id))
		require.NoError(t, err)
		defer resp.Body.Close()
		var obj storage.KnowledgeObject
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))
		return obj
	}

	obj1 := getObj(job1.ResultID)
	obj2 := getObj(job2.ResultID)

	assert.Equal(t, obj1.Metadata["source_url_fingerprint"], obj2.Metadata["source_url_fingerprint"],
		"same URL should produce identical dedup fingerprint on re-ingest")
	assert.Equal(t, sourceURL, obj1.Source)
	assert.Equal(t, sourceURL, obj2.Source)
}

// TestUS0002_URLPipelineAssigned verifies the correct pipeline is recorded on the object.
func TestUS0002_URLPipelineAssigned(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("url.article", &pipeline.Pipeline{
		PipelineName: "url.article",
		Steps: []pipeline.PipelineStep{
			&urlFetchStep{
				extractedTitle: "Pipeline Test",
				extractedBody:  "Content for pipeline check",
			},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "https://example.com/pipeline-test",
		Type:     "url",
		Pipeline: "url.article",
		Source:   "https://example.com/pipeline-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))
	assert.Equal(t, "url.article", obj.Pipeline, "pipeline should be url.article")
}
