package storageutil

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
	"github.com/stretchr/testify/require"
)

// NewTestDriver creates an initialized SQLite driver in a temp directory.
// It registers cleanup to close the driver when the test finishes.
func NewTestDriver(t *testing.T) storage.StorageDriver {
	t.Helper()
	dir := t.TempDir()
	driver, err := sqlite.New(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("new test driver: %v", err)
	}
	if err := driver.Init(context.Background()); err != nil {
		t.Fatalf("init test driver: %v", err)
	}
	t.Cleanup(func() { driver.Close(context.Background()) })
	return driver
}

// SeedObjects creates n KnowledgeObjects with deterministic IDs ("seed-obj-0",
// "seed-obj-1", ...) and minimal content. It returns the created objects.
func SeedObjects(t *testing.T, driver storage.StorageDriver, n int) []*storage.KnowledgeObject {
	t.Helper()
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	objects := make([]*storage.KnowledgeObject, n)
	for i := 0; i < n; i++ {
		obj := &storage.KnowledgeObject{
			ID:         fmt.Sprintf("seed-obj-%d", i),
			Type:       "text",
			RawContent: fmt.Sprintf("content for object %d", i),
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		require.NoError(t, driver.Objects().Create(ctx, obj), "SeedObjects: create object %d", i)
		objects[i] = obj
	}
	return objects
}

// SeedEntities creates entities for the given slugs. The namespace is derived
// from each slug's prefix (the part before the first "."), or defaults to
// "default" if the slug contains no ".". It returns the created entities.
func SeedEntities(t *testing.T, driver storage.StorageDriver, slugs ...string) []*storage.Entity {
	t.Helper()
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	entities := make([]*storage.Entity, len(slugs))
	for i, slug := range slugs {
		ns := "default"
		if idx := strings.Index(slug, "."); idx > 0 {
			ns = slug[:idx]
		}
		entity := &storage.Entity{
			Slug:      slug,
			Title:     slug,
			Namespace: ns,
			CreatedAt: now,
			UpdatedAt: now,
		}
		require.NoError(t, driver.Entities().Upsert(ctx, entity), "SeedEntities: upsert entity %q", slug)
		entities[i] = entity
	}
	return entities
}

// SeedJobs creates jobs with the given statuses and deterministic IDs
// ("seed-job-0", "seed-job-1", ...). It returns the created jobs.
func SeedJobs(t *testing.T, driver storage.StorageDriver, statuses ...storage.JobStatus) []*storage.Job {
	t.Helper()
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	jobs := make([]*storage.Job, len(statuses))
	for i, status := range statuses {
		job := &storage.Job{
			ID:         fmt.Sprintf("seed-job-%d", i),
			Type:       "ingest",
			Status:     status,
			Payload:    fmt.Sprintf(`{"index":%d}`, i),
			Pipeline:   "default",
			Source:     "test",
			MaxRetries: 3,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		require.NoError(t, driver.Jobs().Create(ctx, job), "SeedJobs: create job %d", i)
		jobs[i] = job
	}
	return jobs
}
