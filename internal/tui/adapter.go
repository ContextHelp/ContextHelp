package tui

import (
	"context"
	"errors"

	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/tui/types"
)

// AnalyzeRequest mirrors service.AnalyzeRequest. Re-exported from types package.
type AnalyzeRequest = types.AnalyzeRequest

// ServiceAdapter is the narrow interface the TUI uses to talk to the backend.
// Re-exported from types package.
type ServiceAdapter = types.ServiceAdapter

// Opener opens the service for one operation: write selects the locked
// write path, otherwise the read-only one. The returned func closes it.
type Opener func(write bool) (*service.Service, func(), error)

// RealAdapter implements ServiceAdapter by delegating to *service.Service.
// It opens the service per operation, so a TUI session never holds the
// database lock between operations.
type RealAdapter struct {
	open Opener
}

// NewRealAdapter wraps open. It is safe to pass nil in unit tests that
// only verify interface compliance.
func NewRealAdapter(open Opener) *RealAdapter {
	return &RealAdapter{open: open}
}

// with runs fn on a service opened for one operation.
func with[T any](a *RealAdapter, write bool, fn func(*service.Service) (T, error)) (T, error) {
	var zero T
	if a.open == nil {
		return zero, errors.New("tui: no service")
	}
	svc, closeFn, err := a.open(write)
	if err != nil {
		return zero, err
	}
	defer closeFn()
	return fn(svc)
}

func (a *RealAdapter) Search(ctx context.Context, query string, limit int) ([]*storage.KnowledgeObject, error) {
	return with(a, false, func(s *service.Service) ([]*storage.KnowledgeObject, error) {
		return s.FindByText(ctx, query, limit)
	})
}

func (a *RealAdapter) GetObject(ctx context.Context, id string) (*storage.KnowledgeObject, error) {
	return with(a, false, func(s *service.Service) (*storage.KnowledgeObject, error) {
		return s.GetObject(ctx, id)
	})
}

// counted pairs a page with its total, for with.
type counted[T any] struct {
	items []T
	total int
}

func (a *RealAdapter) ListObjects(ctx context.Context, filter storage.ObjectFilter) ([]*storage.KnowledgeObject, int, error) {
	r, err := with(a, false, func(s *service.Service) (counted[*storage.KnowledgeObject], error) {
		items, n, err := s.ListObjects(ctx, filter)
		return counted[*storage.KnowledgeObject]{items, n}, err
	})
	return r.items, r.total, err
}

func (a *RealAdapter) ListJobs(ctx context.Context, filter storage.JobFilter) ([]*storage.Job, int, error) {
	r, err := with(a, false, func(s *service.Service) (counted[*storage.Job], error) {
		items, n, err := s.ListJobs(ctx, filter)
		return counted[*storage.Job]{items, n}, err
	})
	return r.items, r.total, err
}

func (a *RealAdapter) RetryJob(ctx context.Context, id string) error {
	_, err := with(a, true, func(s *service.Service) (struct{}, error) {
		return struct{}{}, s.RetryJob(ctx, id)
	})
	return err
}

func (a *RealAdapter) CancelJob(ctx context.Context, id string) error {
	_, err := with(a, true, func(s *service.Service) (struct{}, error) {
		return struct{}{}, s.CancelJob(ctx, id)
	})
	return err
}

func (a *RealAdapter) Analyze(ctx context.Context, req types.AnalyzeRequest) (string, error) {
	return with(a, true, func(s *service.Service) (string, error) {
		return s.Analyze(ctx, service.AnalyzeRequest{
			Content:  req.Content,
			Type:     req.Type,
			Pipeline: req.Pipeline,
			Source:   req.Source,
		})
	})
}

func (a *RealAdapter) Compose(ctx context.Context, objects []*storage.KnowledgeObject, t string) (string, error) {
	return with(a, false, func(s *service.Service) (string, error) {
		return s.Compose(ctx, objects, t)
	})
}
