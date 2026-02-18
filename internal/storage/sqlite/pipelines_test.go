package sqlite

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestPipelineStoreCreate(t *testing.T) {
	driver := newTestDriver(t)
	ctx := context.Background()

	pipeline := &storage.Pipeline{
		ID:          "pipe-1",
		Name:        "test-pipeline",
		Description: "Test pipeline",
		Steps: []storage.StepRef{
			{Name: "text_summary", Config: map[string]any{"max_length": 100}},
		},
		IsBuiltIn: false,
		Archived:  false,
		Sandbox: &storage.SandboxConfig{
			Enabled:        true,
			IsolationLevel: "process",
			ResourceLimits: &storage.ResourceLimitsConfig{
				MaxMemory: "512MB",
				MaxCPU:    "2.0",
			},
		},
	}

	err := driver.Pipelines().Create(ctx, pipeline)
	require.NoError(t, err)

	got, err := driver.Pipelines().Get(ctx, "test-pipeline")
	require.NoError(t, err)
	assert.Equal(t, "test-pipeline", got.Name)
	assert.Equal(t, "Test pipeline", got.Description)
	assert.Equal(t, 1, len(got.Steps))
	assert.Equal(t, "text_summary", got.Steps[0].Name)
	assert.False(t, got.IsBuiltIn)
	assert.False(t, got.Archived)
	assert.True(t, got.Sandbox.Enabled)
	assert.Equal(t, "process", got.Sandbox.IsolationLevel)
}

func TestPipelineStoreGet(t *testing.T) {
	driver := newTestDriver(t)
	ctx := context.Background()

	pipeline := &storage.Pipeline{
		ID:          "pipe-1",
		Name:        "test-pipeline",
		Description: "Test pipeline",
		Steps:       []storage.StepRef{{Name: "text_summary"}},
		IsBuiltIn:   false,
		Archived:    false,
	}

	err := driver.Pipelines().Create(ctx, pipeline)
	require.NoError(t, err)

	got, err := driver.Pipelines().Get(ctx, "test-pipeline")
	require.NoError(t, err)
	assert.Equal(t, "test-pipeline", got.Name)

	_, err = driver.Pipelines().Get(ctx, "nonexistent")
	assert.Error(t, err)
}

func TestPipelineStoreList(t *testing.T) {
	driver := newTestDriver(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		pipeline := &storage.Pipeline{
			ID:    "pipe-" + string(rune('a'+i)),
			Name:  "test-pipeline-" + string(rune('a'+i)),
			Steps: []storage.StepRef{{Name: "text_summary"}},
		}
		err := driver.Pipelines().Create(ctx, pipeline)
		require.NoError(t, err)
	}

	pipelines, total, err := driver.Pipelines().List(ctx, storage.PipelineFilter{})
	require.NoError(t, err)
	assert.Equal(t, 3, total)
	assert.Len(t, pipelines, 3)

	pipelines, total, err = driver.Pipelines().List(ctx, storage.PipelineFilter{Name: "test-pipeline-a"})
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Len(t, pipelines, 1)
	assert.Equal(t, "test-pipeline-a", pipelines[0].Name)
}

func TestPipelineStoreListArchived(t *testing.T) {
	driver := newTestDriver(t)
	ctx := context.Background()

	pipeline := &storage.Pipeline{
		ID:       "pipe-1",
		Name:     "test-pipeline",
		Steps:    []storage.StepRef{{Name: "text_summary"}},
		Archived: false,
	}

	err := driver.Pipelines().Create(ctx, pipeline)
	require.NoError(t, err)

	pipelines, total, err := driver.Pipelines().List(ctx, storage.PipelineFilter{IncludeArchived: false})
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Len(t, pipelines, 1)
	assert.False(t, pipelines[0].Archived)

	err = driver.Pipelines().Archive(ctx, "test-pipeline")
	require.NoError(t, err)

	pipelines, total, err = driver.Pipelines().List(ctx, storage.PipelineFilter{OnlyArchived: true})
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Len(t, pipelines, 1)
	assert.True(t, pipelines[0].Archived)
}

func TestPipelineStoreDelete(t *testing.T) {
	driver := newTestDriver(t)
	ctx := context.Background()

	pipeline := &storage.Pipeline{
		ID:    "pipe-1",
		Name:  "test-pipeline",
		Steps: []storage.StepRef{{Name: "text_summary"}},
	}

	err := driver.Pipelines().Create(ctx, pipeline)
	require.NoError(t, err)

	err = driver.Pipelines().Delete(ctx, "test-pipeline")
	require.NoError(t, err)

	_, err = driver.Pipelines().Get(ctx, "test-pipeline")
	assert.Error(t, err)
}

func TestPipelineStoreArchive(t *testing.T) {
	driver := newTestDriver(t)
	ctx := context.Background()

	pipeline := &storage.Pipeline{
		ID:    "pipe-1",
		Name:  "test-pipeline",
		Steps: []storage.StepRef{{Name: "text_summary"}},
	}

	err := driver.Pipelines().Create(ctx, pipeline)
	require.NoError(t, err)

	err = driver.Pipelines().Archive(ctx, "test-pipeline")
	require.NoError(t, err)

	got, err := driver.Pipelines().Get(ctx, "test-pipeline")
	require.NoError(t, err)
	assert.True(t, got.Archived)
}

func TestPipelineStoreUnarchive(t *testing.T) {
	driver := newTestDriver(t)
	ctx := context.Background()

	pipeline := &storage.Pipeline{
		ID:       "pipe-1",
		Name:     "test-pipeline",
		Steps:    []storage.StepRef{{Name: "text_summary"}},
		Archived: true,
	}

	err := driver.Pipelines().Create(ctx, pipeline)
	require.NoError(t, err)

	err = driver.Pipelines().Unarchive(ctx, "test-pipeline")
	require.NoError(t, err)

	got, err := driver.Pipelines().Get(ctx, "test-pipeline")
	require.NoError(t, err)
	assert.False(t, got.Archived)
}

func TestPipelineStoreUpdate(t *testing.T) {
	driver := newTestDriver(t)
	ctx := context.Background()

	pipeline := &storage.Pipeline{
		ID:          "pipe-1",
		Name:        "test-pipeline",
		Description: "Original description",
		Steps:       []storage.StepRef{{Name: "text_summary"}},
	}

	err := driver.Pipelines().Create(ctx, pipeline)
	require.NoError(t, err)

	updated := &storage.Pipeline{
		ID:          "pipe-1",
		Name:        "test-pipeline",
		Description: "Updated description",
		Steps:       []storage.StepRef{{Name: "text_summary"}},
	}

	err = driver.Pipelines().Update(ctx, updated)
	require.NoError(t, err)

	got, err := driver.Pipelines().Get(ctx, "test-pipeline")
	require.NoError(t, err)
	assert.Equal(t, "Updated description", got.Description)
}
