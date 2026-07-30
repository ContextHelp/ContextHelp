package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/stretchr/testify/require"
)

// errPipelineStore is a PipelineStore whose Get always fails, used to prove
// pipelineExists treats a store failure as "does not exist" rather than
// panicking or reporting a false positive.
type errPipelineStore struct {
	storage.PipelineStore
	err error
}

func (e errPipelineStore) Get(context.Context, string) (*storage.Pipeline, error) {
	return nil, e.err
}

// nilPipelineStore returns (nil, nil) — the "no row, no error" shape some
// drivers use. pipelineExists must not treat a nil pipeline as found.
type nilPipelineStore struct {
	storage.PipelineStore
}

func (nilPipelineStore) Get(context.Context, string) (*storage.Pipeline, error) {
	return nil, nil
}

// pipelineStoreDriver wraps a real driver, swapping only Pipelines().
type pipelineStoreDriver struct {
	storage.StorageDriver
	pipelines storage.PipelineStore
}

func (d pipelineStoreDriver) Pipelines() storage.PipelineStore { return d.pipelines }

// newPipelineExistsService builds a service with an empty registry so every
// registry lookup misses unless the test explicitly upserts.
func newPipelineExistsService(t *testing.T, driver storage.StorageDriver) *Service {
	t.Helper()
	q := jobs.NewQueue(driver.Jobs())
	engine := search.NewEngine(driver)
	return New(driver, q, pipeline.DefaultRegistry(), engine, "", nil)
}

// storePipeline persists a pipeline directly, mimicking what CreatePipeline
// does: it writes to the store and never touches the in-memory registry.
func storePipeline(t *testing.T, driver storage.StorageDriver, name string) {
	t.Helper()
	now := time.Now().Truncate(time.Second)
	require.NoError(t, driver.Pipelines().Create(context.Background(), &storage.Pipeline{
		ID:        "pipeline-" + name,
		Name:      name,
		CreatedAt: now,
		UpdatedAt: now,
	}))
}

// TestPipelineExists covers the four resolution paths. The store-only case is
// the actual bug: CreatePipeline persists without registering, so a registry-
// only check rejected every user-created pipeline.
func TestPipelineExists(t *testing.T) {
	tests := []struct {
		name     string
		pipeName string
		// setup seeds the registry and/or store before the check.
		setup func(t *testing.T, svc *Service, driver storage.StorageDriver)
		want  bool
	}{
		{
			name:     "registry only",
			pipeName: "text.short",
			setup: func(_ *testing.T, svc *Service, _ storage.StorageDriver) {
				svc.Pipes.Upsert("text.short", &pipeline.Pipeline{PipelineName: "text.short"})
			},
			want: true,
		},
		{
			name:     "store only",
			pipeName: "user.custom",
			setup: func(t *testing.T, _ *Service, driver storage.StorageDriver) {
				storePipeline(t, driver, "user.custom")
			},
			want: true,
		},
		{
			name:     "registry and store",
			pipeName: "both.places",
			setup: func(t *testing.T, svc *Service, driver storage.StorageDriver) {
				svc.Pipes.Upsert("both.places", &pipeline.Pipeline{PipelineName: "both.places"})
				storePipeline(t, driver, "both.places")
			},
			want: true,
		},
		{
			name:     "neither",
			pipeName: "does.not.exist",
			setup:    func(*testing.T, *Service, storage.StorageDriver) {},
			want:     false,
		},
		{
			name:     "empty name",
			pipeName: "",
			setup:    func(*testing.T, *Service, storage.StorageDriver) {},
			want:     false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			driver := storageutil.NewTestDriver(t)
			svc := newPipelineExistsService(t, driver)
			tt.setup(t, svc, driver)
			got := svc.pipelineExists(context.Background(), tt.pipeName)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestPipelineExists_StoreError asserts a failing store lookup reports "not
// found" instead of propagating a panic or a false positive.
func TestPipelineExists_StoreError(t *testing.T) {
	base := storageutil.NewTestDriver(t)
	driver := pipelineStoreDriver{
		StorageDriver: base,
		pipelines:     errPipelineStore{err: errors.New("store unavailable")},
	}
	svc := newPipelineExistsService(t, driver)

	require.False(t, svc.pipelineExists(context.Background(), "user.custom"),
		"store error must not resolve as found")

	// A registry hit short-circuits before the store is consulted, so a
	// broken store cannot mask a built-in.
	svc.Pipes.Upsert("text.short", &pipeline.Pipeline{PipelineName: "text.short"})
	require.True(t, svc.pipelineExists(context.Background(), "text.short"),
		"registry hit must not depend on the store")
}

// TestPipelineExists_StoreReturnsNil guards the (nil, nil) store shape.
func TestPipelineExists_StoreReturnsNil(t *testing.T) {
	base := storageutil.NewTestDriver(t)
	driver := pipelineStoreDriver{StorageDriver: base, pipelines: nilPipelineStore{}}
	svc := newPipelineExistsService(t, driver)

	require.False(t, svc.pipelineExists(context.Background(), "user.custom"),
		"nil pipeline with nil error must not resolve as found")
}

// TestEnqueueAcceptsStoreBackedPipeline is the end-to-end form of the bug:
// a pipeline that exists only in the store (as CreatePipeline leaves it) must
// enqueue. Against the registry-only check this returns ErrPipelineNotFound.
func TestEnqueueAcceptsStoreBackedPipeline(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	svc := newPipelineExistsService(t, driver)
	storePipeline(t, driver, "user.custom")

	ctx := context.Background()
	jobID, err := svc.Enqueue(ctx, AnalyzeRequest{
		Content:  "hello store-backed pipeline",
		Type:     "text",
		Pipeline: "user.custom",
	})
	require.NoError(t, err, "user-created pipeline must be accepted on enqueue")
	require.NotEmpty(t, jobID)

	job, err := svc.GetJob(ctx, jobID)
	require.NoError(t, err)
	require.Equal(t, "user.custom", job.Pipeline)
}

// TestAnalyzeAcceptsStoreBackedPipeline mirrors the above for Analyze, which
// shares the same pipelineExists check.
func TestAnalyzeAcceptsStoreBackedPipeline(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	svc := newPipelineExistsService(t, driver)
	storePipeline(t, driver, "user.custom")

	jobID, err := svc.Analyze(context.Background(), AnalyzeRequest{
		Content:  "hello store-backed pipeline",
		Type:     "text",
		Pipeline: "user.custom",
	})
	require.NoError(t, err, "user-created pipeline must be accepted on analyze")
	require.NotEmpty(t, jobID)
}

// TestEnqueueRejectsUnknownPipeline confirms the guard still bites when the
// name is absent from both sources — the fix must not blanket-accept.
func TestEnqueueRejectsUnknownPipeline(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	svc := newPipelineExistsService(t, driver)

	_, err := svc.Enqueue(context.Background(), AnalyzeRequest{
		Content:  "hello",
		Type:     "text",
		Pipeline: "does.not.exist",
	})
	require.ErrorIs(t, err, ErrPipelineNotFound)
}
