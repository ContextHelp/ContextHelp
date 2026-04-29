package modals_test

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/tui"
	"github.com/ideacrafterslabs/ctxt/internal/tui/modals"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type captureStubAdapter struct {
	analyzeJobID string
}

func (a *captureStubAdapter) Search(_ context.Context, _ string, _ int) ([]*storage.KnowledgeObject, error) {
	return nil, nil
}
func (a *captureStubAdapter) GetObject(_ context.Context, _ string) (*storage.KnowledgeObject, error) {
	return nil, nil
}
func (a *captureStubAdapter) ListObjects(_ context.Context, _ storage.ObjectFilter) ([]*storage.KnowledgeObject, int, error) {
	return nil, 0, nil
}
func (a *captureStubAdapter) ListJobs(_ context.Context, _ storage.JobFilter) ([]*storage.Job, int, error) {
	return nil, 0, nil
}
func (a *captureStubAdapter) RetryJob(_ context.Context, _ string) error  { return nil }
func (a *captureStubAdapter) CancelJob(_ context.Context, _ string) error { return nil }
func (a *captureStubAdapter) Analyze(_ context.Context, _ tui.AnalyzeRequest) (string, error) {
	return a.analyzeJobID, nil
}
func (a *captureStubAdapter) Compose(_ context.Context, _ []*storage.KnowledgeObject, _ string) (string, error) {
	return "", nil
}

func TestCaptureModalSubmitReturnsCaptureSubmittedMsg(t *testing.T) {
	adapter := &captureStubAdapter{analyzeJobID: "job-xyz"}
	m := modals.NewCaptureModal(adapter, tui.DefaultTheme())
	m.Open()
	m.SetContent("some content to capture")

	cmd := m.Submit()
	require.NotNil(t, cmd)

	msg := cmd()
	captured, ok := msg.(tui.CaptureSubmittedMsg)
	require.True(t, ok, "expected CaptureSubmittedMsg, got %T", msg)
	assert.Equal(t, "job-xyz", captured.JobID)
	assert.False(t, m.IsActive(), "modal should close after submit")
}

func TestCaptureModalEscClosesWithoutSubmit(t *testing.T) {
	adapter := &captureStubAdapter{analyzeJobID: "job-abc"}
	m := modals.NewCaptureModal(adapter, tui.DefaultTheme())
	m.Open()

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	assert.Nil(t, cmd)
	assert.False(t, m.IsActive())
}

func TestCaptureModalViewNonEmpty(t *testing.T) {
	m := modals.NewCaptureModal(&captureStubAdapter{}, tui.DefaultTheme())
	m.Open()
	view := m.View(80, 24)
	assert.NotEmpty(t, view)
}
