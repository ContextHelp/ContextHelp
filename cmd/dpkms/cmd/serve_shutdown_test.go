package cmd

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/security"
	"github.com/ideacrafterslabs/ctxt/internal/server/stack"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// shutdownStore is an audit store that is also the closable store
// serve shuts down. Append blocks until release closes, then records
// whether the row landed before or after Close.
type shutdownStore struct {
	release chan struct{}

	mu     sync.Mutex
	closed bool
	rows   int // appended while open
	late   int // appended after Close
}

func (s *shutdownStore) Append(ctx context.Context, _ *storage.AuditEntry) error {
	select {
	case <-s.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		s.late++
	} else {
		s.rows++
	}
	return nil
}

func (s *shutdownStore) List(context.Context, storage.AuditFilter) ([]*storage.AuditEntry, int, error) {
	return nil, 0, nil
}

func (s *shutdownStore) GetObjectHistory(context.Context, string) ([]*storage.AuditEntry, error) {
	return nil, nil
}

func (s *shutdownStore) Close(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

// serve's shutdown writes the security audit rows queued before it
// into the store, and closes the store only after them.
func TestCloseServeStackDrainsAuditRowsBeforeStoreClose(t *testing.T) {
	store := &shutdownStore{release: make(chan struct{})}
	cfg := security.DefaultConfig()
	cfg.AuthFailureThreshold = 1
	st := &stack.Stack{Security: security.New(cfg, store)}

	const queued = 3
	for i := 0; i < queued; i++ {
		st.Security.RecordAuthFailure(context.Background(), "user:shutdown")
	}
	// The writer is still blocked on the first row when shutdown
	// starts; the rows land only once it is under way.
	time.AfterFunc(50*time.Millisecond, func() { close(store.release) })

	closeServeStack(io.Discard, st, store, 5*time.Second)

	store.mu.Lock()
	defer store.mu.Unlock()
	if !store.closed {
		t.Fatal("store not closed")
	}
	if store.late != 0 {
		t.Fatalf("%d audit row(s) written after the store closed", store.late)
	}
	if store.rows != queued {
		t.Fatalf("audit rows written before close = %d, want %d", store.rows, queued)
	}
}
