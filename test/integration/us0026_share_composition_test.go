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

	"github.com/ideacrafterslabs/ctxt/internal/events"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ---------------------------------------------------------------------------
// Mock steps for US-0026 share composition tests.
// ---------------------------------------------------------------------------

// shareableBriefStep creates an object with a summary suitable for a shareable brief.
type shareableBriefStep struct {
	pipeline.BaseContract
	summary string
}

func (s *shareableBriefStep) Name() string { return "test-shareable-brief" }
func (s *shareableBriefStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	draft.Type = "article"
	draft.Summaries = []string{s.summary}
	return draft, nil
}

// ---------------------------------------------------------------------------
// US-0026 Tests
// ---------------------------------------------------------------------------

// TestUS0026_ShareEventRecordedOnBus verifies that a share event can be published
// on the event bus and observed by a subscriber — simulating the share recording pathway.
func TestUS0026_ShareEventRecordedOnBus(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()

	// Register a subscriber for composition share events.
	received := make(chan events.Event, 1)
	env.svc.Bus.Subscribe("composition.shared", func(_ context.Context, e events.Event) error {
		received <- e
		return nil
	})

	env.svc.Pipes.Upsert("share.brief", &pipeline.Pipeline{
		PipelineName: "share.brief",
		Steps: []pipeline.PipelineStep{
			&shareableBriefStep{summary: "Q4 planning brief for sharing with stakeholders."},
		},
	})

	jobID, err := env.svc.Analyze(ctx, service.AnalyzeRequest{
		Content:  "Q4 planning content for sharing",
		Type:     "article",
		Pipeline: "share.brief",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	// Publish a share event to simulate a share operation.
	shareEvt, err := events.NewEvent(
		"e2e-test",
		"composition.shared",
		map[string]string{"object_id": job.ResultID, "permission": "read"},
	)
	require.NoError(t, err)
	require.NoError(t, env.svc.Bus.Publish(ctx, shareEvt))

	// Subscriber should receive the event.
	select {
	case evt := <-received:
		assert.Equal(t, "composition.shared", evt.Type,
			"received event should be composition.shared")
		assert.NotEmpty(t, evt.ID, "event should have a unique ID")
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for share event on bus")
	}

	// The object is retrievable (composition ready for sharing).
	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, gohttp.StatusOK, resp.StatusCode,
		"shared composition object should be accessible via GET")
}

// TestUS0026_CompositionObjectAccessibleAfterIngest verifies that a composed brief
// object is accessible via the objects API immediately after ingestion — a
// prerequisite for any downstream share operation.
func TestUS0026_CompositionObjectAccessibleAfterIngest(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()

	env.svc.Pipes.Upsert("share.accessible", &pipeline.Pipeline{
		PipelineName: "share.accessible",
		Steps: []pipeline.PipelineStep{
			&shareableBriefStep{summary: "Sprint retrospective brief ready for sharing."},
		},
	})

	jobID, err := env.svc.Analyze(ctx, service.AnalyzeRequest{
		Content:  "sprint retro content",
		Type:     "article",
		Pipeline: "share.accessible",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	// Verify accessible via GET.
	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))
	assert.Equal(t, job.ResultID, obj.ID, "returned object ID should match result ID")
	assert.NotEmpty(t, obj.Summaries, "composition object should have a summary")
}

// TestUS0026_MultipleObjectsComposedAndAccessible verifies that composing a brief
// from multiple ingested objects produces a composition whose source IDs are all
// present and retrievable — simulating what a share operation would distribute.
func TestUS0026_MultipleObjectsComposedAndAccessible(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()

	summaries := []string{
		"Backend team completed API redesign.",
		"Frontend team shipped new dashboard.",
		"QA team approved release candidate.",
	}

	var objects []*storage.KnowledgeObject
	for i, summary := range summaries {
		pipeName := fmt.Sprintf("share.multi.%d", i)
		env.svc.Pipes.Upsert(pipeName, &pipeline.Pipeline{
			PipelineName: pipeName,
			Steps: []pipeline.PipelineStep{
				&shareableBriefStep{summary: summary},
			},
		})

		jobID, err := env.svc.Analyze(ctx, service.AnalyzeRequest{
			Content:  summary,
			Type:     "article",
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
		objects = append(objects, &obj)
	}

	// Compose brief with citations — simulates what would be shared.
	result, err := env.svc.ComposeWithCitations(ctx, objects, "brief")
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Len(t, result.SourceIDs, 3, "share composition should reference all 3 source objects")
	assert.False(t, result.GeneratedAt.IsZero(), "share composition should have a generated_at")
	assert.NotEmpty(t, result.Content, "share composition content must not be empty")

	// Each source object ID appears in source_ids (accessible link reference).
	for _, obj := range objects {
		assert.Contains(t, result.SourceIDs, obj.ID,
			"share source IDs should include object %s", obj.ID)
	}
}

// TestUS0026_ShareLinkContainsObjectReference verifies that a composed result
// contains a reference that could serve as a shareable link (object ID in content).
func TestUS0026_ShareLinkContainsObjectReference(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()

	env.svc.Pipes.Upsert("share.link", &pipeline.Pipeline{
		PipelineName: "share.link",
		Steps: []pipeline.PipelineStep{
			&shareableBriefStep{summary: "Cross-team alignment brief for external sharing."},
		},
	})

	jobID, err := env.svc.Analyze(ctx, service.AnalyzeRequest{
		Content:  "cross-team alignment content",
		Type:     "article",
		Pipeline: "share.link",
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

	result, err := env.svc.ComposeWithCitations(ctx, []*storage.KnowledgeObject{&obj}, "brief")
	require.NoError(t, err)

	// The object ID is the shareable reference — it must appear in the output.
	assert.Contains(t, result.Content, obj.ID,
		"shareable composition must embed the source object ID as an accessible reference")

	// GeneratedAt is a valid timestamp (can be communicated as link creation time).
	assert.True(t, result.GeneratedAt.Before(time.Now().Add(time.Second)),
		"generated_at should be a valid past or current timestamp")
}
