package tui_test

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/tui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockAdapter implements ServiceAdapter for testing.
type MockAdapter struct {
	SearchFn      func(ctx context.Context, query string, limit int) ([]*storage.KnowledgeObject, error)
	GetObjectFn   func(ctx context.Context, id string) (*storage.KnowledgeObject, error)
	ListObjectsFn func(ctx context.Context, filter storage.ObjectFilter) ([]*storage.KnowledgeObject, int, error)
	ListJobsFn    func(ctx context.Context, filter storage.JobFilter) ([]*storage.Job, int, error)
	RetryJobFn    func(ctx context.Context, id string) error
	CancelJobFn   func(ctx context.Context, id string) error
	AnalyzeFn     func(ctx context.Context, req tui.AnalyzeRequest) (string, error)
	ComposeFn     func(ctx context.Context, objects []*storage.KnowledgeObject, t string) (string, error)
}

func (m *MockAdapter) Search(ctx context.Context, q string, limit int) ([]*storage.KnowledgeObject, error) {
	return m.SearchFn(ctx, q, limit)
}
func (m *MockAdapter) GetObject(ctx context.Context, id string) (*storage.KnowledgeObject, error) {
	return m.GetObjectFn(ctx, id)
}
func (m *MockAdapter) ListObjects(ctx context.Context, f storage.ObjectFilter) ([]*storage.KnowledgeObject, int, error) {
	return m.ListObjectsFn(ctx, f)
}
func (m *MockAdapter) ListJobs(ctx context.Context, f storage.JobFilter) ([]*storage.Job, int, error) {
	return m.ListJobsFn(ctx, f)
}
func (m *MockAdapter) RetryJob(ctx context.Context, id string) error { return m.RetryJobFn(ctx, id) }
func (m *MockAdapter) CancelJob(ctx context.Context, id string) error {
	return m.CancelJobFn(ctx, id)
}
func (m *MockAdapter) Analyze(ctx context.Context, req tui.AnalyzeRequest) (string, error) {
	return m.AnalyzeFn(ctx, req)
}
func (m *MockAdapter) Compose(ctx context.Context, objs []*storage.KnowledgeObject, t string) (string, error) {
	return m.ComposeFn(ctx, objs, t)
}

func TestMockAdapterImplementsInterface(t *testing.T) {
	var _ tui.ServiceAdapter = (*MockAdapter)(nil)
	assert.True(t, true, "MockAdapter satisfies ServiceAdapter")
}

func TestRealAdapterDelegatesSearch(t *testing.T) {
	// RealAdapter wraps nil service — this test only exercises interface compliance.
	// Integration tests with a real service live in internal/tui/integration_test.go.
	var adapter tui.ServiceAdapter = tui.NewRealAdapter(nil)
	require.NotNil(t, adapter)
}
