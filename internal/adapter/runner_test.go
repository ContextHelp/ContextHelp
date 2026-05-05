package adapter

import (
	"context"
	"sync"
	"testing"

	"hop.top/kit/go/runtime/bus"
)

// recordingAdapter tracks lifecycle method calls for runner tests.
// Embeds stubAdapter (defined in registry_test.go) for capability + identity boilerplate.
type recordingAdapter struct {
	stubAdapter
	mu          sync.Mutex
	startCalled bool
	drainCalled bool
	stopCalled  bool
	startErr    error
}

func (r *recordingAdapter) Start(_ context.Context, _ bus.Bus) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.startCalled = true
	return r.startErr
}

func (r *recordingAdapter) Drain(_ context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.drainCalled = true
	return nil
}

func (r *recordingAdapter) Stop(_ context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopCalled = true
	return nil
}

// TestRunnerStartEmitsLifecycleTopics verifies that Runner.Start
// publishes started + readied (in that order) on the bus and calls
// the adapter's Start method exactly once.
func TestRunnerStartEmitsLifecycleTopics(t *testing.T) {
	b := bus.New()
	defer func() { _ = b.Close(context.Background()) }()

	got := make([]string, 0, 2)
	var mu sync.Mutex
	unsub := b.Subscribe("dpkms.adapter.lifecycle.*", func(_ context.Context, e bus.Event) error {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, string(e.Topic))
		return nil
	})
	defer unsub()

	a := &recordingAdapter{stubAdapter: stubAdapter{protocol: "email", backend: "test"}}
	runner := NewRunner(b)
	if err := runner.Start(context.Background(), a); err != nil {
		t.Fatalf("runner.Start: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("want 2 lifecycle events, got %d (%v)", len(got), got)
	}
	if got[0] != "dpkms.adapter.lifecycle.started" {
		t.Errorf("first event = %q, want dpkms.adapter.lifecycle.started", got[0])
	}
	if got[1] != "dpkms.adapter.lifecycle.readied" {
		t.Errorf("second event = %q, want dpkms.adapter.lifecycle.readied", got[1])
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.startCalled {
		t.Error("Adapter.Start was not invoked")
	}
}

// TestRunnerStopEmitsDrainAndStop verifies that Runner.Stop drives the
// adapter through Drain → Stop and emits drained + stopped on the bus.
// Both methods on the adapter must run; both events must fire.
func TestRunnerStopEmitsDrainAndStop(t *testing.T) {
	b := bus.New()
	defer func() { _ = b.Close(context.Background()) }()

	var got []string
	var mu sync.Mutex
	unsub := b.Subscribe("dpkms.adapter.lifecycle.*", func(_ context.Context, e bus.Event) error {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, string(e.Topic))
		return nil
	})
	defer unsub()

	a := &recordingAdapter{stubAdapter: stubAdapter{protocol: "email", backend: "test"}}
	runner := NewRunner(b)
	_ = runner.Start(context.Background(), a)

	if err := runner.Stop(context.Background(), a); err != nil {
		t.Fatalf("runner.Stop: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.drainCalled || !a.stopCalled {
		t.Errorf("expected Drain + Stop called, got drain=%v stop=%v", a.drainCalled, a.stopCalled)
	}
	if len(got) < 4 {
		t.Fatalf("want >= 4 lifecycle events, got %d (%v)", len(got), got)
	}
	if got[len(got)-2] != "dpkms.adapter.lifecycle.drained" {
		t.Errorf("expected drained event, got %q", got[len(got)-2])
	}
	if got[len(got)-1] != "dpkms.adapter.lifecycle.stopped" {
		t.Errorf("expected stopped event, got %q", got[len(got)-1])
	}
}
