package jit_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/lateral/events"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/jit"
)

// recordingPub captures every Publish call for inspection.
type recordingPub struct {
	mu     sync.Mutex
	events []recordedEvent
	err    error
}

type recordedEvent struct {
	topic   string
	source  string
	payload any
}

func (p *recordingPub) Publish(_ context.Context, topic, source string, payload any) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, recordedEvent{topic: topic, source: source, payload: payload})
	return p.err
}

func (p *recordingPub) snapshot() []recordedEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]recordedEvent, len(p.events))
	copy(out, p.events)
	return out
}

func TestEmitProposalFailure_NilPubSafe(t *testing.T) {
	err := jit.EmitProposalFailure(context.Background(), nil, "o1", "https://acme.io/x", errors.New("boom"))
	if err != nil {
		t.Fatalf("nil pub returned err = %v, want nil", err)
	}
}

func TestEmitProposalFailure_NilErrIsNoOp(t *testing.T) {
	pub := &recordingPub{}
	if err := jit.EmitProposalFailure(context.Background(), pub, "o1", "https://acme.io/x", nil); err != nil {
		t.Fatalf("nil err returned err = %v", err)
	}
	if got := pub.snapshot(); len(got) != 0 {
		t.Fatalf("nil err produced %d events, want 0", len(got))
	}
}

func TestEmitProposalFailure_TopicAndMechanism(t *testing.T) {
	pub := &recordingPub{}
	wantErr := errors.New("llm down")
	if err := jit.EmitProposalFailure(context.Background(), pub, "obj-42", "https://acme.io/post/1", wantErr); err != nil {
		t.Fatalf("Publish err = %v", err)
	}

	got := pub.snapshot()
	if len(got) != 1 {
		t.Fatalf("events = %d, want 1", len(got))
	}

	ev := got[0]
	if ev.topic != string(events.ScanFailed) {
		t.Errorf("topic = %q, want %q", ev.topic, events.ScanFailed)
	}
	if ev.source == "" {
		t.Error("source empty, want a non-empty kit event source")
	}

	payload, ok := ev.payload.(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T, want map[string]any", ev.payload)
	}
	if got := payload["object_id"]; got != "obj-42" {
		t.Errorf("object_id = %v, want obj-42", got)
	}
	if got := payload["subject"]; got != "https://acme.io/post/1" {
		t.Errorf("subject = %v, want source URL", got)
	}
	if got := payload["severity"]; got != "error" {
		t.Errorf("severity = %v, want error", got)
	}

	q, ok := payload["qualifiers"].(bus.Qualifiers)
	if !ok {
		t.Fatalf("qualifiers type = %T, want bus.Qualifiers", payload["qualifiers"])
	}
	if q.Mechanism != "jit_proposal" {
		t.Errorf("qualifiers.Mechanism = %q, want %q", q.Mechanism, "jit_proposal")
	}
	if q.Reason != wantErr.Error() {
		t.Errorf("qualifiers.Reason = %q, want %q", q.Reason, wantErr.Error())
	}
}

func TestEmitFetchFailures_NilPubSafe(t *testing.T) {
	got := jit.EmitFetchFailures(context.Background(), nil, "o1", []jit.FetchResult{
		{URL: "https://acme.io/x", Err: errors.New("net")},
	})
	if got != 0 {
		t.Fatalf("nil pub returned %d, want 0", got)
	}
}

func TestEmitFetchFailures_EmptyInputIsNoOp(t *testing.T) {
	pub := &recordingPub{}
	if got := jit.EmitFetchFailures(context.Background(), pub, "o1", nil); got != 0 {
		t.Fatalf("nil failures returned %d, want 0", got)
	}
	if got := jit.EmitFetchFailures(context.Background(), pub, "o1", []jit.FetchResult{}); got != 0 {
		t.Fatalf("empty failures returned %d, want 0", got)
	}
	if got := pub.snapshot(); len(got) != 0 {
		t.Fatalf("emitted %d events on empty input, want 0", len(got))
	}
}

func TestEmitFetchFailures_OneEventPerFailure(t *testing.T) {
	pub := &recordingPub{}
	failures := []jit.FetchResult{
		{URL: "https://acme.io/a", Err: errors.New("net a")},
		{URL: "https://acme.io/b", Err: errors.New("net b")},
		{URL: "https://acme.io/c", Err: nil}, // defensive: should be skipped
	}
	got := jit.EmitFetchFailures(context.Background(), pub, "obj-1", failures)
	if got != 2 {
		t.Fatalf("emitted = %d, want 2 (third has nil Err)", got)
	}

	evs := pub.snapshot()
	if len(evs) != 2 {
		t.Fatalf("publisher saw %d events, want 2", len(evs))
	}
	for i, ev := range evs {
		if ev.topic != string(events.SubpathFailed) {
			t.Errorf("evs[%d].topic = %q, want %q", i, ev.topic, events.SubpathFailed)
		}
		payload, ok := ev.payload.(map[string]any)
		if !ok {
			t.Fatalf("evs[%d] payload type = %T", i, ev.payload)
		}
		if got := payload["object_id"]; got != "obj-1" {
			t.Errorf("evs[%d] object_id = %v, want obj-1", i, got)
		}
		if got := payload["subpath"]; got != failures[i].URL {
			t.Errorf("evs[%d] subpath = %v, want %v", i, got, failures[i].URL)
		}
		if got := payload["reason"]; got != failures[i].Err.Error() {
			t.Errorf("evs[%d] reason = %v, want %v", i, got, failures[i].Err.Error())
		}
		if got := payload["severity"]; got != "error" {
			t.Errorf("evs[%d] severity = %v, want error", i, got)
		}
	}
}

func TestEmitFetchFailures_PublisherErrorDoesNotAbortBatch(t *testing.T) {
	pub := &recordingPub{err: errors.New("bus down")}
	failures := []jit.FetchResult{
		{URL: "https://acme.io/a", Err: errors.New("net")},
		{URL: "https://acme.io/b", Err: errors.New("net")},
	}
	got := jit.EmitFetchFailures(context.Background(), pub, "o1", failures)
	if got != 0 {
		t.Fatalf("emitted = %d, want 0 (all publishes errored)", got)
	}
	// But the publisher should still have been ASKED twice — not aborted.
	if len(pub.snapshot()) != 2 {
		t.Fatalf("publisher invoked %d times, want 2 (must not abort batch on one bus error)", len(pub.snapshot()))
	}
}
