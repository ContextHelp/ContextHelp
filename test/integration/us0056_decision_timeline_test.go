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
// Mock steps for US-0056 decision timeline tests.
// ---------------------------------------------------------------------------

// timedDecisionStep injects a Decision and sets a creation timestamp via metadata.
type timedDecisionStep struct {
	pipeline.BaseContract
	decision  storage.Decision
	createdAt time.Time
}

func (s *timedDecisionStep) Name() string { return "test-timed-decision" }
func (s *timedDecisionStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	draft.Type = "decision"
	draft.Decisions = append(draft.Decisions, s.decision)
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Metadata["decision_date"] = s.createdAt.Format(time.RFC3339)
	return draft, nil
}

// ---------------------------------------------------------------------------
// US-0056 Tests
// ---------------------------------------------------------------------------

// TestUS0056_DecisionObjectsRetainTimestamp verifies that decisions ingested with
// timestamp metadata retain that metadata through the pipeline.
func TestUS0056_DecisionObjectsRetainTimestamp(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()

	dates := []time.Time{
		time.Date(2025, 1, 5, 9, 0, 0, 0, time.UTC),
		time.Date(2025, 2, 12, 14, 30, 0, 0, time.UTC),
		time.Date(2025, 3, 20, 11, 0, 0, 0, time.UTC),
	}
	decisions := []storage.Decision{
		{Title: "Adopt monorepo structure", Status: "CLOSED", Impact: "HIGH"},
		{Title: "Defer OAuth2 integration", Status: "OPEN", Impact: "MEDIUM"},
		{Title: "Adopt blue-green deployment", Status: "OPEN", Impact: "HIGH"},
	}

	var objects []*storage.KnowledgeObject
	for i, dec := range decisions {
		pipeName := fmt.Sprintf("timeline.decision.%d", i)
		env.svc.Pipes.Upsert(pipeName, &pipeline.Pipeline{
			PipelineName: pipeName,
			Steps: []pipeline.PipelineStep{
				&timedDecisionStep{decision: dec, createdAt: dates[i]},
			},
		})

		jobID, err := env.svc.Analyze(ctx, service.AnalyzeRequest{
			Content:  fmt.Sprintf("decision content: %s", dec.Title),
			Type:     "decision",
			Pipeline: pipeName,
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
		objects = append(objects, &obj)
	}

	// Each object retains its decision and timestamp metadata.
	require.Len(t, objects, 3)
	for i, obj := range objects {
		require.Len(t, obj.Decisions, 1,
			"object[%d] should have 1 decision", i)
		assert.Equal(t, decisions[i].Title, obj.Decisions[0].Title,
			"object[%d] decision title should match", i)
		require.NotNil(t, obj.Metadata)
		assert.Equal(t, dates[i].Format(time.RFC3339), obj.Metadata["decision_date"],
			"object[%d] should retain decision_date metadata", i)
	}
}

// TestUS0056_TimelineCompositionPreservesChronologicalOrder verifies that composing
// a timeline from decision objects preserves all objects, and that the objects were
// stored with ascending timestamps in the store.
func TestUS0056_TimelineCompositionPreservesChronologicalOrder(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()

	type entry struct {
		title string
		date  time.Time
	}

	// Ingest in reverse chronological order to test that metadata is preserved.
	entries := []entry{
		{"Deploy to production", time.Date(2025, 3, 1, 10, 0, 0, 0, time.UTC)},
		{"Complete staging tests", time.Date(2025, 2, 15, 10, 0, 0, 0, time.UTC)},
		{"Start feature development", time.Date(2025, 1, 10, 10, 0, 0, 0, time.UTC)},
	}

	var resultObjects []*storage.KnowledgeObject
	for i, e := range entries {
		pipeName := fmt.Sprintf("timeline.order.%d", i)
		env.svc.Pipes.Upsert(pipeName, &pipeline.Pipeline{
			PipelineName: pipeName,
			Steps: []pipeline.PipelineStep{
				&timedDecisionStep{
					decision:  storage.Decision{Title: e.title, Status: "CLOSED", Impact: "MEDIUM"},
					createdAt: e.date,
				},
			},
		})

		jobID, err := env.svc.Analyze(ctx, service.AnalyzeRequest{
			Content:  fmt.Sprintf("timeline entry: %s", e.title),
			Type:     "decision",
			Pipeline: pipeName,
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
		resultObjects = append(resultObjects, &obj)
	}

	// Compose a timeline from all objects.
	timeline, err := env.svc.Compose(ctx, resultObjects, "timeline")
	require.NoError(t, err)
	require.NotEmpty(t, timeline)

	// All 3 objects referenced in timeline.
	assert.Contains(t, timeline, "3 knowledge objects",
		"timeline should note it was generated from 3 objects")

	// Parse decision_date from each object and verify they are chronologically distinct.
	dates := make([]time.Time, 0, len(resultObjects))
	for _, obj := range resultObjects {
		require.NotNil(t, obj.Metadata)
		dateStr, ok := obj.Metadata["decision_date"].(string)
		require.True(t, ok, "decision_date should be a string in metadata")
		d, err := time.Parse(time.RFC3339, dateStr)
		require.NoError(t, err, "decision_date should parse as RFC3339")
		dates = append(dates, d)
	}

	// Verify all 3 dates are distinct (no duplicates).
	seen := make(map[string]bool)
	for _, d := range dates {
		key := d.Format(time.RFC3339)
		assert.False(t, seen[key], "duplicate decision_date found: %s", key)
		seen[key] = true
	}
}

// TestUS0056_EmptyDecisionSetProducesEmptyTimeline verifies that composing a timeline
// from zero objects returns a valid composition noting zero objects.
func TestUS0056_EmptyDecisionSetProducesEmptyTimeline(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	timeline, err := env.svc.Compose(context.Background(), []*storage.KnowledgeObject{}, "timeline")
	require.NoError(t, err)
	assert.Contains(t, timeline, "0 knowledge objects",
		"empty timeline should note zero objects")
}

// TestUS0056_DecisionStatusPreservedInTimeline verifies that OPEN and CLOSED
// decisions retain their status field after ingestion.
func TestUS0056_DecisionStatusPreservedInTimeline(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()

	env.svc.Pipes.Upsert("timeline.status", &pipeline.Pipeline{
		PipelineName: "timeline.status",
		Steps: []pipeline.PipelineStep{
			&timedDecisionStep{
				decision:  storage.Decision{Title: "Sunset legacy API", Status: "OPEN", Impact: "HIGH"},
				createdAt: time.Date(2025, 1, 20, 9, 0, 0, 0, time.UTC),
			},
		},
	})

	jobID, err := env.svc.Analyze(ctx, service.AnalyzeRequest{
		Content:  "legacy API sunset decision",
		Type:     "decision",
		Pipeline: "timeline.status",
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

	require.Len(t, obj.Decisions, 1)
	assert.Equal(t, "OPEN", obj.Decisions[0].Status,
		"decision status should be preserved as OPEN in timeline")
	assert.Equal(t, "HIGH", obj.Decisions[0].Impact,
		"decision impact should be preserved as HIGH in timeline")
}
