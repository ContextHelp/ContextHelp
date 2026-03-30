package storageutil

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTestDriver(t *testing.T) {
	driver := NewTestDriver(t)

	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	obj := &storage.KnowledgeObject{
		ID:        "test-obj",
		Type:      "article",
		CreatedAt: now,
		UpdatedAt: now,
	}

	require.NoError(t, driver.Objects().Create(ctx, obj))

	got, err := driver.Objects().Get(ctx, "test-obj")
	require.NoError(t, err)
	assert.Equal(t, "test-obj", got.ID)
}

func TestSeedObjects(t *testing.T) {
	driver := NewTestDriver(t)
	ctx := context.Background()

	t.Run("creates requested count", func(t *testing.T) {
		objects := SeedObjects(t, driver, 5)
		require.Len(t, objects, 5)
	})

	t.Run("objects have correct IDs and type", func(t *testing.T) {
		for i := 0; i < 5; i++ {
			got, err := driver.Objects().Get(ctx, fmt.Sprintf("seed-obj-%d", i))
			require.NoError(t, err, "get seed-obj-%d", i)
			assert.Equal(t, fmt.Sprintf("seed-obj-%d", i), got.ID)
			assert.Equal(t, "text", got.Type)
			assert.Equal(t, fmt.Sprintf("content for object %d", i), got.RawContent)
			assert.False(t, got.CreatedAt.IsZero(), "CreatedAt should be set")
			assert.False(t, got.UpdatedAt.IsZero(), "UpdatedAt should be set")
		}
	})

	t.Run("zero count returns empty slice", func(t *testing.T) {
		d2 := NewTestDriver(t)
		objects := SeedObjects(t, d2, 0)
		assert.Empty(t, objects)
	})
}

func TestBuildGraphKO(t *testing.T) {
	t.Run("graph non-nil with nodes", func(t *testing.T) {
		ko := BuildGraphKO("ko-1", "note", "hello world", "go", "testing")
		require.NotNil(t, ko.Graph)
		assert.NotEmpty(t, ko.Graph.Nodes)
	})

	t.Run("contains summary node", func(t *testing.T) {
		ko := BuildGraphKO("ko-2", "note", "summary text")
		var found bool
		for _, n := range ko.Graph.Nodes {
			if n.NodeType == pluginapi.NodeTypeSummary {
				found = true
				assert.Equal(t, "summary text", n.Content)
			}
		}
		assert.True(t, found, "expected a summary node")
	})

	t.Run("contains section node", func(t *testing.T) {
		ko := BuildGraphKO("ko-3", "note", "body text")
		var found bool
		for _, n := range ko.Graph.Nodes {
			if n.NodeType == pluginapi.NodeTypeSection {
				found = true
				assert.Equal(t, "body text", n.Content)
			}
		}
		assert.True(t, found, "expected a section node")
	})

	t.Run("tag nodes match labels", func(t *testing.T) {
		ko := BuildGraphKO("ko-4", "note", "content", "alpha", "beta")
		var tagNodes []string
		for _, n := range ko.Graph.Nodes {
			if n.NodeType == pluginapi.NodeTypeTag {
				tagNodes = append(tagNodes, n.Label)
			}
		}
		assert.ElementsMatch(t, []string{"alpha", "beta"}, tagNodes)
	})

	t.Run("flat fields preserved", func(t *testing.T) {
		ko := BuildGraphKO("ko-5", "text", "flat content", "mytag")
		assert.Equal(t, "flat content", ko.RawContent)
		require.Len(t, ko.Tags, 1)
		assert.Equal(t, "mytag", ko.Tags[0].Label)
		assert.NotEmpty(t, ko.Summaries)
		assert.NotEmpty(t, ko.Sections)
	})

	t.Run("node IDs are valid", func(t *testing.T) {
		ko := BuildGraphKO("ko-6", "note", "content", "tag1")
		for _, n := range ko.Graph.Nodes {
			ref, err := pluginapi.ParseNodeID(n.ID)
			require.NoError(t, err, "node ID %q should parse", n.ID)
			assert.Equal(t, "ko-6", ref.ObjectID)
			assert.Equal(t, string(n.NodeType), ref.NodeType)
		}
	})
}

