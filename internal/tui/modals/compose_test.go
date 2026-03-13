package modals_test

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/tui"
	"github.com/ideacrafterslabs/ctxt/internal/tui/modals"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type composeStubAdapter struct{}

func (a *composeStubAdapter) Search(_ context.Context, _ string, _ int) ([]*storage.KnowledgeObject, error) {
	return nil, nil
}
func (a *composeStubAdapter) GetObject(_ context.Context, _ string) (*storage.KnowledgeObject, error) {
	return nil, nil
}
func (a *composeStubAdapter) ListObjects(_ context.Context, _ storage.ObjectFilter) ([]*storage.KnowledgeObject, int, error) {
	objs := []*storage.KnowledgeObject{{ID: "x", Summaries: []string{"test"}}}
	return objs, 1, nil
}
func (a *composeStubAdapter) ListJobs(_ context.Context, _ storage.JobFilter) ([]*storage.Job, int, error) {
	return nil, 0, nil
}
func (a *composeStubAdapter) RetryJob(_ context.Context, _ string) error  { return nil }
func (a *composeStubAdapter) CancelJob(_ context.Context, _ string) error { return nil }
func (a *composeStubAdapter) Analyze(_ context.Context, _ tui.AnalyzeRequest) (string, error) {
	return "", nil
}
func (a *composeStubAdapter) Compose(_ context.Context, objs []*storage.KnowledgeObject, t string) (string, error) {
	return "# " + t + "\n\ncontent from " + objs[0].ID, nil
}

func TestComposeModalSubmitReturnsComposeResultMsg(t *testing.T) {
	m := modals.NewComposeModal(&composeStubAdapter{}, tui.DefaultTheme())
	m.Open()
	m.SetType("summary")

	cmd := m.Submit()
	require.NotNil(t, cmd)

	msg := cmd()
	result, ok := msg.(tui.ComposeResultMsg)
	require.True(t, ok, "expected ComposeResultMsg, got %T", msg)
	assert.Contains(t, result.Markdown, "summary")
}

func TestComposeModalViewNonEmpty(t *testing.T) {
	m := modals.NewComposeModal(&composeStubAdapter{}, tui.DefaultTheme())
	m.Open()
	view := m.View(120, 30)
	assert.NotEmpty(t, view)
}
