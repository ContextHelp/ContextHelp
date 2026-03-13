package retrieval

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestRagRetrieve_FiltersByType(t *testing.T) {
	store := newMockStore(
		makeObj("cat-1", "category", "Category content"),
		makeObj("item-1", "item", "Item content"),
		makeObj("doc-1", "document", "Document content"),
	)
	cfg := DefaultConfig()
	cfg.EnableSufficiencyCheck = false
	wf := NewWorkflow(cfg, store, nil, &mockEmbedding{})

	state := &State{
		OriginalQuery:  "test",
		RewrittenQuery: "test",
		ActiveQuery:    "test",
		NeedsRetrieval: true,
	}

	err := wf.ragRetrieve(context.Background(), state, &state.CategoryHits, "category", 10, storage.ObjectFilter{})
	require.NoError(t, err)

	assert.Len(t, state.CategoryHits, 1)
	assert.Equal(t, "cat-1", state.CategoryHits[0].ID)
}

func TestRagRetrieve_RespectsTopK(t *testing.T) {
	store := newMockStore(
		makeObj("cat-1", "category", "Cat 1"),
		makeObj("cat-2", "category", "Cat 2"),
		makeObj("cat-3", "category", "Cat 3"),
	)
	cfg := DefaultConfig()
	cfg.EnableSufficiencyCheck = false
	wf := NewWorkflow(cfg, store, nil, &mockEmbedding{})

	state := &State{ActiveQuery: "test", NeedsRetrieval: true}
	err := wf.ragRetrieve(context.Background(), state, &state.CategoryHits, "category", 2, storage.ObjectFilter{})

	require.NoError(t, err)
	assert.LessOrEqual(t, len(state.CategoryHits), 2)
}

func TestRagRetrieve_NoEmbeddingFallsBackToList(t *testing.T) {
	store := newMockStore(
		makeObj("item-1", "item", "Item 1"),
	)
	cfg := DefaultConfig()
	cfg.EnableSufficiencyCheck = false
	// No embedding provider.
	wf := NewWorkflow(cfg, store, nil, nil)

	state := &State{ActiveQuery: "test", NeedsRetrieval: true}
	err := wf.ragRetrieve(context.Background(), state, &state.ItemHits, "item", 10, storage.ObjectFilter{})

	require.NoError(t, err)
	assert.Len(t, state.ItemHits, 1)
}

func TestRetrieveCategories_Items_Resources(t *testing.T) {
	store := newMockStore(
		makeObj("cat-1", "category", "Cat"),
		makeObj("item-1", "item", "Item"),
		makeObj("doc-1", "document", "Doc"),
	)
	cfg := DefaultConfig()
	cfg.EnableSufficiencyCheck = false
	wf := NewWorkflow(cfg, store, nil, &mockEmbedding{})

	state := &State{ActiveQuery: "query", NeedsRetrieval: true, ProceedToItems: true, ProceedToResources: true}

	require.NoError(t, wf.retrieveCategories(context.Background(), state, storage.ObjectFilter{}))
	require.NoError(t, wf.retrieveItems(context.Background(), state, storage.ObjectFilter{}))
	require.NoError(t, wf.retrieveResources(context.Background(), state, storage.ObjectFilter{}))

	assert.Len(t, state.CategoryHits, 1)
	assert.Len(t, state.ItemHits, 1)
	assert.Len(t, state.ResourceHits, 1)
}
