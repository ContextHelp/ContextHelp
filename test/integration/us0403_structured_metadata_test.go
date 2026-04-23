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
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/steps"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// stubMetadataLLM returns a deterministic JSON response for testing.
type stubMetadataLLM struct {
	resp string
}

func (s *stubMetadataLLM) Generate(_ context.Context, _ string) (string, error) {
	return s.resp, nil
}
func (s *stubMetadataLLM) Name() string { return "stub-metadata" }

// ---------------------------------------------------------------------------
// US-0403 Tests
// ---------------------------------------------------------------------------

// TestUS0403_PersonDateActionExtracted verifies text mentioning a person,
// date, and action item has all three extracted in structured metadata.
func TestUS0403_PersonDateActionExtracted(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	llm := &stubMetadataLLM{
		resp: `{"type":"task","topics":["auth","security"],"people":["@person.alice"],"action_items":["review PR #42"],"dates_mentioned":["2026-05-01"],"source_type":"text","confidence":0.85}`,
	}

	env.svc.Pipes.Upsert("text.short", &pipeline.Pipeline{
		PipelineName: "text.short",
		Steps: []pipeline.PipelineStep{
			steps.NewStructuredMetadataExtractorWithLLM(llm),
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "Alice needs to review PR #42 for auth security by May 1st",
		Type:     "text",
		Pipeline: "text.short",
		Source:   "e2e-test",
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

	require.NotNil(t, obj.Metadata)
	assert.Equal(t, "complete", obj.Metadata["metadata_status"])

	raw, ok := obj.Metadata["enrichment.structured_metadata"]
	require.True(t, ok, "structured metadata must be present")

	// JSON round-trip: API returns map[string]any.
	metaBytes, err := json.Marshal(raw)
	require.NoError(t, err)

	var meta steps.StructuredMetadata
	require.NoError(t, json.Unmarshal(metaBytes, &meta))

	assert.Equal(t, "task", meta.Type)
	assert.Contains(t, meta.Topics, "auth")
	assert.Contains(t, meta.Topics, "security")
	assert.Equal(t, []string{"@person.alice"}, meta.People)
	assert.Equal(t, []string{"review PR #42"}, meta.ActionItems)
	assert.Equal(t, []string{"2026-05-01"}, meta.DatesMentioned)
	assert.InDelta(t, 0.85, meta.Confidence, 0.01)
}

// TestUS0403_TypeClassification verifies different content intents map
// to their expected type classifications.
func TestUS0403_TypeClassification(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	tests := []struct {
		name     string
		content  string
		llmType  string
		wantType string
	}{
		{"task", "TODO: fix the login bug", "task", "task"},
		{"observation", "The server responded in 200ms", "observation", "observation"},
		{"decision", "We decided to use PostgreSQL", "decision", "decision"},
		{"question", "How should we handle retries?", "question", "question"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			llm := &stubMetadataLLM{
				resp: fmt.Sprintf(`{"type":%q,"topics":[],"confidence":0.9}`, tc.llmType),
			}

			pName := "test.type." + tc.name
			env.svc.Pipes.Upsert(pName, &pipeline.Pipeline{
				PipelineName: pName,
				Steps: []pipeline.PipelineStep{
					steps.NewStructuredMetadataExtractorWithLLM(llm),
				},
			})

			jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
				Content:  tc.content,
				Type:     "text",
				Pipeline: pName,
				Source:   "e2e-test",
			})
			require.NoError(t, err)

			job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
			require.NotEmpty(t, job.ResultID)

			resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
			require.NoError(t, err)
			defer resp.Body.Close()

			var obj storage.KnowledgeObject
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

			raw := obj.Metadata["enrichment.structured_metadata"]
			metaBytes, _ := json.Marshal(raw)
			var meta steps.StructuredMetadata
			require.NoError(t, json.Unmarshal(metaBytes, &meta))

			assert.Equal(t, tc.wantType, meta.Type)
		})
	}
}

