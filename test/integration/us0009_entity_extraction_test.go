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

	uri "hop.top/cite/scheme"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ---------------------------------------------------------------------------
// Mock pipeline steps for entity extraction tests.
// ---------------------------------------------------------------------------

// entityExtractionStep simulates extracting @namespace.slug mentions from content.
type entityExtractionStep struct {
	pipeline.BaseContract
	mentions []uri.URI
}

func (s *entityExtractionStep) Name() string { return "test-entity-extraction" }
func (s *entityExtractionStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	draft.Mentions = append(draft.Mentions, s.mentions...)
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Metadata["entities_extracted"] = float64(len(s.mentions))
	return draft, nil
}

// entityObjectCreatorStep simulates creating canonical entity objects for each mention.
type entityObjectCreatorStep struct {
	pipeline.BaseContract
	entityTypes []string // e.g. ["person", "project"]
}

func (s *entityObjectCreatorStep) Name() string { return "test-entity-object-creator" }
func (s *entityObjectCreatorStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Metadata["entity_types"] = s.entityTypes
	return draft, nil
}

// ---------------------------------------------------------------------------
// US-0009 Tests
// ---------------------------------------------------------------------------

// TestUS0009_EntityMentionsExtracted verifies entity extraction produces @namespace.slug Mentions.
func TestUS0009_EntityMentionsExtracted(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	wantMentions := []uri.URI{
		{Scheme: "ctxt", Namespace: "person", ID: "alice-smith"},
		{Scheme: "ctxt", Namespace: "project", ID: "mobile-redesign"},
		{Scheme: "ctxt", Namespace: "system", ID: "backend-api"},
	}

	env.svc.Pipes.Upsert("text.entity-extract", &pipeline.Pipeline{
		PipelineName: "text.entity-extract",
		Steps: []pipeline.PipelineStep{
			&entityExtractionStep{mentions: wantMentions},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "Alice Smith discussed the mobile redesign project with the backend API team",
		Type:     "text",
		Pipeline: "text.entity-extract",
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

	assert.Len(t, obj.Mentions, 3, "should have 3 extracted mentions")
	assert.Equal(t, float64(3), obj.Metadata["entities_extracted"])
}

// TestUS0009_MentionEdgesCreated verifies graph edges are created for each extracted entity.
func TestUS0009_MentionEdgesCreated(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	mentions := []uri.URI{
		{Scheme: "ctxt", Namespace: "person", ID: "bob-jones"},
		{Scheme: "ctxt", Namespace: "concept", ID: "event-sourcing"},
	}

	env.svc.Pipes.Upsert("text.entity-edges", &pipeline.Pipeline{
		PipelineName: "text.entity-edges",
		Steps: []pipeline.PipelineStep{
			&entityExtractionStep{mentions: mentions},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "Bob Jones introduced event sourcing patterns to the team",
		Type:     "text",
		Pipeline: "text.entity-edges",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	edges, err := env.svc.Store.Edges().ListFrom(context.Background(), "object", job.ResultID)
	require.NoError(t, err)
	assert.Len(t, edges, 2, "one edge per extracted entity mention")
}

// TestUS0009_MentionFormatIsNamespaceSlug verifies all mentions follow @namespace.slug format
// (Space = namespace, ID = slug, lowercase hyphenated).
func TestUS0009_MentionFormatIsNamespaceSlug(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	mentions := []uri.URI{
		{Scheme: "ctxt", Namespace: "person", ID: "carol-white"},
		{Scheme: "ctxt", Namespace: "organization", ID: "acme-corp"},
		{Scheme: "ctxt", Namespace: "product", ID: "super-widget"},
	}

	env.svc.Pipes.Upsert("text.entity-format", &pipeline.Pipeline{
		PipelineName: "text.entity-format",
		Steps: []pipeline.PipelineStep{
			&entityExtractionStep{mentions: mentions},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "Carol White at Acme Corp launched Super Widget",
		Type:     "text",
		Pipeline: "text.entity-format",
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

	for _, m := range obj.Mentions {
		assert.Equal(t, "ctxt", m.Scheme, "mention scheme must be ctxt")
		assert.NotEmpty(t, m.Namespace, "mention namespace (Space) must not be empty")
		assert.NotEmpty(t, m.ID, "mention slug (ID) must not be empty")
		// Slug must be lowercase and hyphen-separated.
		assert.Equal(t, strings.ToLower(m.ID), m.ID, "slug must be lowercase")
	}
}

// TestUS0009_EntityObjectsCreatedForMentions verifies entity objects are created
// for each extracted mention (graph connectivity).
func TestUS0009_EntityObjectsCreatedForMentions(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	mentions := []uri.URI{
		{Scheme: "ctxt", Namespace: "person", ID: "dave-kim"},
	}

	env.svc.Pipes.Upsert("text.entity-objects", &pipeline.Pipeline{
		PipelineName: "text.entity-objects",
		Steps: []pipeline.PipelineStep{
			&entityExtractionStep{mentions: mentions},
			&entityObjectCreatorStep{entityTypes: []string{"person"}},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "Dave Kim reviewed the proposal",
		Type:     "text",
		Pipeline: "text.entity-objects",
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

	require.NotNil(t, obj.Metadata)
	entityTypes, ok := obj.Metadata["entity_types"].([]interface{})
	require.True(t, ok, "entity_types metadata must be a slice")
	assert.Len(t, entityTypes, 1, "one entity type recorded")
	assert.Equal(t, "person", entityTypes[0])
}
