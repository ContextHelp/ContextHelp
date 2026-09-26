package retrieval

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// --- test doubles ---

// mockStore implements storage.StorageDriver minimally (only Objects() is used).
type mockStore struct {
	objects *mockObjectStore2
}

func newMockStore(objs ...*storage.KnowledgeObject) *mockStore {
	return &mockStore{objects: &mockObjectStore2{objs: objs}}
}

func (m *mockStore) Init(_ context.Context) error               { return nil }
func (m *mockStore) Close(_ context.Context) error              { return nil }
func (m *mockStore) Objects() storage.ObjectStore               { return m.objects }
func (m *mockStore) Entities() storage.EntityStore              { return nil }
func (m *mockStore) Edges() storage.EdgeStore                   { return nil }
func (m *mockStore) Jobs() storage.JobStore                     { return nil }
func (m *mockStore) Pipelines() storage.PipelineStore           { return nil }
func (m *mockStore) Steps() storage.StepStore                   { return nil }
func (m *mockStore) Registries() storage.RegistryStore          { return nil }
func (m *mockStore) Reminders() storage.ReminderStore           { return nil }
func (m *mockStore) Feeds() storage.FeedStore                   { return nil }
func (m *mockStore) FeedItems() storage.FeedItemStore           { return nil }
func (m *mockStore) Batches() storage.BatchStore                { return nil }
func (m *mockStore) Detectors() storage.DetectorStore           { return nil }
func (m *mockStore) Blobs() storage.BlobStore                   { return nil }
func (m *mockStore) Proximity() storage.ProximityStore          { return nil }
func (m *mockStore) Watches() storage.WatchStore                { return nil }
func (m *mockStore) Aliases() storage.AliasStore                { return nil }
func (m *mockStore) AuditLog() storage.AuditStore               { return nil }
func (m *mockStore) Attachments() storage.AttachmentStore       { return nil }
func (m *mockStore) Resurfacing() storage.ResurfacingQueueStore { return nil }
func (m *mockStore) Entitlements() storage.EntitlementStore     { return nil }
func (m *mockStore) Metering() storage.MeteringStore            { return nil }
func (m *mockStore) Embeddings() storage.EmbeddingStore         { return nil }
func (m *mockStore) SavedSearches() storage.SavedSearchStore    { return nil }
func (m *mockStore) SearchHistory() storage.SearchHistoryStore  { return nil }
func (m *mockStore) Watermarks() storage.WatermarkStore         { return nil }
func (m *mockStore) Health(_ context.Context) error             { return nil }

// mockObjectStore2 is a simple in-memory object store for tests.
type mockObjectStore2 struct {
	objs []*storage.KnowledgeObject
}

func (m *mockObjectStore2) Create(_ context.Context, obj *storage.KnowledgeObject) error {
	m.objs = append(m.objs, obj)
	return nil
}
func (m *mockObjectStore2) Get(_ context.Context, id string) (*storage.KnowledgeObject, error) {
	for _, o := range m.objs {
		if o.ID == id {
			return o, nil
		}
	}
	return nil, nil
}
func (m *mockObjectStore2) GetByContentHash(_ context.Context, _ string) (*storage.KnowledgeObject, error) {
	return nil, nil
}
func (m *mockObjectStore2) GetBySourceKey(_ context.Context, _ string) (*storage.KnowledgeObject, error) {
	return nil, nil
}
func (m *mockObjectStore2) List(_ context.Context, f storage.ObjectFilter) ([]*storage.KnowledgeObject, int, error) {
	var out []*storage.KnowledgeObject
	for _, o := range m.objs {
		if f.Type != "" && o.Type != f.Type {
			continue
		}
		out = append(out, o)
	}
	return out, len(out), nil
}
func (m *mockObjectStore2) Update(_ context.Context, _ *storage.KnowledgeObject) error { return nil }
func (m *mockObjectStore2) Delete(_ context.Context, _ string) error                   { return nil }
func (m *mockObjectStore2) ListBySQL(_ context.Context, _ string, _ []any, _, _ int) ([]*storage.KnowledgeObject, int, error) {
	return nil, 0, nil
}
func (m *mockObjectStore2) Reinforce(_ context.Context, _ string, _ *storage.KnowledgeObject) (string, error) {
	return "", nil
}
func (m *mockObjectStore2) VectorSearch(_ context.Context, _ storage.VectorQuery, f storage.ObjectFilter) ([]*storage.KnowledgeObject, error) {
	var out []*storage.KnowledgeObject
	for _, o := range m.objs {
		if f.Type != "" && o.Type != f.Type {
			continue
		}
		out = append(out, o)
		if f.Limit > 0 && len(out) >= f.Limit {
			break
		}
	}
	return out, nil
}
func (m *mockObjectStore2) FTSSearch(_ context.Context, _ string, f storage.ObjectFilter) ([]*storage.KnowledgeObject, error) {
	return nil, nil
}
func (m *mockObjectStore2) FTSSearchNodeAware(_ context.Context, _ string, _ storage.ObjectFilter, _ pluginapi.NodeAwareFilter) ([]*pluginapi.NodeAwareResult, error) {
	return nil, nil
}
func (m *mockObjectStore2) VectorSearchNodeAware(_ context.Context, _ storage.VectorQuery, _ storage.ObjectFilter, _ pluginapi.NodeAwareFilter) ([]*pluginapi.NodeAwareResult, error) {
	return nil, nil
}
func (m *mockObjectStore2) SetReminder(_ context.Context, _ string, _ time.Time) error {
	return nil
}
func (m *mockObjectStore2) ClearReminder(_ context.Context, _ string) error { return nil }
func (m *mockObjectStore2) ListDueReminders(_ context.Context, _ time.Time) ([]*storage.KnowledgeObject, error) {
	return nil, nil
}
func (m *mockObjectStore2) MarkReminded(_ context.Context, _ string, _ time.Time) error {
	return nil
}
func (m *mockObjectStore2) ListPendingReminders(_ context.Context) ([]*storage.KnowledgeObject, error) {
	return nil, nil
}

