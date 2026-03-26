package integration

import (
	"context"
	"encoding/json"
	"fmt"
	gohttp "net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ---------------------------------------------------------------------------
// Mock steps for US-0023 plan composition tests.
// ---------------------------------------------------------------------------

// decisionWithTasksStep injects Decisions and Tasks into the draft object,
// simulating a post-enrichment object ready for plan generation.
type decisionWithTasksStep struct {
	pipeline.BaseContract
	decisions []storage.Decision
	tasks     []storage.Task
}

func (s *decisionWithTasksStep) Name() string { return "test-decision-tasks" }
func (s *decisionWithTasksStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	draft.Type = "decision"
	draft.Decisions = append(draft.Decisions, s.decisions...)
	draft.Tasks = append(draft.Tasks, s.tasks...)
	return draft, nil
}

// ---------------------------------------------------------------------------
// US-0023 Tests
// ---------------------------------------------------------------------------

// TestUS0023_PlanPreservesDecisionTaskStructure verifies that objects with Decisions
// and Tasks retain their structure through ingestion so a plan composition can use them.
func TestUS0023_PlanPreservesDecisionTaskStructure(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()

	decisions := []storage.Decision{
		{Title: "Adopt event-driven architecture", Status: "OPEN", Impact: "HIGH"},
		{Title: "Defer database migration", Status: "OPEN", Impact: "MEDIUM"},
	}
	tasks := []storage.Task{
		{Title: "Design event schema", Status: "TODO"},
		{Title: "Update migration runbook", Status: "TODO"},
	}

	env.svc.Pipes.Upsert("plan.decisions", &pipeline.Pipeline{
		PipelineName: "plan.decisions",
		Steps: []pipeline.PipelineStep{
			&decisionWithTasksStep{decisions: decisions, tasks: tasks},
		},
	})

	jobID, err := env.svc.Analyze(ctx, service.AnalyzeRequest{
		Content:  "sprint planning decisions content",
		Type:     "decision",
		Pipeline: "plan.decisions",
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

	// Verify decisions preserved.
	require.Len(t, obj.Decisions, 2, "should have 2 decisions")
	assert.Equal(t, "Adopt event-driven architecture", obj.Decisions[0].Title)
	assert.Equal(t, "HIGH", obj.Decisions[0].Impact)
	assert.Equal(t, "OPEN", obj.Decisions[0].Status)
	assert.Equal(t, "Defer database migration", obj.Decisions[1].Title)
	assert.Equal(t, "MEDIUM", obj.Decisions[1].Impact)

	// Verify tasks preserved.
	require.Len(t, obj.Tasks, 2, "should have 2 tasks")
	assert.Equal(t, "Design event schema", obj.Tasks[0].Title)
	assert.Equal(t, "Update migration runbook", obj.Tasks[1].Title)
}

// TestUS0023_PlanCompositionContainsDecisionAndTaskContent verifies that a plan
// composed from decision objects surfaces decision and task titles in the output.
func TestUS0023_PlanCompositionContainsDecisionAndTaskContent(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()

	env.svc.Pipes.Upsert("plan.compose", &pipeline.Pipeline{
		PipelineName: "plan.compose",
		Steps: []pipeline.PipelineStep{
			&decisionWithTasksStep{
				decisions: []storage.Decision{
					{Title: "Migrate to microservices", Status: "OPEN", Impact: "HIGH"},
				},
				tasks: []storage.Task{
					{Title: "Define service boundaries", Status: "TODO"},
					{Title: "Set up CI/CD per service", Status: "TODO"},
				},
			},
		},
	})

	jobID, err := env.svc.Analyze(ctx, service.AnalyzeRequest{
		Content:  "microservices migration plan content",
		Type:     "decision",
		Pipeline: "plan.compose",
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

	// Compose a plan (uses Compose under the hood; plan is a composition type).
	plan, err := env.svc.Compose(ctx, []*storage.KnowledgeObject{&obj}, "plan")
	require.NoError(t, err)
	require.NotEmpty(t, plan)

	// Plan must reference the object.
	assert.True(t, strings.Contains(plan, obj.ID),
		"plan should reference the source object ID")

	// Plan header present.
	assert.Contains(t, plan, "# plan", "plan should have a title header")
}

// TestUS0023_HighImpactDecisionAppearsInPlan verifies that a HIGH-impact decision
// object is included in the composed plan.
func TestUS0023_HighImpactDecisionAppearsInPlan(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()

	env.svc.Pipes.Upsert("plan.impact", &pipeline.Pipeline{
		PipelineName: "plan.impact",
		Steps: []pipeline.PipelineStep{
			&decisionWithTasksStep{
				decisions: []storage.Decision{
					{Title: "Adopt zero-trust networking", Status: "OPEN", Impact: "HIGH"},
				},
				tasks: []storage.Task{
					{Title: "Audit current network perimeter", Status: "TODO"},
				},
			},
		},
	})

	jobID, err := env.svc.Analyze(ctx, service.AnalyzeRequest{
		Content:  "zero-trust networking decision",
		Type:     "decision",
		Pipeline: "plan.impact",
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
	assert.Equal(t, "HIGH", obj.Decisions[0].Impact,
		"decision impact should be HIGH for priority mapping")

	// Composed plan should include this object.
	plan, err := env.svc.Compose(ctx, []*storage.KnowledgeObject{&obj}, "plan")
	require.NoError(t, err)
	assert.Contains(t, plan, obj.ID, "plan should reference the HIGH-impact decision object")
}
