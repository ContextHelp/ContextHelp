package service

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	psteps "github.com/ideacrafterslabs/ctxt/internal/pipeline/steps"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockObjectStoreForSchema struct {
	storage.ObjectStore
	objects []*storage.KnowledgeObject
}

func (m *mockObjectStoreForSchema) List(
	_ context.Context, _ storage.ObjectFilter,
) ([]*storage.KnowledgeObject, int, error) {
	return m.objects, len(m.objects), nil
}

type mockDriverForSchema struct {
	storage.StorageDriver
	objStore *mockObjectStoreForSchema
}

func (m *mockDriverForSchema) Objects() storage.ObjectStore {
	return m.objStore
}

func TestSuggestSchemaEvolution(t *testing.T) {
	objects := []*storage.KnowledgeObject{
		{
			ID: "1",
			Metadata: map[string]any{
				"enrichment.structured_metadata": &psteps.StructuredMetadata{
					Type:   "task",
					Topics: []string{"deploy", "ci"},
				},
			},
		},
		{
			ID: "2",
			Metadata: map[string]any{
				"enrichment.structured_metadata": &psteps.StructuredMetadata{
					Type:   "task",
					Topics: []string{"deploy", "security"},
				},
			},
		},
		{
			ID: "3",
			Metadata: map[string]any{
				"enrichment.structured_metadata": &psteps.StructuredMetadata{
					Type:   "incident",
					Topics: []string{"deploy"},
				},
			},
		},
	}

	svc := &Service{
		Store: &mockDriverForSchema{
			objStore: &mockObjectStoreForSchema{objects: objects},
		},
	}

	// Profile already has "task" type and "deploy" topic.
	schema := config.ProfileSchema{
		EntityTypes:     []string{"task"},
		TopicVocabulary: []string{"deploy"},
	}

	result, err := svc.SuggestSchemaEvolution(
		context.Background(), "ops", schema)
	require.NoError(t, err)
	assert.Equal(t, "ops", result.Profile)

	// "incident" seen 1 time — below minCount(2), should not appear.
	assert.Empty(t, result.NewTypes)

	// "ci" seen 1 time — below minCount.
	// "security" seen 1 time — below minCount.
	assert.Empty(t, result.NewTopics)
}

func TestSuggestSchemaEvolutionFrequent(t *testing.T) {
	objects := []*storage.KnowledgeObject{
		{
			ID: "1",
			Metadata: map[string]any{
				"enrichment.structured_metadata": &psteps.StructuredMetadata{
					Type:   "incident",
					Topics: []string{"security"},
				},
			},
		},
		{
			ID: "2",
			Metadata: map[string]any{
				"enrichment.structured_metadata": &psteps.StructuredMetadata{
					Type:   "incident",
					Topics: []string{"security", "auth"},
				},
			},
		},
		{
			ID: "3",
			Metadata: map[string]any{
				"enrichment.structured_metadata": &psteps.StructuredMetadata{
					Type:   "decision",
					Topics: []string{"auth"},
				},
			},
		},
	}

	svc := &Service{
		Store: &mockDriverForSchema{
			objStore: &mockObjectStoreForSchema{objects: objects},
		},
	}

	schema := config.ProfileSchema{
		EntityTypes:     []string{"task"},
		TopicVocabulary: []string{"deploy"},
	}

	result, err := svc.SuggestSchemaEvolution(
		context.Background(), "dev", schema)
	require.NoError(t, err)

	// "incident" seen 2x → suggested.
	require.Len(t, result.NewTypes, 1)
	assert.Equal(t, "incident", result.NewTypes[0].Value)
	assert.Equal(t, 2, result.NewTypes[0].Count)

	// "security" seen 2x, "auth" seen 2x → both suggested.
	require.Len(t, result.NewTopics, 2)
	// Sorted by count desc then alpha.
	assert.Equal(t, "auth", result.NewTopics[0].Value)
	assert.Equal(t, "security", result.NewTopics[1].Value)
}

func TestSuggestSchemaEvolutionMapMetadata(t *testing.T) {
	// Metadata stored as map[string]any (after DB round-trip).
	objects := []*storage.KnowledgeObject{
		{
			ID: "1",
			Metadata: map[string]any{
				"enrichment.structured_metadata": map[string]any{
					"type":   "bug",
					"topics": []any{"infra", "ci"},
				},
			},
		},
		{
			ID: "2",
			Metadata: map[string]any{
				"enrichment.structured_metadata": map[string]any{
					"type":   "bug",
					"topics": []any{"infra"},
				},
			},
		},
	}

	svc := &Service{
		Store: &mockDriverForSchema{
			objStore: &mockObjectStoreForSchema{objects: objects},
		},
	}

	result, err := svc.SuggestSchemaEvolution(
		context.Background(), "ci", config.ProfileSchema{})
	require.NoError(t, err)

	require.Len(t, result.NewTypes, 1)
	assert.Equal(t, "bug", result.NewTypes[0].Value)

	require.Len(t, result.NewTopics, 1)
	assert.Equal(t, "infra", result.NewTopics[0].Value)
}

func TestSuggestSchemaEvolutionEmpty(t *testing.T) {
	svc := &Service{
		Store: &mockDriverForSchema{
			objStore: &mockObjectStoreForSchema{objects: nil},
		},
	}

	result, err := svc.SuggestSchemaEvolution(
		context.Background(), "empty", config.ProfileSchema{})
	require.NoError(t, err)
	assert.Empty(t, result.NewTypes)
	assert.Empty(t, result.NewTopics)
}
