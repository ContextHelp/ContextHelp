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
