package integration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	psteps "github.com/ideacrafterslabs/ctxt/internal/pipeline/steps"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
)

// TestUS0409_SchemaEvolution verifies per-profile schema co-evolution.
func TestUS0409_SchemaEvolution(t *testing.T) {
	dir := t.TempDir()
	driver, err := sqlite.New(dir + "/test.db")
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, driver.Init(ctx))
	t.Cleanup(func() { driver.Close(ctx) })

	// -- Schema constrains metadata extraction --

	t.Run("custom_entity_type_constrains_extraction", func(t *testing.T) {
		schema := &config.ProfileSchema{
			EntityTypes:     []string{"incident", "postmortem"},
			TopicVocabulary: []string{"infra", "security"},
		}
		llm := &fakeLLM{
			resp: `{"type":"task","topics":["infra","random","security"],"confidence":0.9}`,
		}
		step := psteps.NewStructuredMetadataExtractorWithSchema(llm, schema)
		draft := &storage.KnowledgeObject{RawContent: "Production DB down"}

		got, err := step.Run(ctx, draft)
		require.NoError(t, err)

		meta := got.Metadata["enrichment.structured_metadata"].(*psteps.StructuredMetadata)
		// "task" not in schema → falls back to "incident" (first).
		assert.Equal(t, "incident", meta.Type)
		// "random" filtered out.
		assert.Equal(t, []string{"infra", "security"}, meta.Topics)
	})

	t.Run("classification_rule_overrides_llm", func(t *testing.T) {
		schema := &config.ProfileSchema{
			EntityTypes: []string{"incident", "postmortem"},
			ClassificationRules: []config.ClassificationRule{
				{Pattern: "(?i)post.?mortem", Type: "postmortem"},
			},
		}
		llm := &fakeLLM{
			resp: `{"type":"incident","topics":[],"confidence":0.8}`,
		}
		step := psteps.NewStructuredMetadataExtractorWithSchema(llm, schema)
		draft := &storage.KnowledgeObject{
			RawContent: "Post-mortem: the root cause was a bad deploy",
		}

		got, err := step.Run(ctx, draft)
		require.NoError(t, err)

		meta := got.Metadata["enrichment.structured_metadata"].(*psteps.StructuredMetadata)
		assert.Equal(t, "postmortem", meta.Type)
	})

	// -- Schema evolution suggests from recent objects --

	t.Run("evolve_suggests_frequent_types_and_topics", func(t *testing.T) {
		// Insert objects with metadata.
		for i := 0; i < 3; i++ {
			obj := &storage.KnowledgeObject{
				ID:         t.Name() + string(rune('a'+i)),
				RawContent: "test",
				Type:       "text",
				Status:     "active",
				Metadata: map[string]any{
					"enrichment.structured_metadata": map[string]any{
						"type":   "bug",
						"topics": []any{"kubernetes", "networking"},
					},
				},
			}
			require.NoError(t, driver.Objects().Create(ctx, obj))
		}

		svc := service.New(driver, nil, nil, nil, "", nil, config.Config{})
		schema := config.ProfileSchema{
			EntityTypes:     []string{"task"},
			TopicVocabulary: []string{"deploy"},
		}

		result, err := svc.SuggestSchemaEvolution(ctx, "ops", schema)
		require.NoError(t, err)

		// "bug" type seen 3x → suggested.
		require.Len(t, result.NewTypes, 1)
		assert.Equal(t, "bug", result.NewTypes[0].Value)
		assert.Equal(t, 3, result.NewTypes[0].Count)

		// "kubernetes" and "networking" seen 3x each → suggested.
		require.Len(t, result.NewTopics, 2)
	})

	// -- Schema CRUD via config --

	t.Run("schema_version_increments", func(t *testing.T) {
		schema := config.ProfileSchema{}

		schema.EntityTypes = append(schema.EntityTypes, "task")
		schema.Version++

		schema.TopicVocabulary = append(schema.TopicVocabulary, "deploy")
		schema.Version++

		schema.ClassificationRules = append(schema.ClassificationRules,
			config.ClassificationRule{Pattern: "test", Type: "task"})
		schema.Version++

		assert.Equal(t, 3, schema.Version)
		assert.Len(t, schema.EntityTypes, 1)
		assert.Len(t, schema.TopicVocabulary, 1)
		assert.Len(t, schema.ClassificationRules, 1)
	})
}

type fakeLLM struct {
	resp string
}

func (f *fakeLLM) Generate(_ context.Context, _ string) (string, error) {
	return f.resp, nil
}

func (f *fakeLLM) Name() string { return "fake" }
