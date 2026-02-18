package storage

import "context"

// StorageDriver is the top-level interface for all persistence operations.
type StorageDriver interface {
	Init(ctx context.Context) error
	Close(ctx context.Context) error
	Objects() ObjectStore
	Entities() EntityStore
	Edges() EdgeStore
	Jobs() JobStore
	Pipelines() PipelineStore
	Steps() StepStore
	Registries() RegistryStore
	Reminders() ReminderStore
	Health(ctx context.Context) error
}

// ObjectStore persists and retrieves knowledge objects.
type ObjectStore interface {
	Create(ctx context.Context, obj *KnowledgeObject) error
	Get(ctx context.Context, id string) (*KnowledgeObject, error)
	List(ctx context.Context, filter ObjectFilter) ([]*KnowledgeObject, int, error)
	Update(ctx context.Context, obj *KnowledgeObject) error
	Delete(ctx context.Context, id string) error
	ListBySQL(ctx context.Context, where string, args []any, limit, offset int) ([]*KnowledgeObject, int, error)
}

// EntityStore persists and retrieves named entities.
type EntityStore interface {
	Upsert(ctx context.Context, entity *Entity) error
	Get(ctx context.Context, slug string) (*Entity, error)
	List(ctx context.Context, filter EntityFilter) ([]*Entity, error)
	Resolve(ctx context.Context, mention string) (*Entity, error)
}

// EdgeStore persists and retrieves edges between nodes (ADR-049).
type EdgeStore interface {
	Create(ctx context.Context, edge *Edge) error
	ListFrom(ctx context.Context, fromType, fromID string) ([]*Edge, error)
	ListTo(ctx context.Context, toType, toID string) ([]*Edge, error)
	Delete(ctx context.Context, id string) error
	DeleteByObject(ctx context.Context, objectID string) error
}

// JobStore persists and retrieves jobs.
type JobStore interface {
	Create(ctx context.Context, job *Job) error
	Get(ctx context.Context, id string) (*Job, error)
	List(ctx context.Context, filter JobFilter) ([]*Job, int, error)
	AcquireNext(ctx context.Context) (*Job, error)
	Complete(ctx context.Context, id string, resultID string) error
	Fail(ctx context.Context, id string, errMsg string) error
	Retry(ctx context.Context, id string) error
	RecoverStale(ctx context.Context, timeout int64) (int, error)
}

// PipelineStore persists and retrieves pipelines.
type PipelineStore interface {
	Create(ctx context.Context, pipeline *Pipeline) error
	Get(ctx context.Context, name string) (*Pipeline, error)
	List(ctx context.Context, filter PipelineFilter) ([]*Pipeline, int, error)
	Update(ctx context.Context, pipeline *Pipeline) error
	Delete(ctx context.Context, name string) error
	Archive(ctx context.Context, name string) error
	Unarchive(ctx context.Context, name string) error
}

// StepStore persists and retrieves registered steps.
type StepStore interface {
	Create(ctx context.Context, step *RegisteredStep) error
	Get(ctx context.Context, name string) (*RegisteredStep, error)
	List(ctx context.Context, source string) ([]*RegisteredStep, int, error)
	Unregister(ctx context.Context, name string) error
	Update(ctx context.Context, step *RegisteredStep) error
}

// RegistryStore caches registry manifests.
type RegistryStore interface {
	CacheManifest(ctx context.Context, cache *RegistryCache) error
	GetCachedManifest(ctx context.Context, url string) (*RegistryCache, error)
	UpdateETag(ctx context.Context, url, etag string) error
	List(ctx context.Context) ([]*RegistryCache, int, error)
}

// ReminderStore persists and retrieves system reminders.
type ReminderStore interface {
	Create(ctx context.Context, reminder *SystemReminder) error
	Get(ctx context.Context, id string) (*SystemReminder, error)
	List(ctx context.Context, activeOnly bool) ([]*SystemReminder, int, error)
	Dismiss(ctx context.Context, id string) error
}
