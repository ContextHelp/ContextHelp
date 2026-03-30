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
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
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

// BuildGraphKO builds a graph-canonical KnowledgeObject with Graph populated.
// Flat fields (Tags, Sections, Summaries) are also set for backward compat.
// id and typ are used as-is; content becomes the section body and summary text.
// tags is the list of tag labels to attach.
func BuildGraphKO(id, typ, content string, tags ...string) *storage.KnowledgeObject {
	now := time.Now().Truncate(time.Second)

	// Flat fields — kept for backward compat.
	flatTags := make([]storage.Tag, len(tags))
	for i, lbl := range tags {
		flatTags[i] = storage.Tag{Label: lbl, Weight: 1.0}
	}

	// Graph nodes: one summary + one section + one tag per label.
	nodes := []pluginapi.GraphNode{
		{
			ID:       pluginapi.NewNodeID(id, pluginapi.NodeTypeSummary, 0),
			NodeType: pluginapi.NodeTypeSummary,
			Label:    "Summary",
			Content:  content,
			Order:    0,
		},
		{
			ID:       pluginapi.NewNodeID(id, pluginapi.NodeTypeSection, 0),
			NodeType: pluginapi.NodeTypeSection,
			Label:    "Body",
			Content:  content,
			Order:    0,
		},
	}
	for i, lbl := range tags {
		nodes = append(nodes, pluginapi.GraphNode{
			ID:       pluginapi.NewNodeID(id, pluginapi.NodeTypeTag, i),
			NodeType: pluginapi.NodeTypeTag,
			Label:    lbl,
			Content:  lbl,
			Order:    i,
		})
	}

	return &storage.KnowledgeObject{
		ID:         id,
		Type:       typ,
		RawContent: content,
		Summaries:  []string{content},
		Sections: []storage.Section{{
			Title:   "Body",
			Content: content,
			Order:   0,
		}},
		Tags:      flatTags,
		Graph:     &pluginapi.ObjectGraph{Nodes: nodes},
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// SeedObjects creates n KnowledgeObjects with deterministic IDs ("seed-obj-0",
// "seed-obj-1", ...) and graph-canonical content. It returns the created objects.
func SeedObjects(t *testing.T, driver storage.StorageDriver, n int) []*storage.KnowledgeObject {
	t.Helper()
	ctx := context.Background()
	objects := make([]*storage.KnowledgeObject, n)
	for i := 0; i < n; i++ {
		obj := BuildGraphKO(
			fmt.Sprintf("seed-obj-%d", i),
			"text",
			fmt.Sprintf("content for object %d", i),
		)
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
