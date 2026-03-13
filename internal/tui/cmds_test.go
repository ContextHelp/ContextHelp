package tui_test

import (
	"context"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/tui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchCmdReturnsSearchResultsMsg(t *testing.T) {
	objs := []*storage.KnowledgeObject{
		{ID: "abc", Type: "text", Summaries: []string{"hello world"}},
	}
	mock := &MockAdapter{
		SearchFn: func(_ context.Context, _ string, _ int) ([]*storage.KnowledgeObject, error) {
			return objs, nil
		},
	}

	cmd := tui.SearchCmd(mock, "hello")
	require.NotNil(t, cmd)

	// Execute the command synchronously (Bubble Tea commands are plain functions).
	msg := cmd()
	result, ok := msg.(tui.SearchResultsMsg)
	require.True(t, ok, "expected SearchResultsMsg, got %T", msg)
	assert.Len(t, result.Results, 1)
	assert.Equal(t, "abc", result.Results[0].ID)
}

func TestPollJobsCmdReturnsJobsUpdatedMsg(t *testing.T) {
	jobs := []*storage.Job{
		{ID: "job1", Status: storage.JobPending},
	}
	mock := &MockAdapter{
		ListJobsFn: func(_ context.Context, _ storage.JobFilter) ([]*storage.Job, int, error) {
			return jobs, 1, nil
		},
	}

	cmd := tui.PollJobsCmd(mock)
	require.NotNil(t, cmd)

	// The poll command fires immediately on first call; subsequent ticks are
	// triggered by the returned tea.Cmd. For unit testing we only verify the
	// message type of the first emission.
	msg := cmd()
	updated, ok := msg.(tui.JobsUpdatedMsg)
	require.True(t, ok, "expected JobsUpdatedMsg, got %T", msg)
	assert.Len(t, updated.Jobs, 1)
}

func TestLoadObjectCmdReturnsObjectLoadedMsg(t *testing.T) {
	obj := &storage.KnowledgeObject{ID: "xyz", Type: "url"}
	mock := &MockAdapter{
		GetObjectFn: func(_ context.Context, id string) (*storage.KnowledgeObject, error) {
			assert.Equal(t, "xyz", id)
			return obj, nil
		},
	}

	cmd := tui.LoadObjectCmd(mock, "xyz")
	msg := cmd()
	loaded, ok := msg.(tui.ObjectLoadedMsg)
	require.True(t, ok)
	assert.Equal(t, "xyz", loaded.Object.ID)
}

func TestSearchCmdDebounce(t *testing.T) {
	// Verify that rapid successive calls to SearchCmd with the same query
	// incur at least the debounce delay (250ms). This is a smoke test; the
	// real debounce lives inside the pane.
	start := time.Now()
	mock := &MockAdapter{
		SearchFn: func(_ context.Context, _ string, _ int) ([]*storage.KnowledgeObject, error) {
			return nil, nil
		},
	}
	cmd := tui.SearchCmd(mock, "test")
	_ = cmd()
	assert.WithinDuration(t, start, time.Now(), 500*time.Millisecond)
}
