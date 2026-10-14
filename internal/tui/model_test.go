package tui_test

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/tui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildModel is a helper that creates a Model with a stub adapter and
// zero StartOpts.
func buildModel() tui.Model {
	return buildModelWith(tui.StartOpts{})
}

// buildModelWith creates a Model with a stub adapter and the given opts.
func buildModelWith(opts tui.StartOpts) tui.Model {
	adapter := &MockAdapter{
		SearchFn: func(_ context.Context, _ string, _ int) ([]*storage.KnowledgeObject, error) {
			return nil, nil
		},
		ListJobsFn: func(_ context.Context, _ storage.JobFilter) ([]*storage.Job, int, error) {
			return nil, 0, nil
		},
		GetObjectFn: func(_ context.Context, _ string) (*storage.KnowledgeObject, error) {
			return nil, nil
		},
		ListObjectsFn: func(_ context.Context, _ storage.ObjectFilter) ([]*storage.KnowledgeObject, int, error) {
			return nil, 0, nil
		},
		RetryJobFn:  func(_ context.Context, _ string) error { return nil },
		CancelJobFn: func(_ context.Context, _ string) error { return nil },
		AnalyzeFn:   func(_ context.Context, _ tui.AnalyzeRequest) (string, error) { return "", nil },
		ComposeFn: func(_ context.Context, _ []*storage.KnowledgeObject, _ string) (string, error) {
			return "", nil
		},
	}
	return tui.New(adapter, opts)
}

func TestModelWindowSizeStackedLayout(t *testing.T) {
	m := buildModel()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	view := updated.(tui.Model).View()
	// At width 80 (< BreakpointStacked=100) the layout is stacked.
	assert.NotEmpty(t, view)
}

func TestModelWindowSizeSplitLayout(t *testing.T) {
	m := buildModel()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	view := updated.(tui.Model).View()
	assert.NotEmpty(t, view)
}

func TestModelWindowSizeWideLayout(t *testing.T) {
	m := buildModel()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 200, Height: 30})
	view := updated.(tui.Model).View()
	assert.NotEmpty(t, view)
}

func TestModelJobsUpdatedRepollsJobs(t *testing.T) {
	m := buildModel()
	_, cmd := m.Update(tui.JobsUpdatedMsg{Jobs: []*storage.Job{}})
	// The model must re-issue PollJobsCmd; cmd must be non-nil.
	require.NotNil(t, cmd, "expected a poll command after JobsUpdatedMsg")
}

func TestModelQuitKeyExitsProgram(t *testing.T) {
	m := buildModel()
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	require.NotNil(t, cmd)
	msg := cmd()
	assert.Equal(t, tea.Quit(), msg)
}

// The status bar shows the profile the caller resolved, verbatim; the
// model never falls back to a config default of its own.
func TestModelStatusBarShowsStartProfile(t *testing.T) {
	for _, tc := range []struct {
		name, profile, want string
	}{
		{name: "named profile", profile: "growth", want: "profile: growth "},
		{name: "no profile", profile: "", want: "profile:  "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := buildModelWith(tui.StartOpts{Profile: tc.profile})
			updated, _ := m.Update(tea.WindowSizeMsg{Width: 200, Height: 30})
			view := updated.(tui.Model).View().Content
			assert.True(t, strings.Contains(view, tc.want), "status bar missing %q:\n%s", tc.want, view)
		})
	}
}
