package adapter

import (
	"context"
	"errors"
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

// TestRunnerStop_PublishFailureDoesNotSkipStop is the regression guard
// for the resource-leak bug where Runner.Stop returned early after a
// failed `drained` publish — Adapter.Stop must run even when the bus
// is unhealthy, since Stop releases resources.
//
// The test adds a sync subscriber that returns an error, which causes
// every kit/bus publish on the lifecycle topic to surface that error.
// Despite that, the runner must still call Adapter.Drain AND
// Adapter.Stop, and the returned error must wrap the publish failure
// so callers see what went wrong.
func TestRunnerStop_PublishFailureDoesNotSkipStop(t *testing.T) {
	b := bus.New()
	defer func() { _ = b.Close(context.Background()) }()

	publishVeto := errors.New("bus unhealthy")
	b.Subscribe("dpkms.adapter.lifecycle.*", func(_ context.Context, _ bus.Event) error {
		return publishVeto
	})

	a := &recordingAdapter{stubAdapter: stubAdapter{protocol: "email", backend: "test"}}
	runner := NewRunner(b)

	// Stop must return an error (publish fails) but still invoke both
	// Drain and Stop on the adapter.
	err := runner.Stop(context.Background(), a)
	if err == nil {
		t.Fatal("Stop: expected error from failing publish, got nil")
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.drainCalled {
		t.Error("Adapter.Drain was not called despite publish failure")
	}
	if !a.stopCalled {
		t.Error("Adapter.Stop was not called despite publish failure (resource-leak risk)")
	}
}

// TestRunnerStop_JoinsDrainAndStopErrors is the regression guard for
// the error-joining bug where Drain's error was %v-formatted instead
// of %w-wrapped, breaking errors.Is on the drain side. Both errors
// must be unwrap-able via errors.Is.
func TestRunnerStop_JoinsDrainAndStopErrors(t *testing.T) {
	b := bus.New()
	defer func() { _ = b.Close(context.Background()) }()

	drainErr := errors.New("drain-failure")
	stopErr := errors.New("stop-failure")

	a := &errorAdapter{
		stubAdapter: stubAdapter{protocol: "email", backend: "test"},
		drainErr:    drainErr,
		stopErr:     stopErr,
	}
	runner := NewRunner(b)
	err := runner.Stop(context.Background(), a)
	if err == nil {
		t.Fatal("Stop: expected combined error, got nil")
	}
	if !errors.Is(err, drainErr) {
		t.Errorf("errors.Is(err, drainErr) = false; drain error not unwrap-able from %v", err)
	}
	if !errors.Is(err, stopErr) {
		t.Errorf("errors.Is(err, stopErr) = false; stop error not unwrap-able from %v", err)
	}
}

// errorAdapter returns configured errors from Drain and Stop.
type errorAdapter struct {
	stubAdapter
	drainErr error
	stopErr  error
}

func (e *errorAdapter) Drain(_ context.Context) error { return e.drainErr }
func (e *errorAdapter) Stop(_ context.Context) error  { return e.stopErr }
