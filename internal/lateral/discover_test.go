package lateral

import (
	"context"
	"sync"
	"testing"

	"hop.top/kit/go/runtime/bus"
)

type fakeBus struct {
	mu         sync.Mutex
	subscribed []string
	handlers   map[string]bus.Handler
}

func newFakeBus() *fakeBus {
	return &fakeBus{handlers: map[string]bus.Handler{}}
}

func (b *fakeBus) Publish(_ context.Context, _ bus.Event) error { return nil }

func (b *fakeBus) Subscribe(pattern string, h bus.Handler) bus.Unsubscribe {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subscribed = append(b.subscribed, pattern)
	b.handlers[pattern] = h
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		delete(b.handlers, pattern)
	}
}

func (b *fakeBus) SubscribeAsync(_ string, _ bus.AsyncHandler) bus.Unsubscribe {
	return func() {}
}

func (b *fakeBus) Close(_ context.Context) error { return nil }

func TestDiscover_SubscribesToBothEvents(t *testing.T) {
	fb := newFakeBus()
	d := NewDiscover(DiscoverConfig{Bus: fb, Registry: NewRegistry()})
	if err := d.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer d.Stop()

	want := map[string]bool{
		"ctxt.ingest.object.persisted": false,
		"ctxt.ingest.object.captured":  false,
	}
	fb.mu.Lock()
	for _, s := range fb.subscribed {
		if _, ok := want[s]; ok {
			want[s] = true
		}
	}
	fb.mu.Unlock()
	for k, ok := range want {
		if !ok {
			t.Errorf("expected subscription to %q", k)
		}
	}
}

type fakeStore struct {
	attempts int
	readyAt  int
	record   map[string]any
}

func (s *fakeStore) Read(_ context.Context, _ string) (map[string]any, error) {
	s.attempts++
	if s.attempts < s.readyAt {
		return nil, ErrNotYetReadable
	}
	return s.record, nil
}

func TestDiscover_ReadWithRetryEventuallySucceeds(t *testing.T) {
	store := &fakeStore{readyAt: 3, record: map[string]any{"id": "o-1", "namespace": "@github.org/repo"}}
	d := NewDiscover(DiscoverConfig{Bus: newFakeBus(), Registry: NewRegistry(), Store: store})

	rec, err := d.readParent(context.Background(), "o-1")
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if rec["namespace"] != "@github.org/repo" {
		t.Fatalf("unexpected record: %v", rec)
	}
	if store.attempts < 3 {
		t.Fatalf("expected at least 3 attempts, got %d", store.attempts)
	}
}

func TestDiscover_ReadWithRetryFailsAfterAllAttempts(t *testing.T) {
	store := &fakeStore{readyAt: 100} // never ready within retry budget
	d := NewDiscover(DiscoverConfig{Bus: newFakeBus(), Registry: NewRegistry(), Store: store})

	_, err := d.readParent(context.Background(), "o-missing")
	if err == nil {
		t.Fatal("expected error after retries exhausted")
	}
}
