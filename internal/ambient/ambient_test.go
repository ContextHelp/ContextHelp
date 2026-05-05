package ambient

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeSource is a minimal Source for substrate tests. Tests inject events via
// Emit; Stop closes the channel.
type fakeSource struct {
	name    string
	events  chan RawEvent
	started atomic.Bool
	stopped atomic.Bool
}

func newFakeSource(name string) *fakeSource {
	return &fakeSource{
		name:   name,
		events: make(chan RawEvent, 8),
	}
}

func (f *fakeSource) Name() string { return f.name }

func (f *fakeSource) Start(_ context.Context, _ Publisher) error {
	f.started.Store(true)
	return nil
}

func (f *fakeSource) Events() <-chan RawEvent { return f.events }

func (f *fakeSource) Drain(_ context.Context) error { return nil }

func (f *fakeSource) Stop(_ context.Context) error {
	if f.stopped.CompareAndSwap(false, true) {
		close(f.events)
	}
	return nil
}

// Emit pushes a RawEvent through the source. Tests use this to drive dispatch.
func (f *fakeSource) Emit(ev RawEvent) {
	f.events <- ev
}

func TestKindsAreDistinct(t *testing.T) {
	t.Parallel()
	kinds := []Kind{KindText, KindURL, KindImage, KindFile, KindWindowFocus, KindMeeting}
	seen := make(map[Kind]bool, len(kinds))
	for _, k := range kinds {
		if seen[k] {
			t.Fatalf("duplicate kind: %q", k)
		}
		seen[k] = true
	}
}

func TestLifecycleStatesAreDistinct(t *testing.T) {
	t.Parallel()
	states := []LifecycleState{StateStopped, StateStarting, StateReady, StateDraining, StateFailed}
	seen := make(map[LifecycleState]bool, len(states))
	for _, s := range states {
		if seen[s] {
			t.Fatalf("duplicate state: %q", s)
		}
		seen[s] = true
	}
}

// Topic builders MUST follow the 4-segment past-tense convention from ADR-066:
//
//	ctxt.<category>.<object>.<action>
//
// Tests assert the resulting strings match expected shapes so callers don't
// drift away from the convention.
func TestTopicBuildersShape(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		got      string
		expected string
	}{
		{"source-lifecycle", SourceLifecycleTopic("started"), "ctxt.ambient.source.started"},
		{"event-captured", EventTopic("captured"), "ctxt.ambient.event.captured"},
		{"event-deduped", EventTopic("deduped"), "ctxt.ambient.event.deduped"},
		{"buffer-appended", BufferTopic("appended"), "ctxt.ambient.buffer.appended"},
		{"enqueue-succeeded", EnqueueTopic("succeeded"), "ctxt.ambient.enqueue.succeeded"},
		{"session-opened", SessionTopic("opened"), "ctxt.ambient.session.opened"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if c.got != c.expected {
				t.Errorf("got %q, want %q", c.got, c.expected)
			}
		})
	}
}

// recordingDedup tracks fingerprints seen so tests can assert dedup behaviour
// without depending on the real LRU implementation (T-0499 lands that).
type recordingDedup struct {
	mu   sync.Mutex
	seen map[string]bool
}

func newRecordingDedup() *recordingDedup {
	return &recordingDedup{seen: make(map[string]bool)}
}

func (r *recordingDedup) IsDuplicate(fp string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.seen[fp] {
		return true
	}
	r.seen[fp] = true
	return false
}

// recordingEnqueuer records every Enqueue call.
type recordingEnqueuer struct {
	mu     sync.Mutex
	events []RawEvent
	fail   error
}

func (r *recordingEnqueuer) Enqueue(_ context.Context, ev RawEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail != nil {
		return r.fail
	}
	r.events = append(r.events, ev)
	return nil
}

func (r *recordingEnqueuer) Snapshot() []RawEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]RawEvent, len(r.events))
	copy(out, r.events)
	return out
}

// stubCutter returns a constant SessionID. Used by tests that want to assert
// the Runner tags events correctly.
type stubCutter struct {
	id      string
	mu      sync.Mutex
	events  int
}

func (s *stubCutter) OnEvent(RawEvent) {
	s.mu.Lock()
	s.events++
	s.mu.Unlock()
}

func (s *stubCutter) ActiveID() string { return s.id }

func (s *stubCutter) Events() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.events
}

// waitForCondition polls until pred returns true or the deadline expires.
// Substrate tests run a Runner in a goroutine; we poll for the expected
// post-condition rather than time.Sleep.
func waitForCondition(t *testing.T, deadline time.Duration, pred func() bool) {
	t.Helper()
	stop := time.After(deadline)
	tick := time.NewTicker(2 * time.Millisecond)
	defer tick.Stop()
	for {
		if pred() {
			return
		}
		select {
		case <-stop:
			t.Fatalf("condition not met within %s", deadline)
		case <-tick.C:
		}
	}
}