// fixedSemantic is a SemanticSource whose default model ("fixed-3", dimension
// 3) embeds every query as {1, 0, 0}. The tier tests only need a query
// vector; the default-model behaviour itself is covered in semantic_test.go
// against recorded Ollama embeddings.
func fixedSemantic() SemanticSource {
	return SemanticSource{
		Models:   staticModels{model: &registry.Model{ModelID: "fixed-3", Dimension: 3}},
		Resolver: fixedResolver{vec: []float32{1, 0, 0}},
	}
}

type staticModels struct{ model *registry.Model }

func (s staticModels) Default(context.Context) (*registry.Model, error) {
	if s.model == nil {
		return nil, registry.ErrNoDefaultModel
	}
	return s.model, nil
}

func (s staticModels) Populating(context.Context, time.Time) ([]registry.Model, error) {
	if s.model == nil {
		return nil, nil
	}
	return []registry.Model{*s.model}, nil
}

type fixedResolver struct{ vec []float32 }

func (r fixedResolver) ForModel(context.Context, registry.Model) (providers.EmbeddingProvider, error) {
	return fixedProvider(r), nil
}

func (r fixedResolver) ForRegistration(context.Context) (providers.EmbeddingProvider, json.RawMessage, error) {
	return fixedProvider(r), nil, nil
}

type fixedProvider struct{ vec []float32 }

func (p fixedProvider) Name() string    { return "fixed" }
func (p fixedProvider) Dimensions() int { return len(p.vec) }
func (p fixedProvider) Embed(context.Context, string) ([]float32, error) {
	return p.vec, nil
}

