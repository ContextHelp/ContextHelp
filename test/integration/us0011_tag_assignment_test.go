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
// Mock pipeline steps for tag assignment tests.
// ---------------------------------------------------------------------------

// vocabTagAssignmentStep simulates constrained tag assignment from a vocabulary.
type vocabTagAssignmentStep struct {
	pipeline.BaseContract
	vocabulary  []string // allowed tag set
	assignedTags []string // subset of vocabulary to assign
}

func (s *vocabTagAssignmentStep) Name() string { return "test-vocab-tag-assignment" }
func (s *vocabTagAssignmentStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	vocabSet := make(map[string]bool, len(s.vocabulary))
	for _, v := range s.vocabulary {
		vocabSet[v] = true
	}
	for _, label := range s.assignedTags {
		if vocabSet[label] {
			draft.Tags = append(draft.Tags, storage.Tag{Label: label, Source: "vocab-constrained"})
		}
	}
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Metadata["vocabulary_used"] = "default"
	draft.Metadata["tags_assigned"] = float64(len(draft.Tags))
	return draft, nil
}

// ---------------------------------------------------------------------------
// US-0011 Tests
// ---------------------------------------------------------------------------

// TestUS0011_TagsAssignedFromVocabulary verifies tags are assigned from the configured vocabulary.
func TestUS0011_TagsAssignedFromVocabulary(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	vocabulary := []string{"architecture", "database", "decision", "frontend", "backend", "api"}
	assignedTags := []string{"architecture", "database", "decision"}

	env.svc.Pipes.Upsert("text.tag-assign", &pipeline.Pipeline{
		PipelineName: "text.tag-assign",
		Steps: []pipeline.PipelineStep{
			&vocabTagAssignmentStep{
				vocabulary:   vocabulary,
				assignedTags: assignedTags,
			},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "We decided to use PostgreSQL as our architecture decision for the database layer",
		Type:     "text",
		Pipeline: "text.tag-assign",
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

	require.NotEmpty(t, obj.Tags, "tags should be assigned")
	for _, tag := range obj.Tags {
		found := false
		for _, v := range vocabulary {
			if tag.Label == v {
				found = true
				break
			}
		}
		assert.True(t, found, "tag %q must be in the allowed vocabulary", tag.Label)
	}
}

// TestUS0011_NoOutOfVocabularyTagsProduced verifies the step never assigns tags
// outside the configured vocabulary.
func TestUS0011_NoOutOfVocabularyTagsProduced(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	vocabulary := []string{"security", "performance", "testing"}
	// Simulate a model that tries to assign an out-of-vocab tag: only in-vocab ones pass.
	attemptedTags := []string{"security", "performance", "invalid-tag", "testing", "not-in-vocab"}

	env.svc.Pipes.Upsert("text.tag-constrained", &pipeline.Pipeline{
		PipelineName: "text.tag-constrained",
		Steps: []pipeline.PipelineStep{
			&vocabTagAssignmentStep{
				vocabulary:   vocabulary,
				assignedTags: attemptedTags,
			},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "Security testing and performance benchmarks for the new API",
		Type:     "text",
		Pipeline: "text.tag-constrained",
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

	vocabSet := map[string]bool{"security": true, "performance": true, "testing": true}
	for _, tag := range obj.Tags {
		assert.True(t, vocabSet[tag.Label],
			"tag %q is outside the configured vocabulary and must not be assigned", tag.Label)
	}
	// Only the 3 in-vocab tags should survive.
	assert.Len(t, obj.Tags, 3, "exactly 3 in-vocabulary tags should be assigned")
}

// TestUS0011_VocabularyUsedRecordedInMetadata verifies vocabulary_used is persisted on the object.
func TestUS0011_VocabularyUsedRecordedInMetadata(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	vocabulary := []string{"api", "backend"}
	assignedTags := []string{"api"}

	env.svc.Pipes.Upsert("text.tag-meta", &pipeline.Pipeline{
		PipelineName: "text.tag-meta",
		Steps: []pipeline.PipelineStep{
			&vocabTagAssignmentStep{
				vocabulary:   vocabulary,
				assignedTags: assignedTags,
			},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "API design discussion notes",
		Type:     "text",
		Pipeline: "text.tag-meta",
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

	assert.Equal(t, "default", obj.Metadata["vocabulary_used"],
		"vocabulary_used must be recorded in Metadata")
	assert.Equal(t, float64(1), obj.Metadata["tags_assigned"])
}

// TestUS0011_TagsStoredAsJSONArrayOnObject verifies tags are stored as a non-empty JSON array.
func TestUS0011_TagsStoredAsJSONArrayOnObject(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	vocabulary := []string{"architecture", "api", "design"}
	assignedTags := []string{"architecture", "api"}

	env.svc.Pipes.Upsert("text.tag-array", &pipeline.Pipeline{
		PipelineName: "text.tag-array",
		Steps: []pipeline.PipelineStep{
			&vocabTagAssignmentStep{
				vocabulary:   vocabulary,
				assignedTags: assignedTags,
			},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "API architecture review session",
		Type:     "text",
		Pipeline: "text.tag-array",
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

	assert.NotEmpty(t, obj.Tags, "tags must be stored as non-empty array")
	assert.Len(t, obj.Tags, 2)

	labels := make([]string, len(obj.Tags))
	for i, tag := range obj.Tags {
		labels[i] = tag.Label
	}
	assert.Contains(t, labels, "architecture")
	assert.Contains(t, labels, "api")
}