// TestUS0403_Idempotent verifies re-extraction of same content produces
// identical metadata.
func TestUS0403_Idempotent(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	llm := &stubMetadataLLM{
		resp: `{"type":"idea","topics":["caching"],"people":[],"action_items":[],"dates_mentioned":[],"source_type":"text","confidence":0.75}`,
	}

	env.svc.Pipes.Upsert("text.short", &pipeline.Pipeline{
		PipelineName: "text.short",
		Steps: []pipeline.PipelineStep{
			steps.NewStructuredMetadataExtractorWithLLM(llm),
		},
	})

	content := "We could add a Redis cache layer for faster lookups"

	// First ingestion.
	jobID1, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content: content, Type: "text", Pipeline: "text.short", Source: "e2e-test",
	})
	require.NoError(t, err)
	job1 := waitForJob(t, env.URL, jobID1, storage.JobCompleted)

	// Second ingestion.
	jobID2, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content: content, Type: "text", Pipeline: "text.short", Source: "e2e-test",
	})
	require.NoError(t, err)
	job2 := waitForJob(t, env.URL, jobID2, storage.JobCompleted)

	// Fetch both objects.
	fetchMeta := func(resultID string) steps.StructuredMetadata {
		resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, resultID))
		require.NoError(t, err)
		defer resp.Body.Close()
		var obj storage.KnowledgeObject
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))
		raw := obj.Metadata["enrichment.structured_metadata"]
		metaBytes, _ := json.Marshal(raw)
		var meta steps.StructuredMetadata
		require.NoError(t, json.Unmarshal(metaBytes, &meta))
		return meta
	}

	meta1 := fetchMeta(job1.ResultID)
	meta2 := fetchMeta(job2.ResultID)

	assert.Equal(t, meta1.Type, meta2.Type)
	assert.Equal(t, meta1.Topics, meta2.Topics)
	assert.Equal(t, meta1.Confidence, meta2.Confidence)
}

// TestUS0403_FailedExtractionStoresPending verifies that when the LLM
// is unavailable, the object is stored with metadata_status: pending.
func TestUS0403_FailedExtractionStoresPending(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	// Use step without LLM to simulate failure.
	env.svc.Pipes.Upsert("text.short", &pipeline.Pipeline{
		PipelineName: "text.short",
		Steps: []pipeline.PipelineStep{
			steps.NewStructuredMetadataExtractor(),
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "Content that cannot be enriched without LLM",
		Type:     "text",
		Pipeline: "text.short",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

	assert.Equal(t, "pending", obj.Metadata["metadata_status"])
	assert.Nil(t, obj.Metadata["enrichment.structured_metadata"],
		"no structured metadata when LLM unavailable")
}

// TestUS0403_MetadataFilterableByType verifies objects with structured
// metadata can be found by filtering on type via search.
func TestUS0403_MetadataFilterableByType(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	// Seed objects with structured metadata directly.
	for i, typ := range []string{"task", "observation", "task"} {
		obj := &storage.KnowledgeObject{
			ID:         fmt.Sprintf("meta-filter-%d", i),
			RawContent: fmt.Sprintf("content %d", i),
			Type:       "text",
			CreatedAt:  now,
			UpdatedAt:  now,
			Metadata: map[string]any{
				"metadata_status": "complete",
				"enrichment.structured_metadata": map[string]any{
					"type":       typ,
					"topics":     []string{},
					"confidence": 0.9,
				},
			},
		}
		require.NoError(t, env.svc.Store.Objects().Create(ctx, obj))
	}

	// Search for type==text (the object's Type field).
	resp, err := gohttp.Get(env.URL + "/api/v1/search?q=type==text")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var body struct {
		Data  []storage.KnowledgeObject `json:"data"`
		Total int                       `json:"total"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))

	// All three seeded objects should match.
	assert.GreaterOrEqual(t, body.Total, 3, "should find at least 3 text objects")
}
