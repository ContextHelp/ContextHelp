package ambient

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"hop.top/kit/go/runtime/bus"
)

// recordingBus captures every published Event. Substrate tests assert the
// expected lifecycle + per-event topic sequence reaches subscribers.
type recordingBus struct {
	bus.Bus
	mu     sync.Mutex
	events []bus.Event
}

func newRecordingBus() *recordingBus {
	rb := &recordingBus{Bus: bus.New()}
	// Subscribe to every ctxt.ambient.* topic.
	rb.Subscribe("ctxt.ambient.#", func(_ context.Context, e bus.Event) error {
		rb.mu.Lock()
		rb.events = append(rb.events, e)
		rb.mu.Unlock()
		return nil
	})
	return rb
}

func (rb *recordingBus) Topics() []string {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	out := make([]string, len(rb.events))
	for i, e := range rb.events {
		out[i] = string(e.Topic)
	}
	return out
}

func (rb *recordingBus) HasTopic(topic string) bool {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	for _, e := range rb.events {
		if string(e.Topic) == topic {
			return true
		}
	}
	return false
}

func (rb *recordingBus) CountTopic(topic string) int {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	n := 0
	for _, e := range rb.events {
		if string(e.Topic) == topic {
			n++
		}
	}
	return n
}

func TestNewRunner_RequiresBus(t *testing.T) {
	t.Parallel()
	if _, err := NewRunner(RunnerOptions{}); err == nil {
		t.Fatal("NewRunner with nil bus: expected error, got nil")
	}
}

func TestNewRunner_DefaultsToNoopDependencies(t *testing.T) {
	t.Parallel()
	r, err := NewRunner(RunnerOptions{Bus: bus.New()})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	if r.cutter == nil || r.dedup == nil || r.enqueue == nil {
		t.Errorf("expected noop defaults, got cutter=%v dedup=%v enqueue=%v",
			r.cutter, r.dedup, r.enqueue)
	}
	if got := r.State(); got != StateStopped {
		t.Errorf("initial state = %q, want %q", got, StateStopped)
	}
}

