package integration

// US-0041: Agent Uses Constrained Enrichment
//
// Tests constrained enrichment — agent-triggered extraction that restricts
// output to a known vocabulary (@namespace.slug entities, allowed_tags set).
//
// The HTTP /enrich endpoint is NOT yet implemented. Tests exercise the service
// layer (pipeline-based enrichment) and document the expected HTTP contract.
//
// Gate: INTEGRATION=1 env var required to run.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	gohttp "net/http"
	"os"
	"regexp"
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
// Mock steps for constrained enrichment tests.
// ---------------------------------------------------------------------------

// constrainedEntityStep simulates constrained entity extraction: only extracts
// mentions matching the @namespace.slug pattern; drops malformed values.
type constrainedEntityStep struct {
	pipeline.BaseContract
	rawMentions []string // input: raw extracted strings
	// Pattern: @[a-z0-9]+\.[a-z0-9-]+
}

var entityPattern = regexp.MustCompile(`^@[a-z0-9]+\.[a-z0-9-]+$`)

func (s *constrainedEntityStep) Name() string { return "test-constrained-entity" }
func (s *constrainedEntityStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	for _, raw := range s.rawMentions {
		if !entityPattern.MatchString(raw) {
			continue // drop: out-of-pattern value
		}
		// Parse @namespace.slug → uri.URI{Namespace: namespace, ID: slug}
		parts := strings.SplitN(strings.TrimPrefix(raw, "@"), ".", 2)
		if len(parts) != 2 {
			continue
		}
		draft.Mentions = append(draft.Mentions, uri.URI{
			Scheme:    "ctxt",
			Namespace: parts[0],
			ID:        parts[1],
		})
	}
	return draft, nil
}

// constrainedTagStep simulates constrained tag extraction: only assigns tags
// from the allowed_tags vocabulary; drops out-of-vocabulary values.
type constrainedTagStep struct {
	pipeline.BaseContract
	rawTags     []string // input: raw candidate tags
	allowedTags map[string]bool
}

func (s *constrainedTagStep) Name() string { return "test-constrained-tags" }
func (s *constrainedTagStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	for _, raw := range s.rawTags {
		if !s.allowedTags[raw] {
			continue // drop: out-of-vocabulary
		}
		draft.Tags = append(draft.Tags, storage.Tag{Label: raw, Weight: 1.0, Source: "constrained"})
	}
	return draft, nil
}

// ---------------------------------------------------------------------------
// US-0041 Tests: service-layer (works today)
// ---------------------------------------------------------------------------

