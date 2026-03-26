package integration

import (
	"context"
	"encoding/json"
	"fmt"
	gohttp "net/http"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/uri"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// slugPattern enforces @namespace.slug format: lowercase alphanumeric and hyphens only.
var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*[a-z0-9]$|^[a-z0-9]$`)

// ---------------------------------------------------------------------------
// Mock pipeline steps for LMQL-constrained extraction tests.
// ---------------------------------------------------------------------------

// lmqlConstrainedEntityStep simulates LMQL token-level constrained entity extraction.
// All produced mentions are guaranteed to conform to @namespace.slug format.
type lmqlConstrainedEntityStep struct {
	pipeline.BaseContract
	mentions []uri.URI
}

func (s *lmqlConstrainedEntityStep) Name() string { return "test-lmql-constrained-entity" }
func (s *lmqlConstrainedEntityStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	// Simulate LMQL hard-constraint: only emit mentions with valid namespace.slug format.
	for _, m := range s.mentions {
		if m.Scheme == "ctxt" && m.Space != "" && slugPattern.MatchString(m.ID) {
			draft.Mentions = append(draft.Mentions, m)
		}
	}
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Metadata["extraction_provider"] = "lmql"
	draft.Metadata["constrained"] = true
	return draft, nil
}

// lmqlConstrainedTagStep simulates LMQL token-level constrained tag assignment.
type lmqlConstrainedTagStep struct {
	pipeline.BaseContract
	vocabulary   []string
	assignedTags []string
}

func (s *lmqlConstrainedTagStep) Name() string { return "test-lmql-constrained-tag" }
func (s *lmqlConstrainedTagStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	vocabSet := make(map[string]bool, len(s.vocabulary))
	for _, v := range s.vocabulary {
		vocabSet[v] = true
	}
	// Simulate LMQL in() constraint: physically cannot emit tags outside vocabulary.
	for _, label := range s.assignedTags {
		if vocabSet[label] {
			draft.Tags = append(draft.Tags, storage.Tag{Label: label, Source: "lmql"})
		}
	}
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Metadata["extraction_provider"] = "lmql"
	draft.Metadata["constrained"] = true
	return draft, nil
}

// ---------------------------------------------------------------------------
// US-0014 Tests
// ---------------------------------------------------------------------------

// TestUS0014_LMQLExtractedEntitiesConformToNamespaceSlugFormat verifies that
// entities extracted via LMQL-constrained pipeline always follow @namespace.slug.
func TestUS0014_LMQLExtractedEntitiesConformToNamespaceSlugFormat(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	// Well-formed mentions — these should all pass through the constraint.
	mentions := []uri.URI{
		{Scheme: "ctxt", Space: "person", ID: "alice-smith"},
		{Scheme: "ctxt", Space: "project", ID: "mobile-redesign"},
		{Scheme: "ctxt", Space: "system", ID: "backend-api"},
	}

	env.svc.Pipes.Upsert("text.lmql-entities", &pipeline.Pipeline{
		PipelineName: "text.lmql-entities",
		Steps: []pipeline.PipelineStep{
			&lmqlConstrainedEntityStep{mentions: mentions},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "Alice Smith is leading the mobile redesign project using the backend API",
		Type:     "text",
		Pipeline: "text.lmql-entities",
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

	require.NotEmpty(t, obj.Mentions, "LMQL extraction must produce mentions")

	for _, m := range obj.Mentions {
		assert.Equal(t, "ctxt", m.Scheme, "mention scheme must be ctxt")
		assert.NotEmpty(t, m.Space, "mention namespace must not be empty")
		assert.True(t, slugPattern.MatchString(m.ID),
			"mention slug %q must conform to @namespace.slug format (lowercase, hyphenated)", m.ID)
	}

	assert.Equal(t, "lmql", obj.Metadata["extraction_provider"])
	assert.Equal(t, true, obj.Metadata["constrained"])
}

// TestUS0014_LMQLConstraintFiltersInvalidSlugs verifies that mentions with invalid slug
// format are rejected by the constraint (not stored on the object).
func TestUS0014_LMQLConstraintFiltersInvalidSlugs(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	// Mix of valid and invalid slugs — only valid should pass through.
	allMentions := []uri.URI{
		{Scheme: "ctxt", Space: "person", ID: "alice-smith"},    // valid
		{Scheme: "ctxt", Space: "person", ID: "Alice Smith"},     // invalid: spaces + caps
		{Scheme: "ctxt", Space: "concept", ID: "event-sourcing"}, // valid
		{Scheme: "ctxt", Space: "concept", ID: "EventSourcing"},  // invalid: camelCase
	}

	env.svc.Pipes.Upsert("text.lmql-filter", &pipeline.Pipeline{
		PipelineName: "text.lmql-filter",
		Steps: []pipeline.PipelineStep{
			&lmqlConstrainedEntityStep{mentions: allMentions},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "Alice Smith applies event sourcing principles",
		Type:     "text",
		Pipeline: "text.lmql-filter",
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

	// Only 2 valid mentions should pass the constraint.
	assert.Len(t, obj.Mentions, 2, "only valid @namespace.slug mentions should pass constraint")
	for _, m := range obj.Mentions {
		assert.True(t, slugPattern.MatchString(m.ID),
			"all stored mentions must have valid slug format")
	}
}

// TestUS0014_LMQLTagConstraintEnforcesVocabulary verifies LMQL in() constraint
// prevents out-of-vocabulary tags from being emitted.
func TestUS0014_LMQLTagConstraintEnforcesVocabulary(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	vocabulary := []string{"architecture", "database", "api", "security"}
	// Attempt to assign some out-of-vocab tags — constraint blocks them.
	attemptedTags := []string{"architecture", "invalid-tag", "database", "not-in-vocab"}

	env.svc.Pipes.Upsert("text.lmql-tags", &pipeline.Pipeline{
		PipelineName: "text.lmql-tags",
		Steps: []pipeline.PipelineStep{
			&lmqlConstrainedTagStep{
				vocabulary:   vocabulary,
				assignedTags: attemptedTags,
			},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "Database architecture review with security considerations",
		Type:     "text",
		Pipeline: "text.lmql-tags",
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

	// Only 2 valid vocabulary tags should survive.
	assert.Len(t, obj.Tags, 2, "LMQL in() constraint must block out-of-vocab tags")

	vocabSet := map[string]bool{"architecture": true, "database": true, "api": true, "security": true}
	for _, tag := range obj.Tags {
		assert.True(t, vocabSet[tag.Label],
			"tag %q must be in vocabulary (LMQL hard constraint)", tag.Label)
		assert.Equal(t, "lmql", tag.Source, "tag source must record lmql provider")
	}
}

// TestUS0014_ExtractionProviderRecordedInMetadata verifies the LMQL provider is
// recorded in Metadata for traceability.
func TestUS0014_ExtractionProviderRecordedInMetadata(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("text.lmql-provider", &pipeline.Pipeline{
		PipelineName: "text.lmql-provider",
		Steps: []pipeline.PipelineStep{
			&lmqlConstrainedEntityStep{mentions: []uri.URI{
				{Scheme: "ctxt", Space: "person", ID: "test-user"},
			}},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "Test user reviewed the document",
		Type:     "text",
		Pipeline: "text.lmql-provider",
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

	assert.Equal(t, "lmql", obj.Metadata["extraction_provider"],
		"extraction_provider must be recorded as lmql")
	assert.Equal(t, true, obj.Metadata["constrained"],
		"constrained flag must be set to true for LMQL extraction")
}