// helper to create a KnowledgeObject for tests.
func makeObj(id, typ, rawContent string) *storage.KnowledgeObject {
	return &storage.KnowledgeObject{
		ID:         id,
		Type:       typ,
		RawContent: rawContent,
		Summaries:  []string{rawContent},
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
}

// --- workflow tests ---

func TestWorkflow_NoRetrieval_WhenQueryIsSufficient(t *testing.T) {
	// LLM says NO_RETRIEVE at route intention step.
	llm := &stubLLM{
		response: "<decision>NO_RETRIEVE</decision><rewritten_query>hello</rewritten_query>",
	}
	store := newMockStore()
	cfg := DefaultConfig()
	cfg.EnableSufficiencyCheck = true

	wf := NewWorkflow(cfg, store, llm, fixedSemantic())
	result, err := wf.Retrieve(context.Background(), "hello", nil, storage.ObjectFilter{})

	require.NoError(t, err)
	assert.False(t, result.NeedsRetrieval)
	assert.Empty(t, result.Categories)
	assert.Empty(t, result.Items)
	assert.Empty(t, result.Resources)
}

func TestWorkflow_EarlyTermination_AfterCategories(t *testing.T) {
	callCount := 0
	llm := &stubLLMFunc{fn: func(_ context.Context, prompt string) (string, error) {
		callCount++
		// First call: route intention → needs retrieval.
		if callCount == 1 {
			return "<decision>RETRIEVE</decision><rewritten_query>go interfaces</rewritten_query>", nil
		}
		// Second call: sufficiency after categories → sufficient.
		return "<decision>NO_RETRIEVE</decision><rewritten_query>go interfaces</rewritten_query>", nil
	}}

	store := newMockStore(
		makeObj("cat-1", "category", "Go interfaces overview"),
		makeObj("item-1", "item", "Item detail"),
	)
	cfg := DefaultConfig()
	cfg.EnableSufficiencyCheck = true

	wf := NewWorkflow(cfg, store, llm, fixedSemantic())
	result, err := wf.Retrieve(context.Background(), "what are go interfaces?", nil, storage.ObjectFilter{})

	require.NoError(t, err)
	assert.True(t, result.NeedsRetrieval)
	assert.NotEmpty(t, result.Categories)
	// Items should be empty — workflow stopped after categories.
	assert.Empty(t, result.Items)
	assert.Empty(t, result.Resources)
}

func TestWorkflow_ProgressesThroughAllTiers(t *testing.T) {
	callCount := 0
	llm := &stubLLMFunc{fn: func(_ context.Context, _ string) (string, error) {
		callCount++
		// Always say RETRIEVE so all tiers are exhausted.
		return "<decision>RETRIEVE</decision><rewritten_query>query</rewritten_query>", nil
	}}

	store := newMockStore(
		makeObj("cat-1", "category", "Category content"),
		makeObj("item-1", "item", "Item content"),
		makeObj("doc-1", "document", "Document content"),
	)
	cfg := DefaultConfig()
	cfg.EnableSufficiencyCheck = true

	wf := NewWorkflow(cfg, store, llm, fixedSemantic())
	result, err := wf.Retrieve(context.Background(), "deep query", nil, storage.ObjectFilter{})

	require.NoError(t, err)
	assert.NotEmpty(t, result.Categories)
	assert.NotEmpty(t, result.Items)
	assert.NotEmpty(t, result.Resources)
}

func TestWorkflow_SufficiencyDisabled_RetrievesAllTiers(t *testing.T) {
	store := newMockStore(
		makeObj("cat-1", "category", "Cat"),
		makeObj("item-1", "item", "Item"),
		makeObj("doc-1", "document", "Doc"),
	)
	cfg := DefaultConfig()
	cfg.EnableSufficiencyCheck = false

	wf := NewWorkflow(cfg, store, nil, fixedSemantic())
	result, err := wf.Retrieve(context.Background(), "any query", nil, storage.ObjectFilter{})

	require.NoError(t, err)
	assert.True(t, result.NeedsRetrieval)
	assert.NotEmpty(t, result.Categories)
	assert.NotEmpty(t, result.Items)
	assert.NotEmpty(t, result.Resources)
}

func TestWorkflow_QueryRewrittenAcrossTiers(t *testing.T) {
	callCount := 0
	llm := &stubLLMFunc{fn: func(_ context.Context, _ string) (string, error) {
		callCount++
		switch callCount {
		case 1: // route intention
			return "<decision>RETRIEVE</decision><rewritten_query>rewritten-v1</rewritten_query>", nil
		case 2: // sufficiency after categories
			return "<decision>RETRIEVE</decision><rewritten_query>rewritten-v2</rewritten_query>", nil
		default:
			return "<decision>NO_RETRIEVE</decision><rewritten_query>rewritten-v2</rewritten_query>", nil
		}
	}}

	store := newMockStore(
		makeObj("cat-1", "category", "Cat"),
		makeObj("item-1", "item", "Item"),
	)
	cfg := DefaultConfig()
	cfg.EnableSufficiencyCheck = true

	wf := NewWorkflow(cfg, store, llm, fixedSemantic())
	result, err := wf.Retrieve(context.Background(), "original", nil, storage.ObjectFilter{})

	require.NoError(t, err)
	assert.Equal(t, "original", result.OriginalQuery)
	assert.Equal(t, "rewritten-v1", result.RewrittenQuery)
	// NextStepQuery should reflect the last rewrite.
	assert.NotEmpty(t, result.NextStepQuery)
}

func TestWorkflow_CategoryTierDisabled(t *testing.T) {
	store := newMockStore(
		makeObj("cat-1", "category", "Cat"),
		makeObj("item-1", "item", "Item"),
	)
	cfg := DefaultConfig()
	cfg.EnableSufficiencyCheck = false
	cfg.Categories.Enabled = false

	wf := NewWorkflow(cfg, store, nil, fixedSemantic())
	result, err := wf.Retrieve(context.Background(), "query", nil, storage.ObjectFilter{})

	require.NoError(t, err)
	assert.Empty(t, result.Categories)
	assert.NotEmpty(t, result.Items)
}

// stubLLMFunc allows a function to be used as an LLM provider.
type stubLLMFunc struct {
	fn func(context.Context, string) (string, error)
}

func (s *stubLLMFunc) Generate(ctx context.Context, prompt string) (string, error) {
	return s.fn(ctx, prompt)
}
func (s *stubLLMFunc) Name() string { return "stub-func" }
