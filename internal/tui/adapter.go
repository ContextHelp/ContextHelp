package tui

import (
	"context"
	"errors"
	"sync"

	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/tui/types"
)

// AnalyzeRequest mirrors service.AnalyzeRequest. Re-exported from types package.
type AnalyzeRequest = types.AnalyzeRequest

// ServiceAdapter is the narrow interface the TUI uses to talk to the backend.
// Re-exported from types package.
type ServiceAdapter = types.ServiceAdapter

// Opener opens the service on the write path for one operation; the
// returned func closes it and releases the database lock.
type Opener func() (*service.Service, func(), error)

// RealAdapter implements ServiceAdapter by delegating to *service.Service.
// Reads go to one read-only service opened for the session (it takes no
// lock); each write opens the write path for that operation alone, one at
// a time, so the TUI never holds the lock between operations and never
// contends with itself for it.
type RealAdapter struct {
	read      *service.Service
	openWrite Opener
	writeMu   sync.Mutex
	ctxFn     func(context.Context) context.Context
}

// NewRealAdapter wraps the session's read service and the write opener.
// Both may be nil in unit tests that only verify interface compliance.
func NewRealAdapter(read *service.Service, openWrite Opener) *RealAdapter {
	return &RealAdapter{read: read, openWrite: openWrite}
}

// WithContext sets fn to derive every operation's context (the viewer
// the store scopes reads by).
func (a *RealAdapter) WithContext(fn func(context.Context) context.Context) *RealAdapter {
	a.ctxFn = fn
	return a
}

func (a *RealAdapter) ctx(ctx context.Context) context.Context {
	if a.ctxFn == nil {
		return ctx
	}
	return a.ctxFn(ctx)
}

// with runs fn on the read service, or on a write service opened for
// this operation alone.
func with[T any](a *RealAdapter, write bool, fn func(*service.Service) (T, error)) (T, error) {
	var zero T
	if !write {
		if a.read == nil {
			return zero, errors.New("tui: no service")
		}
		return fn(a.read)
	}
	if a.openWrite == nil {
		return zero, errors.New("tui: no service")
	}
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	svc, closeFn, err := a.openWrite()
	if err != nil {
		return zero, err
	}
	defer closeFn()
	return fn(svc)
}

func (a *RealAdapter) Search(ctx context.Context, query string, limit int) ([]*storage.KnowledgeObject, error) {
	ctx = a.ctx(ctx)
	return with(a, false, func(s *service.Service) ([]*storage.KnowledgeObject, error) {
		return s.FindByText(ctx, query, limit)
	})
}

func (a *RealAdapter) GetObject(ctx context.Context, id string) (*storage.KnowledgeObject, error) {
	ctx = a.ctx(ctx)
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
	ctx = a.ctx(ctx)
	r, err := with(a, false, func(s *service.Service) (counted[*storage.KnowledgeObject], error) {
		items, n, err := s.ListObjects(ctx, filter)
		return counted[*storage.KnowledgeObject]{items, n}, err
	})
	return r.items, r.total, err
}

func (a *RealAdapter) ListJobs(ctx context.Context, filter storage.JobFilter) ([]*storage.Job, int, error) {
	ctx = a.ctx(ctx)
	r, err := with(a, false, func(s *service.Service) (counted[*storage.Job], error) {
		items, n, err := s.ListJobs(ctx, filter)
		return counted[*storage.Job]{items, n}, err
	})
	return r.items, r.total, err
}

func (a *RealAdapter) RetryJob(ctx context.Context, id string) error {
	ctx = a.ctx(ctx)
	_, err := with(a, true, func(s *service.Service) (struct{}, error) {
		return struct{}{}, s.RetryJob(ctx, id)
	})
	return err
}

func (a *RealAdapter) CancelJob(ctx context.Context, id string) error {
	ctx = a.ctx(ctx)
	_, err := with(a, true, func(s *service.Service) (struct{}, error) {
		return struct{}{}, s.CancelJob(ctx, id)
	})
	return err
}

func (a *RealAdapter) Analyze(ctx context.Context, req types.AnalyzeRequest) (string, error) {
	ctx = a.ctx(ctx)
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
	ctx = a.ctx(ctx)
	return with(a, false, func(s *service.Service) (string, error) {
		return s.Compose(ctx, objects, t)
	})
}