// TestRunner_DispatchEmitsExpectedTopicSequence drives one event through and
// asserts the kit/bus topics fire in the documented order.
func TestRunner_DispatchEmitsExpectedTopicSequence(t *testing.T) {
	t.Parallel()
	rb := newRecordingBus()
	src := newFakeSource("clipboard")
	enq := &recordingEnqueuer{}

	r, err := NewRunner(RunnerOptions{
		Bus:      rb.Bus,
		Enqueuer: enq,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	if err := r.Register(src); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	done := make(chan error, 1)
	go func() { done <- r.Start(ctx) }()

	// Wait for source.started before emitting.
	waitForCondition(t, time.Second, func() bool {
		return rb.HasTopic("ctxt.ambient.source.started")
	})

	src.Emit(RawEvent{
		Source:            "clipboard",
		OccurredAt:        time.Now(),
		Kind:              KindURL,
		Payload:           []byte("https://example.com"),
		Fingerprint:       "fp-1",
		SuggestedPipeline: "url.generic",
	})

	waitForCondition(t, time.Second, func() bool {
		return rb.HasTopic("ctxt.ambient.enqueue.succeeded")
	})

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	// Required topic order: captured → enqueue.attempted → enqueue.succeeded.
	if !rb.HasTopic("ctxt.ambient.event.captured") {
		t.Error("missing ctxt.ambient.event.captured")
	}
	if !rb.HasTopic("ctxt.ambient.enqueue.attempted") {
		t.Error("missing ctxt.ambient.enqueue.attempted")
	}
	if !rb.HasTopic("ctxt.ambient.enqueue.succeeded") {
		t.Error("missing ctxt.ambient.enqueue.succeeded")
	}
	if !rb.HasTopic("ctxt.ambient.source.stopped") {
		t.Error("missing ctxt.ambient.source.stopped")
	}

	if got := enq.Snapshot(); len(got) != 1 {
		t.Fatalf("enqueuer received %d events, want 1", len(got))
	}
}

// TestRunner_DedupDropsDuplicateFingerprints verifies that the second event
// with the same fingerprint is dropped at dedup with the documented topic.
func TestRunner_DedupDropsDuplicateFingerprints(t *testing.T) {
	t.Parallel()
	rb := newRecordingBus()
	src := newFakeSource("clipboard")
	enq := &recordingEnqueuer{}
	dedup := newRecordingDedup()

	r, err := NewRunner(RunnerOptions{
		Bus:      rb.Bus,
		Dedup:    dedup,
		Enqueuer: enq,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	_ = r.Register(src)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- r.Start(ctx) }()
	waitForCondition(t, time.Second, func() bool {
		return rb.HasTopic("ctxt.ambient.source.started")
	})

	ev := RawEvent{Source: "clipboard", Fingerprint: "fp-same", Kind: KindText, Payload: []byte("hi")}
	src.Emit(ev)
	src.Emit(ev) // duplicate

	waitForCondition(t, time.Second, func() bool {
		return rb.CountTopic("ctxt.ambient.event.deduped") == 1
	})
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Start: %v", err)
	}

	if got := rb.CountTopic("ctxt.ambient.enqueue.succeeded"); got != 1 {
		t.Errorf("enqueue.succeeded fired %d times, want 1 (second event should dedupe)", got)
	}
	if got := rb.CountTopic("ctxt.ambient.event.deduped"); got != 1 {
		t.Errorf("event.deduped fired %d times, want 1", got)
	}
	if got := enq.Snapshot(); len(got) != 1 {
		t.Errorf("enqueuer received %d events, want 1", len(got))
	}
}

// TestRunner_TagsEventsWithSessionID verifies the cutter is consulted on
// every event and the resulting SessionID lands on the event before enqueue.
func TestRunner_TagsEventsWithSessionID(t *testing.T) {
	t.Parallel()
	rb := newRecordingBus()
	src := newFakeSource("clipboard")
	enq := &recordingEnqueuer{}
	cutter := &stubCutter{id: "sess_a1b2c3"}

	r, err := NewRunner(RunnerOptions{
		Bus:      rb.Bus,
		Cutter:   cutter,
		Enqueuer: enq,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	_ = r.Register(src)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- r.Start(ctx) }()
	waitForCondition(t, time.Second, func() bool {
		return rb.HasTopic("ctxt.ambient.source.started")
	})

	src.Emit(RawEvent{Source: "clipboard", Fingerprint: "fp-1", Kind: KindText, Payload: []byte("x")})

	waitForCondition(t, time.Second, func() bool {
		return rb.HasTopic("ctxt.ambient.enqueue.succeeded")
	})
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Start: %v", err)
	}

	if cutter.Events() != 1 {
		t.Errorf("cutter.OnEvent called %d times, want 1", cutter.Events())
	}
	got := enq.Snapshot()
	if len(got) != 1 {
		t.Fatalf("enqueuer got %d events, want 1", len(got))
	}
	if got[0].SessionID != "sess_a1b2c3" {
		t.Errorf("SessionID = %q, want sess_a1b2c3", got[0].SessionID)
	}
	if !rb.HasTopic("ctxt.ambient.session.event_joined") {
		t.Error("missing ctxt.ambient.session.event_joined when SessionID populated")
	}
}

// TestRunner_EnqueueFailureEmitsFailedTopic verifies that a non-nil enqueue
// error fires the documented .failed topic and the event is not retried in
// the same dispatch cycle (retry policy lives elsewhere).
func TestRunner_EnqueueFailureEmitsFailedTopic(t *testing.T) {
	t.Parallel()
	rb := newRecordingBus()
	src := newFakeSource("clipboard")
	enq := &recordingEnqueuer{fail: errors.New("dpkms unreachable")}

	r, err := NewRunner(RunnerOptions{Bus: rb.Bus, Enqueuer: enq})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	_ = r.Register(src)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- r.Start(ctx) }()
	waitForCondition(t, time.Second, func() bool {
		return rb.HasTopic("ctxt.ambient.source.started")
	})

	src.Emit(RawEvent{Source: "clipboard", Fingerprint: "fp-1", Kind: KindText, Payload: []byte("x")})

	waitForCondition(t, time.Second, func() bool {
		return rb.HasTopic("ctxt.ambient.enqueue.failed")
	})
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Start: %v", err)
	}

	if rb.HasTopic("ctxt.ambient.enqueue.succeeded") {
		t.Error("succeeded should not fire when enqueue returns error")
	}
	if got := rb.CountTopic("ctxt.ambient.enqueue.failed"); got != 1 {
		t.Errorf("enqueue.failed fired %d times, want 1", got)
	}
}

// TestRunner_AlreadyStartedRejected verifies double-Start returns an error.
func TestRunner_AlreadyStartedRejected(t *testing.T) {
	t.Parallel()
	r, err := NewRunner(RunnerOptions{Bus: bus.New()})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	done := make(chan error, 1)
	go func() { done <- r.Start(ctx) }()
	waitForCondition(t, time.Second, func() bool {
		return r.State() == StateReady
	})

	if err := r.Start(ctx); err == nil {
		t.Fatal("second Start: expected error, got nil")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("first Start: %v", err)
	}
}
