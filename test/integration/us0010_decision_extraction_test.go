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
// Mock pipeline steps for decision extraction tests.
// ---------------------------------------------------------------------------

// decisionExtractionStep simulates extracting decisions with impact/status from content.
type decisionExtractionStep struct {
	pipeline.BaseContract
	decisions []storage.Decision
}

func (s *decisionExtractionStep) Name() string { return "test-decision-extraction" }
func (s *decisionExtractionStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	draft.Decisions = append(draft.Decisions, s.decisions...)
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Metadata["decisions_extracted"] = float64(len(s.decisions))
	return draft, nil
}

// validDecisionImpacts is the allowed set per US-0010 acceptance criteria.
var validDecisionImpacts = map[string]bool{
	"LOW": true, "MEDIUM": true, "HIGH": true,
}

// validDecisionStatuses is the allowed set per US-0010 acceptance criteria.
var validDecisionStatuses = map[string]bool{
	"OPEN": true, "RESOLVED": true, "SUPERSEDED": true,
}

// ---------------------------------------------------------------------------
// US-0010 Tests
// ---------------------------------------------------------------------------

// TestUS0010_DecisionObjectsExtractedWithCorrectFields verifies decision objects
// contain decision text, impact, and status.
func TestUS0010_DecisionObjectsExtractedWithCorrectFields(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	wantDecisions := []storage.Decision{
		{Title: "Use PostgreSQL as the primary datastore", Impact: "HIGH", Status: "OPEN"},
		{Title: "Adopt micro-frontend architecture", Impact: "MEDIUM", Status: "RESOLVED"},
	}

	env.svc.Pipes.Upsert("text.decision-extract", &pipeline.Pipeline{
		PipelineName: "text.decision-extract",
		Steps: []pipeline.PipelineStep{
			&decisionExtractionStep{decisions: wantDecisions},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "We decided to use PostgreSQL as the primary datastore (HIGH impact). We resolved to adopt micro-frontend architecture.",
		Type:     "text",
		Pipeline: "text.decision-extract",
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

	require.Len(t, obj.Decisions, 2, "should have 2 extracted decisions")
	assert.Equal(t, "Use PostgreSQL as the primary datastore", obj.Decisions[0].Title)
	assert.Equal(t, "HIGH", obj.Decisions[0].Impact)
	assert.Equal(t, "OPEN", obj.Decisions[0].Status)
	assert.Equal(t, "MEDIUM", obj.Decisions[1].Impact)
	assert.Equal(t, "RESOLVED", obj.Decisions[1].Status)
}

// TestUS0010_DecisionImpactConstrainedToValidEnums verifies all extracted decision
// impact values are within the allowed enum set: LOW, MEDIUM, HIGH.
func TestUS0010_DecisionImpactConstrainedToValidEnums(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	decisions := []storage.Decision{
		{Title: "Low-impact config change", Impact: "LOW", Status: "OPEN"},
		{Title: "Medium-impact refactor", Impact: "MEDIUM", Status: "OPEN"},
		{Title: "High-impact architecture change", Impact: "HIGH", Status: "OPEN"},
	}

	env.svc.Pipes.Upsert("text.decision-impact", &pipeline.Pipeline{
		PipelineName: "text.decision-impact",
		Steps: []pipeline.PipelineStep{
			&decisionExtractionStep{decisions: decisions},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "Various decisions with different impact levels",
		Type:     "text",
		Pipeline: "text.decision-impact",
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

	for _, d := range obj.Decisions {
		assert.True(t, validDecisionImpacts[d.Impact],
			"impact %q must be one of LOW, MEDIUM, HIGH", d.Impact)
	}
}

// TestUS0010_DecisionStatusConstrainedToValidEnums verifies all extracted decision
// status values are within the allowed enum set: OPEN, RESOLVED, SUPERSEDED.
func TestUS0010_DecisionStatusConstrainedToValidEnums(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	decisions := []storage.Decision{
		{Title: "Open decision", Impact: "HIGH", Status: "OPEN"},
		{Title: "Resolved decision", Impact: "LOW", Status: "RESOLVED"},
		{Title: "Superseded decision", Impact: "MEDIUM", Status: "SUPERSEDED"},
	}

	env.svc.Pipes.Upsert("text.decision-status", &pipeline.Pipeline{
		PipelineName: "text.decision-status",
		Steps: []pipeline.PipelineStep{
			&decisionExtractionStep{decisions: decisions},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "Various decisions with different statuses",
		Type:     "text",
		Pipeline: "text.decision-status",
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

	for _, d := range obj.Decisions {
		assert.True(t, validDecisionStatuses[d.Status],
			"status %q must be one of OPEN, RESOLVED, SUPERSEDED", d.Status)
	}
}

// TestUS0010_NoDecisionsReturnsEmptySlice verifies content with no decisions
// produces an empty Decisions slice (not an error).
func TestUS0010_NoDecisionsReturnsEmptySlice(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("text.decision-empty", &pipeline.Pipeline{
		PipelineName: "text.decision-empty",
		Steps: []pipeline.PipelineStep{
			&decisionExtractionStep{decisions: []storage.Decision{}},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "Plain content with no decisions or action items",
		Type:     "text",
		Pipeline: "text.decision-empty",
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

	// Empty slice is fine; nil is also acceptable.
	assert.Empty(t, obj.Decisions, "no decisions in content should produce empty slice")
}

// TestUS0010_DecisionCountStoredInMetadata verifies decisions_extracted count is recorded.
func TestUS0010_DecisionCountStoredInMetadata(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	decisions := []storage.Decision{
		{Title: "Use Redis for caching", Impact: "MEDIUM", Status: "OPEN"},
	}

	env.svc.Pipes.Upsert("text.decision-count", &pipeline.Pipeline{
		PipelineName: "text.decision-count",
		Steps: []pipeline.PipelineStep{
			&decisionExtractionStep{decisions: decisions},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "We decided to use Redis for caching",
		Type:     "text",
		Pipeline: "text.decision-count",
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

	assert.Equal(t, float64(1), obj.Metadata["decisions_extracted"],
		"decisions_extracted should equal the number of extracted decisions")
}