// TestUS0041_ConstrainedEntitiesConformToPattern verifies that the enrichment
// pipeline only stores mentions matching @namespace.slug pattern.
func TestUS0041_ConstrainedEntitiesConformToPattern(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}
	env := startTestEnv(t)
	defer env.stop(t)

	// Raw extraction produces valid + invalid values.
	rawMentions := []string{
		"@person.alice",           // valid
		"@project.backend-api",    // valid
		"Invalid Entity Name",     // invalid — dropped
		"@BAD.UPPERCASE",          // invalid — dropped (uppercase)
		"@concept.event-sourcing", // valid
	}

	pipeName := "constrained.entities.test"
	env.svc.Pipes.Upsert(pipeName, &pipeline.Pipeline{
		PipelineName: pipeName,
		Steps: []pipeline.PipelineStep{
			&constrainedEntityStep{rawMentions: rawMentions},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "Alice worked on the backend-api project, applying event-sourcing.",
		Type:     "text",
		Pipeline: pipeName,
		Source:   "agent-enrich-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	obj, err := env.svc.Store.Objects().Get(context.Background(), job.ResultID)
	require.NoError(t, err)

	assert.Len(t, obj.Mentions, 3, "only 3 of 5 raw mentions pass the @namespace.slug constraint")

	for _, m := range obj.Mentions {
		assert.Equal(t, "ctxt", m.Scheme)
		assert.Regexp(t, `^[a-z0-9]+$`, m.Namespace, "namespace must be lowercase alphanumeric")
		assert.Regexp(t, `^[a-z0-9-]+$`, m.ID, "slug must be lowercase alphanumeric with hyphens")
	}
}

// TestUS0041_ConstrainedTagsAreSubsetOfAllowedList verifies only allowed_tags
// values are stored; out-of-vocabulary tags are dropped.
func TestUS0041_ConstrainedTagsAreSubsetOfAllowedList(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}
	env := startTestEnv(t)
	defer env.stop(t)

	allowedTags := map[string]bool{
		"recommended": true,
		"important":   true,
		"draft":       true,
		"archived":    true,
	}

	// Raw candidates include out-of-vocabulary values.
	rawTags := []string{
		"recommended",    // in vocabulary
		"important",      // in vocabulary
		"hallucinated",   // NOT in vocabulary — dropped
		"random-garbage", // NOT in vocabulary — dropped
	}

	pipeName := "constrained.tags.test"
	env.svc.Pipes.Upsert(pipeName, &pipeline.Pipeline{
		PipelineName: pipeName,
		Steps: []pipeline.PipelineStep{
			&constrainedTagStep{rawTags: rawTags, allowedTags: allowedTags},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "Content with constrained tag extraction",
		Type:     "text",
		Pipeline: pipeName,
		Source:   "agent-enrich-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	obj, err := env.svc.Store.Objects().Get(context.Background(), job.ResultID)
	require.NoError(t, err)

	assert.Len(t, obj.Tags, 2, "only 2 of 4 raw tags pass the vocabulary constraint")

	for _, tag := range obj.Tags {
		assert.True(t, allowedTags[tag.Label],
			"stored tag %q must be in allowed_tags vocabulary", tag.Label)
	}
}

// TestUS0041_EnrichmentResultIsDurable verifies enriched object fields persist
// across multiple GET /objects/{id} calls (no write-on-read mutation).
func TestUS0041_EnrichmentResultIsDurable(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}
	env := startTestEnv(t)
	defer env.stop(t)

	pipeName := "constrained.durable.test"
	env.svc.Pipes.Upsert(pipeName, &pipeline.Pipeline{
		PipelineName: pipeName,
		Steps: []pipeline.PipelineStep{
			&constrainedEntityStep{rawMentions: []string{"@person.bob", "@project.delta"}},
			&constrainedTagStep{
				rawTags:     []string{"recommended"},
				allowedTags: map[string]bool{"recommended": true},
			},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "Bob is leading project delta.",
		Type:     "text",
		Pipeline: pipeName,
		Source:   "agent-enrich-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	ctx := context.Background()

	// First retrieval.
	obj1, err := env.svc.Store.Objects().Get(ctx, job.ResultID)
	require.NoError(t, err)
	require.Len(t, obj1.Mentions, 2)
	require.Len(t, obj1.Tags, 1)

	// Second retrieval — must return identical data.
	obj2, err := env.svc.Store.Objects().Get(ctx, job.ResultID)
	require.NoError(t, err)
	require.Len(t, obj2.Mentions, 2, "mentions must be stable on second read")
	require.Len(t, obj2.Tags, 1, "tags must be stable on second read")

	assert.Equal(t, obj1.Mentions, obj2.Mentions, "mentions must be identical on second read")
	assert.Equal(t, obj1.Tags[0].Label, obj2.Tags[0].Label, "tag label must be identical on second read")
}

// TestUS0041_EnrichmentUpdatesObjectViaHTTP verifies the object's mentions/tags
// are visible via GET /api/v1/objects/{id} after enrichment pipeline runs.
func TestUS0041_EnrichmentUpdatesObjectViaHTTP(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}
	env := startTestEnv(t)
	defer env.stop(t)

	pipeName := "constrained.http.test"
	env.svc.Pipes.Upsert(pipeName, &pipeline.Pipeline{
		PipelineName: pipeName,
		Steps: []pipeline.PipelineStep{
			&constrainedEntityStep{rawMentions: []string{"@person.carol", "@concept.microservices"}},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "Carol presented microservices architecture.",
		Type:     "text",
		Pipeline: pipeName,
		Source:   "agent-enrich-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	resp := doGet(t, fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	defer resp.Body.Close()

	require.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

	assert.Len(t, obj.Mentions, 2,
		"GET /objects/{id} must return object with constrained mentions after enrichment")

	// Validate @namespace.slug format via HTTP response.
	for _, m := range obj.Mentions {
		assert.Equal(t, "ctxt", m.Scheme)
		assert.NotEmpty(t, m.Namespace)
		assert.NotEmpty(t, m.ID)
	}
}

// TestUS0041_PipelineFieldReflectsEnrichmentPipeline verifies the pipeline field
// on the stored object matches the enrichment pipeline used.
func TestUS0041_PipelineFieldReflectsEnrichmentPipeline(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}
	env := startTestEnv(t)
	defer env.stop(t)

	pipeName := "constrained.pipeline.field.test"
	env.svc.Pipes.Upsert(pipeName, &pipeline.Pipeline{
		PipelineName: pipeName,
		Steps: []pipeline.PipelineStep{
			&constrainedEntityStep{rawMentions: []string{"@person.dave"}},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "Dave reviewed the infrastructure changes.",
		Type:     "text",
		Pipeline: pipeName,
		Source:   "agent-enrich-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	obj, err := env.svc.Store.Objects().Get(context.Background(), job.ResultID)
	require.NoError(t, err)

	assert.Equal(t, pipeName, obj.Pipeline,
		"stored object pipeline field must reflect the enrichment pipeline used")
}

// ---------------------------------------------------------------------------
// US-0041 Tests: HTTP endpoint stub (MISSING — documents expected contract)
// ---------------------------------------------------------------------------

// TestUS0041_HTTPEnrichEndpointExists verifies POST /api/v1/enrich is registered.
//
// MISSING ENDPOINT: Will fail until /api/v1/enrich is registered in server.go.
func TestUS0041_HTTPEnrichEndpointExists(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}
	env := startTestEnv(t)
	defer env.stop(t)

	body, _ := json.Marshal(map[string]any{
		"object_id":       "o-placeholder",
		"extraction_type": "entities",
		"constraints": map[string]any{
			"entity_pattern": `@[a-z0-9]+\.[a-z0-9-]+`,
			"allowed_tags":   []string{"recommended", "important"},
		},
		"provider": "openai/gpt-4o-mini",
	})

	resp, err := gohttp.Post(env.URL+"/api/v1/enrich", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	// Expected: 202 Accepted with job_id, object_id, extraction_type.
	// Currently returns 404 — test documents the gap.
	assert.Equal(t, gohttp.StatusAccepted, resp.StatusCode,
		"POST /api/v1/enrich must return 202 Accepted — endpoint not yet implemented")
}

// TestUS0041_HTTPEnrichNonexistentObjectReturns404 verifies POST /enrich with
// unknown object_id returns 404.
//
// MISSING ENDPOINT: Will fail until /api/v1/enrich is implemented.
func TestUS0041_HTTPEnrichNonexistentObjectReturns404(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}
	env := startTestEnv(t)
	defer env.stop(t)

	body, _ := json.Marshal(map[string]any{
		"object_id":       "o-does-not-exist-xyz",
		"extraction_type": "entities",
		"constraints":     map[string]any{"entity_pattern": `@[a-z0-9]+\.[a-z0-9-]+`},
		"provider":        "openai/gpt-4o-mini",
	})

	resp, err := gohttp.Post(env.URL+"/api/v1/enrich", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusNotFound, resp.StatusCode,
		"POST /api/v1/enrich with nonexistent object_id must return 404")
}

// TestUS0041_HTTPEnrichUnsupportedExtractionTypeReturns400 verifies bad extraction_type = 400.
//
// MISSING ENDPOINT: Will fail until /api/v1/enrich is implemented.
func TestUS0041_HTTPEnrichUnsupportedExtractionTypeReturns400(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}
	env := startTestEnv(t)
	defer env.stop(t)

	body, _ := json.Marshal(map[string]any{
		"object_id":       "o-placeholder",
		"extraction_type": "unsupported_type_xyz",
		"constraints":     map[string]any{},
		"provider":        "openai/gpt-4o-mini",
	})

	resp, err := gohttp.Post(env.URL+"/api/v1/enrich", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusBadRequest, resp.StatusCode,
		"POST /api/v1/enrich with unsupported extraction_type must return 400")
}
