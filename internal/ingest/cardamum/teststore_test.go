package cardamum

import (
	"context"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// testStore is a minimal in-memory ObjectStore for e2e tests.
type testStore struct {
	objects    map[string]*storage.KnowledgeObject
	sourceKeys map[string]*storage.KnowledgeObject
}

func newTestStore() *testStore {
	return &testStore{
		objects:    make(map[string]*storage.KnowledgeObject),
		sourceKeys: make(map[string]*storage.KnowledgeObject),
	}
}

func (m *testStore) Create(_ context.Context, obj *storage.KnowledgeObject) error {
	m.objects[obj.ID] = obj
	if obj.SourceKey != "" {
		m.sourceKeys[obj.SourceKey] = obj
	}
	return nil
}

func (m *testStore) Get(_ context.Context, id string) (*storage.KnowledgeObject, error) {
	return m.objects[id], nil
}

func (m *testStore) GetByContentHash(_ context.Context, _ string) (*storage.KnowledgeObject, error) {
	return nil, nil
}

func (m *testStore) GetBySourceKey(_ context.Context, key string) (*storage.KnowledgeObject, error) {
	return m.sourceKeys[key], nil
}

func (m *testStore) List(_ context.Context, _ storage.ObjectFilter) ([]*storage.KnowledgeObject, int, error) {
	return nil, 0, nil
}

func (m *testStore) Update(_ context.Context, _ *storage.KnowledgeObject) error { return nil }
func (m *testStore) Delete(_ context.Context, _ string) error                   { return nil }

func (m *testStore) ListBySQL(_ context.Context, _ string, _ []any, _, _ int) ([]*storage.KnowledgeObject, int, error) {
	return nil, 0, nil
}

func (m *testStore) Reinforce(_ context.Context, _ string, _ *storage.KnowledgeObject) (string, error) {
	return "", nil
}

func (m *testStore) ListWithEmbeddings(_ context.Context) ([]*storage.KnowledgeObject, error) {
	return nil, nil
}

func (m *testStore) ListWithoutEmbeddings(_ context.Context) ([]*storage.KnowledgeObject, error) {
	return nil, nil
}

func (m *testStore) SetReminder(_ context.Context, _ string, _ time.Time) error { return nil }
func (m *testStore) ClearReminder(_ context.Context, _ string) error            { return nil }

func (m *testStore) ListDueReminders(_ context.Context, _ time.Time) ([]*storage.KnowledgeObject, error) {
	return nil, nil
}

func (m *testStore) MarkReminded(_ context.Context, _ string, _ time.Time) error { return nil }

func (m *testStore) ListPendingReminders(_ context.Context) ([]*storage.KnowledgeObject, error) {
	return nil, nil
}

func (m *testStore) VectorSearch(_ context.Context, _ []float32, _ storage.ObjectFilter) ([]*storage.KnowledgeObject, error) {
	return nil, nil
}

func (m *testStore) FTSSearch(_ context.Context, _ string, _ storage.ObjectFilter) ([]*storage.KnowledgeObject, error) {
	return nil, nil
}

func (m *testStore) FTSSearchNodeAware(_ context.Context, _ string, _ storage.ObjectFilter, _ pluginapi.NodeAwareFilter) ([]*pluginapi.NodeAwareResult, error) {
	return nil, nil
}

func (m *testStore) VectorSearchNodeAware(_ context.Context, _ []float32, _ storage.ObjectFilter, _ pluginapi.NodeAwareFilter) ([]*pluginapi.NodeAwareResult, error) {
	return nil, nil
}
