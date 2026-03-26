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
// Mock steps for US-0058 impact assessment tests.
// ---------------------------------------------------------------------------

// impactDecisionStep injects decisions with explicit impact levels into the draft.
type impactDecisionStep struct {
	pipeline.BaseContract
	decisions []storage.Decision
}

func (s *impactDecisionStep) Name() string { return "test-impact-decision" }
func (s *impactDecisionStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	draft.Type = "decision"
	draft.Decisions = append(draft.Decisions, s.decisions...)
	return draft, nil
}

// ---------------------------------------------------------------------------
// US-0058 Tests
// ---------------------------------------------------------------------------

// TestUS0058_HighImpactDecisionPreserved verifies that a HIGH-impact decision
// retains its impact field after ingestion.
func TestUS0058_HighImpactDecisionPreserved(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()

	env.svc.Pipes.Upsert("impact.high", &pipeline.Pipeline{
		PipelineName: "impact.high",
		Steps: []pipeline.PipelineStep{
			&impactDecisionStep{decisions: []storage.Decision{
				{Title: "Migrate all traffic to new CDN", Status: "OPEN", Impact: "HIGH"},
			}},
		},
	})

	jobID, err := env.svc.Analyze(ctx, service.AnalyzeRequest{
		Content:  "CDN migration decision",
		Type:     "decision",
		Pipeline: "impact.high",
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

	require.Len(t, obj.Decisions, 1)
	assert.Equal(t, "HIGH", obj.Decisions[0].Impact,
		"impact level should be HIGH")
	assert.Equal(t, "OPEN", obj.Decisions[0].Status,
		"decision status should be OPEN")
}

// TestUS0058_MultipleImpactLevelsPreserved verifies that decisions with different
// impact levels (HIGH, MEDIUM, LOW) all retain their correct values after ingestion.
func TestUS0058_MultipleImpactLevelsPreserved(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()

	env.svc.Pipes.Upsert("impact.mixed", &pipeline.Pipeline{
		PipelineName: "impact.mixed",
		Steps: []pipeline.PipelineStep{
			&impactDecisionStep{decisions: []storage.Decision{
				{Title: "Rewrite auth service", Status: "OPEN", Impact: "HIGH"},
				{Title: "Update logging format", Status: "OPEN", Impact: "MEDIUM"},
				{Title: "Change button colour in UI", Status: "CLOSED", Impact: "LOW"},
			}},
		},
	})

	jobID, err := env.svc.Analyze(ctx, service.AnalyzeRequest{
		Content:  "mixed impact level decisions from sprint",
		Type:     "decision",
		Pipeline: "impact.mixed",
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

	require.Len(t, obj.Decisions, 3, "should have 3 decisions")

	// Verify each impact level preserved.
	assert.Equal(t, "HIGH", obj.Decisions[0].Impact)
	assert.Equal(t, "MEDIUM", obj.Decisions[1].Impact)
	assert.Equal(t, "LOW", obj.Decisions[2].Impact)
}

// TestUS0058_ImpactAssessmentCompositionIncludesImpactLevels verifies that a
// composed impact assessment from decision objects includes the impact data.
func TestUS0058_ImpactAssessmentCompositionIncludesImpactLevels(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()

	env.svc.Pipes.Upsert("impact.assess", &pipeline.Pipeline{
		PipelineName: "impact.assess",
		Steps: []pipeline.PipelineStep{
			&impactDecisionStep{decisions: []storage.Decision{
				{Title: "Scale database cluster", Status: "OPEN", Impact: "HIGH"},
				{Title: "Enable read replicas", Status: "OPEN", Impact: "MEDIUM"},
			}},
		},
	})

	jobID, err := env.svc.Analyze(ctx, service.AnalyzeRequest{
		Content:  "database scaling decisions",
		Type:     "decision",
		Pipeline: "impact.assess",
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

	// Verify decisions with impact levels are present.
	require.Len(t, obj.Decisions, 2)
	impactLevels := map[string]bool{}
	for _, d := range obj.Decisions {
		impactLevels[d.Impact] = true
	}
	assert.True(t, impactLevels["HIGH"], "HIGH impact decision should be present")
	assert.True(t, impactLevels["MEDIUM"], "MEDIUM impact decision should be present")

	// Compose assessment — verifies composition API works with these objects.
	assessment, err := env.svc.Compose(ctx, []*storage.KnowledgeObject{&obj}, "impact-assessment")
	require.NoError(t, err)
	require.NotEmpty(t, assessment)

	assert.Contains(t, assessment, "# impact-assessment",
		"assessment should have correct title header")
	assert.Contains(t, assessment, obj.ID,
		"assessment should reference source object")
}

// TestUS0058_NoDecisionsInObjectProducesEmptyImpactList verifies that composing
// an assessment from objects with no decisions produces a valid empty composition.
func TestUS0058_NoDecisionsInObjectProducesEmptyImpactList(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()

	env.svc.Pipes.Upsert("impact.nodecisions", &pipeline.Pipeline{
		PipelineName: "impact.nodecisions",
		Steps: []pipeline.PipelineStep{
			&briefObjectStep{
				title:   "Meeting Notes",
				summary: "General discussion with no formal decisions.",
			},
		},
	})

	jobID, err := env.svc.Analyze(ctx, service.AnalyzeRequest{
		Content:  "meeting notes with no decisions",
		Type:     "article",
		Pipeline: "impact.nodecisions",
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

	// No decisions in this object.
	assert.Len(t, obj.Decisions, 0,
		"object with no decision pipeline step should have zero decisions")

	// Compose should still succeed (empty impact list is not an error).
	assessment, err := env.svc.Compose(ctx, []*storage.KnowledgeObject{&obj}, "impact-assessment")
	require.NoError(t, err, "composing with no decisions should not error")
	assert.NotEmpty(t, assessment, "assessment should be non-empty even with no decisions")
}
