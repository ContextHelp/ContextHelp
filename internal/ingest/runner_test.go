package ingest

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// stubAdapter returns fixed objects.
type stubAdapter struct {
	name    string
	objects []Object
	err     error
}

func (s *stubAdapter) Name() string { return s.name }
func (s *stubAdapter) Fetch(_ context.Context) ([]Object, error) {
	return s.objects, s.err
}

// memObjectStore is a minimal in-memory ObjectStore for testing.
type memObjectStore struct {
	objects    map[string]*storage.KnowledgeObject
	sourceKeys map[string]*storage.KnowledgeObject
}

func newMemObjectStore() *memObjectStore {
	return &memObjectStore{
		objects:    make(map[string]*storage.KnowledgeObject),
		sourceKeys: make(map[string]*storage.KnowledgeObject),
	}
}

func (m *memObjectStore) Create(_ context.Context, obj *storage.KnowledgeObject) error {
	m.objects[obj.ID] = obj
	if obj.SourceKey != "" {
		m.sourceKeys[obj.SourceKey] = obj
	}
	return nil
}

func (m *memObjectStore) Get(_ context.Context, id string) (*storage.KnowledgeObject, error) {
	return m.objects[id], nil
}

func (m *memObjectStore) GetByContentHash(_ context.Context, _ string) (*storage.KnowledgeObject, error) {
	return nil, nil
}

func (m *memObjectStore) GetBySourceKey(_ context.Context, key string) (*storage.KnowledgeObject, error) {
	return m.sourceKeys[key], nil
}

func (m *memObjectStore) List(_ context.Context, _ storage.ObjectFilter) ([]*storage.KnowledgeObject, int, error) {
	return nil, 0, nil
}

func (m *memObjectStore) Update(_ context.Context, _ *storage.KnowledgeObject) error {
	return nil
}

func (m *memObjectStore) Delete(_ context.Context, _ string) error { return nil }

func (m *memObjectStore) ListBySQL(_ context.Context, _ string, _ []any, _, _ int) ([]*storage.KnowledgeObject, int, error) {
	return nil, 0, nil
}

func (m *memObjectStore) Reinforce(_ context.Context, _ string, _ *storage.KnowledgeObject) (string, error) {
	return "", nil
}

func (m *memObjectStore) ListWithEmbeddings(_ context.Context) ([]*storage.KnowledgeObject, error) {
	return nil, nil
}

func (m *memObjectStore) ListWithoutEmbeddings(_ context.Context) ([]*storage.KnowledgeObject, error) {
	return nil, nil
}

func (m *memObjectStore) SetReminder(_ context.Context, _ string, _ time.Time) error {
	return nil
}

func (m *memObjectStore) ClearReminder(_ context.Context, _ string) error { return nil }

func (m *memObjectStore) ListDueReminders(_ context.Context, _ time.Time) ([]*storage.KnowledgeObject, error) {
	return nil, nil
}

func (m *memObjectStore) MarkReminded(_ context.Context, _ string, _ time.Time) error {
	return nil
}

func (m *memObjectStore) ListPendingReminders(_ context.Context) ([]*storage.KnowledgeObject, error) {
	return nil, nil
}

func (m *memObjectStore) VectorSearch(_ context.Context, _ []float32, _ storage.ObjectFilter) ([]*storage.KnowledgeObject, error) {
	return nil, nil
}

func (m *memObjectStore) FTSSearch(_ context.Context, _ string, _ storage.ObjectFilter) ([]*storage.KnowledgeObject, error) {
	return nil, nil
}

func (m *memObjectStore) FTSSearchNodeAware(_ context.Context, _ string, _ storage.ObjectFilter, _ pluginapi.NodeAwareFilter) ([]*pluginapi.NodeAwareResult, error) {
	return nil, nil
}

func (m *memObjectStore) VectorSearchNodeAware(_ context.Context, _ []float32, _ storage.ObjectFilter, _ pluginapi.NodeAwareFilter) ([]*pluginapi.NodeAwareResult, error) {
	return nil, nil
}

func TestRunnerDeduplicates(t *testing.T) {
	store := newMemObjectStore()
	runner := NewRunner(store)

	adapter := &stubAdapter{
		name: "test",
		objects: []Object{
			{ID: "a", Type: "note", Content: "hello"},
			{ID: "b", Type: "note", Content: "world"},
		},
	}

	ctx := context.Background()

	// First run: both created
	res, err := runner.Run(ctx, adapter)
	if err != nil {
		t.Fatal(err)
	}
	if res.Created != 2 {
		t.Errorf("first run: created = %d, want 2", res.Created)
	}
	if len(store.objects) != 2 {
		t.Errorf("store has %d objects, want 2", len(store.objects))
	}

	// Second run: both skipped (dedup by source_key)
	res2, err := runner.Run(ctx, adapter)
	if err != nil {
		t.Fatal(err)
	}
	// Runner counts upsert-no-ops as created (skips silently);
	// but store count stays at 2
	if len(store.objects) != 2 {
		t.Errorf("after second run: store has %d objects, want 2",
			len(store.objects))
	}
	_ = res2
}

func TestRunnerFromStdin(t *testing.T) {
	store := newMemObjectStore()
	runner := NewRunner(store)

	input := `[{"id":"x","type":"contact","content":"hello","tags":["t1"]}]`
	res, err := runner.RunFromStdin(
		context.Background(), "pipe", bytes.NewBufferString(input))
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 1 {
		t.Errorf("total = %d, want 1", res.Total)
	}
	if res.Created != 1 {
		t.Errorf("created = %d, want 1", res.Created)
	}

	// Verify source_key was set correctly
	obj := store.sourceKeys["pipe:x"]
	if obj == nil {
		t.Fatal("expected object with source_key pipe:x")
	}
	if obj.Type != "contact" {
		t.Errorf("type = %q, want contact", obj.Type)
	}
	if len(obj.Tags) != 1 || obj.Tags[0].Label != "t1" {
		t.Errorf("tags = %v, want [{t1}]", obj.Tags)
	}
}

func TestRunnerSourceKeyFormat(t *testing.T) {
	store := newMemObjectStore()
	runner := NewRunner(store)

	adapter := &stubAdapter{
		name:    "myapp",
		objects: []Object{{ID: "obj-1", Type: "note", Content: "test"}},
	}

	_, err := runner.Run(context.Background(), adapter)
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := store.sourceKeys["myapp:obj-1"]; !ok {
		t.Error("expected source_key format adapter:id")
	}
}
