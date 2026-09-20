package daemon

import (
	"context"
	"sync"
	"testing"
	"time"

	"hop.top/kit/go/runtime/bus"

	lateral "github.com/ideacrafterslabs/ctxt/internal/lateral"
	adapterbus "github.com/ideacrafterslabs/ctxt/internal/lateral/adapters/bus"
)

// recorderStrategy is a probe-counter test strategy.
type recorderStrategy struct {
	mu       sync.Mutex
	probed   []lateral.CapturedEvent
	matchURL string
}

func (r *recorderStrategy) ID() string                     { return "test.recorder" }
func (r *recorderStrategy) Family() lateral.StrategyFamily { return lateral.FamilyJIT }
func (r *recorderStrategy) Applies(_ context.Context, ev lateral.CapturedEvent) lateral.AppliesResult {
	if r.matchURL == "" || r.matchURL == ev.SourceURL {
		return lateral.AppliesResult{Matches: true}
	}
	return lateral.AppliesResult{}
}
func (r *recorderStrategy) Probe(_ context.Context, ev lateral.CapturedEvent, _ lateral.ActiveContext) ([]lateral.Candidate, error) {
	r.mu.Lock()
	r.probed = append(r.probed, ev)
	r.mu.Unlock()
	return nil, nil
}
func (r *recorderStrategy) Preconditions() []string { return nil }
func (r *recorderStrategy) snapshot() []lateral.CapturedEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]lateral.CapturedEvent, len(r.probed))
	copy(out, r.probed)
	return out
}

func newTestLifecycle(t *testing.T) (*Lifecycle, *recorderStrategy, bus.Bus) {
	t.Helper()
	b := bus.New(bus.WithEnforce(bus.ModeOff))
	pub := adapterbus.New(b)
	rec := &recorderStrategy{}
	reg := lateral.NewRegistry()
	reg.Register(rec)
	l, err := NewLifecycle(LifecycleOptions{
		Bus:       b,
		Registry:  reg,
		Publisher: pub,
		WorkerID:  "test-worker",
	})
	if err != nil {
		t.Fatalf("NewLifecycle err = %v", err)
	}
	return l, rec, b
}

func TestNewLifecycle_RequiresBusRegistryPublisher(t *testing.T) {
	cases := []LifecycleOptions{
		{},               // all nil
		{Bus: bus.New()}, // missing registry
		{Bus: bus.New(), Registry: lateral.NewRegistry()}, // missing publisher
	}
	for i, opts := range cases {
		if _, err := NewLifecycle(opts); err == nil {
			t.Errorf("case %d: expected error, got nil", i)
		}
	}
}

func TestStart_SubscribesAndDispatches(t *testing.T) {
	l, rec, b := newTestLifecycle(t)
	if err := l.Start(context.Background()); err != nil {
		t.Fatalf("Start err = %v", err)
	}
	defer l.Stop()

	err := b.Publish(context.Background(), bus.NewEvent(
		bus.Topic(CapturedEventTopic),
		"test",
		map[string]any{
			"object_id":  "obj-1",
			"source_url": "https://x.example/post/1",
		},
	))
	if err != nil {
		t.Fatalf("Publish err = %v", err)
	}
	got := rec.snapshot()
	if len(got) != 1 {
		t.Fatalf("probed %d times; want 1", len(got))
	}
	if got[0].ObjectID != "obj-1" || got[0].SourceURL != "https://x.example/post/1" {
		t.Errorf("ev = %+v", got[0])
	}
}

func TestStart_HandlesPersistedTopic(t *testing.T) {
	l, rec, b := newTestLifecycle(t)
	if err := l.Start(context.Background()); err != nil {
		t.Fatalf("Start err = %v", err)
	}
	defer l.Stop()

	_ = b.Publish(context.Background(), bus.NewEvent(
		bus.Topic(PersistedEventTopic),
		"test",
		map[string]any{"object_id": "obj-2", "source_url": "https://x.example/post/2"},
	))
	if got := rec.snapshot(); len(got) != 1 {
		t.Errorf("probed %d times; want 1", len(got))
	}
}

func TestStart_MalformedPayloadDropped(t *testing.T) {
	l, rec, b := newTestLifecycle(t)
	if err := l.Start(context.Background()); err != nil {
		t.Fatalf("Start err = %v", err)
	}
	defer l.Stop()

	// String payload — not the expected map.
	if err := b.Publish(context.Background(), bus.NewEvent(
		bus.Topic(CapturedEventTopic),
		"test",
		"not a map",
	)); err != nil {
		t.Fatalf("Publish err = %v", err)
	}
	if got := rec.snapshot(); len(got) != 0 {
		t.Errorf("probed %d times on malformed payload; want 0", len(got))
	}
}

func TestEnqueueDirect_SyncDispatch(t *testing.T) {
	l, rec, _ := newTestLifecycle(t)
	err := l.EnqueueDirect(context.Background(), lateral.CapturedEvent{
		ObjectID:  "obj-d",
		SourceURL: "https://x.example/direct",
	})
	if err != nil {
		t.Fatalf("EnqueueDirect err = %v", err)
	}
	if got := rec.snapshot(); len(got) != 1 || got[0].ObjectID != "obj-d" {
		t.Errorf("got = %v", got)
	}
}

func TestEnqueueDirect_NoMatchErrors(t *testing.T) {
	// Empty registry — nothing matches.
	b := bus.New(bus.WithEnforce(bus.ModeOff))
	l, err := NewLifecycle(LifecycleOptions{
		Bus:       b,
		Registry:  lateral.NewRegistry(),
		Publisher: adapterbus.New(b),
	})
	if err != nil {
		t.Fatalf("NewLifecycle err = %v", err)
	}
	err = l.EnqueueDirect(context.Background(), lateral.CapturedEvent{
		ObjectID:  "obj-nomatch",
		SourceURL: "https://x.example/",
	})
	if err == nil {
		t.Error("expected error on no-match")
	}
}

func TestRun_PollerProcessesScheduledColdCycle(t *testing.T) {
	l, _, _ := newTestLifecycle(t)
	l.pollIntv = 10 * time.Millisecond

	if _, err := l.EnqueueColdCycle(context.Background()); err != nil {
		t.Fatalf("EnqueueColdCycle err = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- l.Run(ctx) }()

	// Wait for at least one scan to occur. The poller's first claim
	// completes the enqueued job; ScanFunc increments the counter.
	deadline := time.Now().Add(400 * time.Millisecond)
	for time.Now().Before(deadline) {
		if l.ScanCount() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if l.ScanCount() == 0 {
		t.Error("ScanFunc never invoked")
	}
	cancel()
	<-done
}

func TestRun_StopsOnContextCancel(t *testing.T) {
	l, _, _ := newTestLifecycle(t)
	l.pollIntv = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- l.Run(ctx) }()

	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after context cancel")
	}
}
