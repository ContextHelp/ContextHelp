package storage

import (
	"context"
	"testing"
)

// mockDriver verifies that a concrete type can satisfy StorageDriver.
type mockDriver struct{}

func (m *mockDriver) Init(ctx context.Context) error          { return nil }
func (m *mockDriver) Close(ctx context.Context) error         { return nil }
func (m *mockDriver) Objects() ObjectStore                    { return &mockObjectStore{} }
func (m *mockDriver) Entities() EntityStore                   { return &mockEntityStore{} }
func (m *mockDriver) Edges() EdgeStore                        { return &mockEdgeStore{} }
func (m *mockDriver) Jobs() JobStore                          { return &mockJobStore{} }
func (m *mockDriver) Health(ctx context.Context) error        { return nil }

type mockObjectStore struct{}

func (m *mockObjectStore) Create(ctx context.Context, obj *KnowledgeObject) error { return nil }
func (m *mockObjectStore) Get(ctx context.Context, id string) (*KnowledgeObject, error) {
	return nil, nil
}
func (m *mockObjectStore) List(ctx context.Context, filter ObjectFilter) ([]*KnowledgeObject, int, error) {
	return nil, 0, nil
}
func (m *mockObjectStore) Update(ctx context.Context, obj *KnowledgeObject) error { return nil }
func (m *mockObjectStore) Delete(ctx context.Context, id string) error            { return nil }
func (m *mockObjectStore) ListBySQL(ctx context.Context, where string, args []any, limit, offset int) ([]*KnowledgeObject, int, error) {
	return nil, 0, nil
}

type mockEntityStore struct{}

func (m *mockEntityStore) Upsert(ctx context.Context, entity *Entity) error { return nil }
func (m *mockEntityStore) Get(ctx context.Context, slug string) (*Entity, error) {
	return nil, nil
}
func (m *mockEntityStore) List(ctx context.Context, filter EntityFilter) ([]*Entity, error) {
	return nil, nil
}
func (m *mockEntityStore) Resolve(ctx context.Context, mention string) (*Entity, error) {
	return nil, nil
}

type mockEdgeStore struct{}

func (m *mockEdgeStore) Create(ctx context.Context, edge *Edge) error { return nil }
func (m *mockEdgeStore) ListFrom(ctx context.Context, fromType, fromID string) ([]*Edge, error) {
	return nil, nil
}
func (m *mockEdgeStore) ListTo(ctx context.Context, toType, toID string) ([]*Edge, error) {
	return nil, nil
}
func (m *mockEdgeStore) Delete(ctx context.Context, id string) error            { return nil }
func (m *mockEdgeStore) DeleteByObject(ctx context.Context, objectID string) error { return nil }

type mockJobStore struct{}

func (m *mockJobStore) Create(ctx context.Context, job *Job) error { return nil }
func (m *mockJobStore) Get(ctx context.Context, id string) (*Job, error) {
	return nil, nil
}
func (m *mockJobStore) List(ctx context.Context, filter JobFilter) ([]*Job, int, error) {
	return nil, 0, nil
}
func (m *mockJobStore) AcquireNext(ctx context.Context) (*Job, error) { return nil, nil }
func (m *mockJobStore) Complete(ctx context.Context, id string, resultID string) error {
	return nil
}
func (m *mockJobStore) Fail(ctx context.Context, id string, errMsg string) error { return nil }
func (m *mockJobStore) Retry(ctx context.Context, id string) error               { return nil }
func (m *mockJobStore) RecoverStale(ctx context.Context, timeout int64) (int, error) {
	return 0, nil
}

func TestStorageDriverInterfaceSatisfaction(t *testing.T) {
	var d StorageDriver = &mockDriver{}
	if d == nil {
		t.Fatal("mockDriver should satisfy StorageDriver")
	}
}

func TestObjectStoreInterfaceSatisfaction(t *testing.T) {
	var s ObjectStore = &mockObjectStore{}
	if s == nil {
		t.Fatal("mockObjectStore should satisfy ObjectStore")
	}
}

func TestEntityStoreInterfaceSatisfaction(t *testing.T) {
	var s EntityStore = &mockEntityStore{}
	if s == nil {
		t.Fatal("mockEntityStore should satisfy EntityStore")
	}
}

func TestEdgeStoreInterfaceSatisfaction(t *testing.T) {
	var s EdgeStore = &mockEdgeStore{}
	if s == nil {
		t.Fatal("mockEdgeStore should satisfy EdgeStore")
	}
}

func TestJobStoreInterfaceSatisfaction(t *testing.T) {
	var s JobStore = &mockJobStore{}
	if s == nil {
		t.Fatal("mockJobStore should satisfy JobStore")
	}
}
