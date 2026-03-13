package panes_test

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/tui"
	"github.com/ideacrafterslabs/ctxt/internal/tui/panes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubAdapter implements tui.ServiceAdapter for pane tests.
type stubAdapter struct{}

func (s *stubAdapter) Search(_ context.Context, _ string, _ int) ([]*storage.KnowledgeObject, error) {
	return nil, nil
}
func (s *stubAdapter) GetObject(_ context.Context, _ string) (*storage.KnowledgeObject, error) {
	return nil, nil
}
func (s *stubAdapter) ListObjects(_ context.Context, _ storage.ObjectFilter) ([]*storage.KnowledgeObject, int, error) {
	return nil, 0, nil
}
func (s *stubAdapter) ListJobs(_ context.Context, _ storage.JobFilter) ([]*storage.Job, int, error) {
	return nil, 0, nil
}
func (s *stubAdapter) RetryJob(_ context.Context, _ string) error  { return nil }
func (s *stubAdapter) CancelJob(_ context.Context, _ string) error { return nil }
func (s *stubAdapter) Analyze(_ context.Context, _ tui.AnalyzeRequest) (string, error) {
	return "", nil
}
func (s *stubAdapter) Compose(_ context.Context, _ []*storage.KnowledgeObject, _ string) (string, error) {
	return "", nil
}

func TestSearchPaneUpdatesListOnResults(t *testing.T) {
	theme := tui.DefaultTheme()
	sp := panes.NewSearchPane(&stubAdapter{}, theme)

	objs := []*storage.KnowledgeObject{
		{ID: "a", Type: "text", Summaries: []string{"Alpha"}},
		{ID: "b", Type: "url", Summaries: []string{"Beta"}},
	}

	updated, cmd := sp.Update(tui.SearchResultsMsg{Results: objs})
	require.NotNil(t, updated)
	assert.Nil(t, cmd, "no command expected after receiving results")

	searchPane, ok := updated.(*panes.SearchPane)
	require.True(t, ok)
	assert.Equal(t, 2, searchPane.ResultCount())
}

func TestSearchPaneViewNonEmpty(t *testing.T) {
	theme := tui.DefaultTheme()
	sp := panes.NewSearchPane(&stubAdapter{}, theme)
	view := sp.View(80, 24)
	assert.NotEmpty(t, view)
}

func TestSearchPaneSelectedObjectNilOnEmpty(t *testing.T) {
	theme := tui.DefaultTheme()
	sp := panes.NewSearchPane(&stubAdapter{}, theme)
	assert.Nil(t, sp.SelectedObject())
}

func TestSearchPaneFocusBlur(t *testing.T) {
	theme := tui.DefaultTheme()
	sp := panes.NewSearchPane(&stubAdapter{}, theme)
	assert.False(t, sp.IsFocused())
	sp.Focus()
	assert.True(t, sp.IsFocused())
	sp.Blur()
	assert.False(t, sp.IsFocused())
}
