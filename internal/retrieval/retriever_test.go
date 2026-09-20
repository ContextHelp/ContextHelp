package retrieval

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
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

	err := wf.ragRetrieve(context.Background(), state, &state.CategoryHits, "category", 10, storage.ObjectFilter{}, nil)
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
	err := wf.ragRetrieve(context.Background(), state, &state.CategoryHits, "category", 2, storage.ObjectFilter{}, nil)

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
	err := wf.ragRetrieve(context.Background(), state, &state.ItemHits, "item", 10, storage.ObjectFilter{}, nil)

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

	require.NoError(t, wf.retrieveCategories(context.Background(), state, storage.ObjectFilter{}, nil))
	require.NoError(t, wf.retrieveItems(context.Background(), state, storage.ObjectFilter{}, nil))
	require.NoError(t, wf.retrieveResources(context.Background(), state, storage.ObjectFilter{}, nil))

	assert.Len(t, state.CategoryHits, 1)
	assert.Len(t, state.ItemHits, 1)
	assert.Len(t, state.ResourceHits, 1)
}

// nodeAwareMockObjects wraps mockObjectStore2 and overrides VectorSearchNodeAware
// to verify the NodeAwareFilter is passed through correctly.
type nodeAwareMockObjects struct {
	*mockObjectStore2
	nodeAwareResults []*storage.KnowledgeObject
	capturedFilter   *pluginapi.NodeAwareFilter
}

func (m *nodeAwareMockObjects) VectorSearchNodeAware(
	_ context.Context,
	_ []float32,
	f storage.ObjectFilter,
	naf pluginapi.NodeAwareFilter,
) ([]*pluginapi.NodeAwareResult, error) {
	m.capturedFilter = &naf
	var out []*pluginapi.NodeAwareResult
	for _, o := range m.nodeAwareResults {
		if f.Type != "" && o.Type != f.Type {
			continue
		}
		out = append(out, &pluginapi.NodeAwareResult{Object: o})
	}
	return out, nil
}

// naStore is a minimal StorageDriver that surfaces nodeAwareMockObjects.
type naStore struct {
	nodeObjs *nodeAwareMockObjects
}

func newNodeAwareMockStore(all, nodeAware []*storage.KnowledgeObject) *naStore {
	base := &mockObjectStore2{objs: all}
	na := &nodeAwareMockObjects{
		mockObjectStore2: base,
		nodeAwareResults: nodeAware,
	}
	return &naStore{nodeObjs: na}
}

// Forward all StorageDriver methods from mockStore but return our nodeObjs.
func (n *naStore) Init(_ context.Context) error               { return nil }
func (n *naStore) Close(_ context.Context) error              { return nil }
func (n *naStore) Objects() storage.ObjectStore               { return n.nodeObjs }
func (n *naStore) Entities() storage.EntityStore              { return nil }
func (n *naStore) Edges() storage.EdgeStore                   { return nil }
func (n *naStore) Jobs() storage.JobStore                     { return nil }
func (n *naStore) Pipelines() storage.PipelineStore           { return nil }
func (n *naStore) Steps() storage.StepStore                   { return nil }
func (n *naStore) Registries() storage.RegistryStore          { return nil }
func (n *naStore) Reminders() storage.ReminderStore           { return nil }
func (n *naStore) Feeds() storage.FeedStore                   { return nil }
func (n *naStore) FeedItems() storage.FeedItemStore           { return nil }
func (n *naStore) Batches() storage.BatchStore                { return nil }
func (n *naStore) Detectors() storage.DetectorStore           { return nil }
func (n *naStore) Blobs() storage.BlobStore                   { return nil }
func (n *naStore) Proximity() storage.ProximityStore          { return nil }
func (n *naStore) Watches() storage.WatchStore                { return nil }
func (n *naStore) Aliases() storage.AliasStore                { return nil }
func (n *naStore) AuditLog() storage.AuditStore               { return nil }
func (n *naStore) Attachments() storage.AttachmentStore       { return nil }
func (n *naStore) Resurfacing() storage.ResurfacingQueueStore { return nil }
func (n *naStore) Entitlements() storage.EntitlementStore     { return nil }
func (n *naStore) Metering() storage.MeteringStore            { return nil }
func (n *naStore) Vectors() storage.VectorStore               { return nil }
func (n *naStore) SavedSearches() storage.SavedSearchStore    { return nil }
func (n *naStore) SearchHistory() storage.SearchHistoryStore  { return nil }
func (n *naStore) Watermarks() storage.WatermarkStore         { return nil }
func (n *naStore) Health(_ context.Context) error             { return nil }

// TestRagRetrieve_NodeAwareFilterRoutesThroughNodeAwarePath verifies that when a
// NodeAwareFilter with NodeTypes is set, VectorSearchNodeAware is invoked and the
// filter is passed through. Only the node-aware result set is returned.
func TestRagRetrieve_NodeAwareFilterRoutesThroughNodeAwarePath(t *testing.T) {
	all := []*storage.KnowledgeObject{
		makeObj("item-1", "item", "Item 1"),
		makeObj("item-2", "item", "Item 2"),
	}
	// Only item-1 has a matching node type in the node-aware set.
	nodeAware := []*storage.KnowledgeObject{makeObj("item-1", "item", "Item 1")}

	store := newNodeAwareMockStore(all, nodeAware)
	cfg := DefaultConfig()
	cfg.EnableSufficiencyCheck = false
	wf := NewWorkflow(cfg, store, nil, &mockEmbedding{})

	nf := &pluginapi.NodeAwareFilter{NodeTypes: []string{"question"}}
	state := &State{ActiveQuery: "open questions?", NeedsRetrieval: true}

	err := wf.ragRetrieve(context.Background(), state, &state.ItemHits, "item", 10, storage.ObjectFilter{}, nf)
	require.NoError(t, err)

	// Only the node-aware filtered result is returned.
	require.Len(t, state.ItemHits, 1)
	assert.Equal(t, "item-1", state.ItemHits[0].ID)

	// Filter was passed to the store.
	require.NotNil(t, store.nodeObjs.capturedFilter)
	assert.Equal(t, []string{"question"}, store.nodeObjs.capturedFilter.NodeTypes)
}

// TestRetrieve_NodeAwareFilterThreadedThroughWorkflow verifies that passing a
// NodeAwareFilter to Retrieve causes it to be threaded through all tier calls.
func TestRetrieve_NodeAwareFilterThreadedThroughWorkflow(t *testing.T) {
	all := []*storage.KnowledgeObject{
		makeObj("cat-1", "category", "Cat"),
	}
	// node-aware path returns same object.
	nodeAware := []*storage.KnowledgeObject{makeObj("cat-1", "category", "Cat")}
	store := newNodeAwareMockStore(all, nodeAware)

	cfg := DefaultConfig()
	cfg.EnableSufficiencyCheck = false
	wf := NewWorkflow(cfg, store, nil, &mockEmbedding{})

	nf := &pluginapi.NodeAwareFilter{NodeTypes: []string{"section"}}
	result, err := wf.Retrieve(context.Background(), "query", nil, storage.ObjectFilter{}, nf)

	require.NoError(t, err)
	assert.NotEmpty(t, result.Categories)
	// Verify the node-aware store received the filter.
	require.NotNil(t, store.nodeObjs.capturedFilter)
	assert.Equal(t, []string{"section"}, store.nodeObjs.capturedFilter.NodeTypes)
}
