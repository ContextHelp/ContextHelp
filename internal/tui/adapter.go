package tui

import (
	"context"

	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/tui/types"
)

// AnalyzeRequest mirrors service.AnalyzeRequest. Re-exported from types package.
type AnalyzeRequest = types.AnalyzeRequest

// ServiceAdapter is the narrow interface the TUI uses to talk to the backend.
// Re-exported from types package.
type ServiceAdapter = types.ServiceAdapter

// RealAdapter implements ServiceAdapter by delegating to *service.Service.
type RealAdapter struct {
	svc *service.Service
}

// NewRealAdapter wraps a *service.Service. It is safe to pass nil in unit tests
// that only verify interface compliance.
func NewRealAdapter(svc *service.Service) *RealAdapter {
	return &RealAdapter{svc: svc}
}

func (a *RealAdapter) Search(ctx context.Context, query string, limit int) ([]*storage.KnowledgeObject, error) {
	return a.svc.FindByText(ctx, query, limit)
}

func (a *RealAdapter) GetObject(ctx context.Context, id string) (*storage.KnowledgeObject, error) {
	return a.svc.GetObject(ctx, id)
}

func (a *RealAdapter) ListObjects(ctx context.Context, filter storage.ObjectFilter) ([]*storage.KnowledgeObject, int, error) {
	return a.svc.ListObjects(ctx, filter)
}

func (a *RealAdapter) ListJobs(ctx context.Context, filter storage.JobFilter) ([]*storage.Job, int, error) {
	return a.svc.ListJobs(ctx, filter)
}

func (a *RealAdapter) RetryJob(ctx context.Context, id string) error {
	return a.svc.RetryJob(ctx, id)
}

func (a *RealAdapter) CancelJob(ctx context.Context, id string) error {
	return a.svc.CancelJob(ctx, id)
}

func (a *RealAdapter) Analyze(ctx context.Context, req types.AnalyzeRequest) (string, error) {
	return a.svc.Analyze(ctx, service.AnalyzeRequest{
		Content:  req.Content,
		Type:     req.Type,
		Pipeline: req.Pipeline,
		Source:   req.Source,
	})
}

func (a *RealAdapter) Compose(ctx context.Context, objects []*storage.KnowledgeObject, t string) (string, error) {
	return a.svc.Compose(ctx, objects, t)
}