func TestSeedEntities(t *testing.T) {
	driver := NewTestDriver(t)
	ctx := context.Background()

	t.Run("creates entities with namespace from slug prefix", func(t *testing.T) {
		slugs := []string{"team.alice", "team.bob", "org.acme"}
		entities := SeedEntities(t, driver, slugs...)
		require.Len(t, entities, 3)

		got, err := driver.Entities().Get(ctx, "team.alice")
		require.NoError(t, err)
		assert.Equal(t, "team.alice", got.Slug)
		assert.Equal(t, "team", got.Namespace)

		got, err = driver.Entities().Get(ctx, "org.acme")
		require.NoError(t, err)
		assert.Equal(t, "org.acme", got.Slug)
		assert.Equal(t, "org", got.Namespace)
	})

	t.Run("slug without dot uses default namespace", func(t *testing.T) {
		d2 := NewTestDriver(t)
		entities := SeedEntities(t, d2, "standalone")
		require.Len(t, entities, 1)
		assert.Equal(t, "default", entities[0].Namespace)

		got, err := d2.Entities().Get(ctx, "standalone")
		require.NoError(t, err)
		assert.Equal(t, "default", got.Namespace)
	})

	t.Run("title matches slug", func(t *testing.T) {
		d2 := NewTestDriver(t)
		entities := SeedEntities(t, d2, "ns.entity-title")
		require.Len(t, entities, 1)
		assert.Equal(t, "ns.entity-title", entities[0].Title)
	})

	t.Run("timestamps are set", func(t *testing.T) {
		d2 := NewTestDriver(t)
		entities := SeedEntities(t, d2, "ts.check")
		require.Len(t, entities, 1)
		assert.False(t, entities[0].CreatedAt.IsZero())
		assert.False(t, entities[0].UpdatedAt.IsZero())
	})

	t.Run("no slugs returns empty slice", func(t *testing.T) {
		d2 := NewTestDriver(t)
		entities := SeedEntities(t, d2)
		assert.Empty(t, entities)
	})
}

func TestSeedJobs(t *testing.T) {
	driver := NewTestDriver(t)
	ctx := context.Background()

	t.Run("creates jobs with given statuses", func(t *testing.T) {
		statuses := []storage.JobStatus{
			storage.JobPending,
			storage.JobRunning,
			storage.JobCompleted,
			storage.JobFailed,
		}
		jobs := SeedJobs(t, driver, statuses...)
		require.Len(t, jobs, 4)

		for i, status := range statuses {
			assert.Equal(t, fmt.Sprintf("seed-job-%d", i), jobs[i].ID)
			assert.Equal(t, status, jobs[i].Status)
		}
	})

	t.Run("jobs are persisted and retrievable", func(t *testing.T) {
		for i := 0; i < 4; i++ {
			got, err := driver.Jobs().Get(ctx, fmt.Sprintf("seed-job-%d", i))
			require.NoError(t, err, "get seed-job-%d", i)
			assert.Equal(t, fmt.Sprintf("seed-job-%d", i), got.ID)
			assert.Equal(t, "ingest", got.Type)
			assert.Equal(t, "default", got.Pipeline)
			assert.Equal(t, "test", got.Source)
			assert.Equal(t, 3, got.MaxRetries)
		}
	})

	t.Run("job statuses match expected values", func(t *testing.T) {
		expected := []storage.JobStatus{
			storage.JobPending,
			storage.JobRunning,
			storage.JobCompleted,
			storage.JobFailed,
		}
		for i, exp := range expected {
			got, err := driver.Jobs().Get(ctx, fmt.Sprintf("seed-job-%d", i))
			require.NoError(t, err)
			assert.Equal(t, exp, got.Status)
		}
	})

	t.Run("timestamps are set", func(t *testing.T) {
		before := time.Now().Add(-time.Minute)
		d2 := NewTestDriver(t)
		jobs := SeedJobs(t, d2, storage.JobPending)
		require.Len(t, jobs, 1)
		assert.True(t, jobs[0].CreatedAt.After(before) || jobs[0].CreatedAt.Equal(before))
	})

	t.Run("no statuses returns empty slice", func(t *testing.T) {
		d2 := NewTestDriver(t)
		jobs := SeedJobs(t, d2)
		assert.Empty(t, jobs)
	})
}
