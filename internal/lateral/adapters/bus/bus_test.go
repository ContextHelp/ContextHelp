package bus

import (
	"context"
	"errors"
	"testing"

	kitbus "hop.top/kit/go/runtime/bus"
)

type stubPub struct {
	last kitbus.Event
	err  error
}

func (s *stubPub) Publish(_ context.Context, e kitbus.Event) error {
	s.last = e
	return s.err
}

func TestPublish_BuildsEventWithExpectedFields(t *testing.T) {
	p := &stubPub{}
	a := New(p)
	err := a.Publish(context.Background(), "ctxt.lateral.test.action", "lateral.test", map[string]any{"k": 1})
	if err != nil {
		t.Fatalf("Publish err = %v", err)
	}
	if string(p.last.Topic) != "ctxt.lateral.test.action" {
		t.Errorf("Topic = %q", p.last.Topic)
	}
	if p.last.Source != "lateral.test" {
		t.Errorf("Source = %q", p.last.Source)
	}
	if m, ok := p.last.Payload.(map[string]any); !ok || m["k"] != 1 {
		t.Errorf("Payload = %v; want map[k:1]", p.last.Payload)
	}
}

func TestPublish_PropagatesError(t *testing.T) {
	want := errors.New("validation failed")
	p := &stubPub{err: want}
	a := New(p)
	err := a.Publish(context.Background(), "ctxt.lateral.test.action", "lateral.test", nil)
	if !errors.Is(err, want) {
		t.Errorf("err = %v; want %v", err, want)
	}
}

func TestNew_PanicsOnNil(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on nil publisher")
		}
	}()
	_ = New(nil)
}

// Real-bus integration: confirm the adapter satisfies the
// kitdomain.EventPublisher interface kit's domain types consume, by
// publishing through an actual in-memory bus and observing the
// subscriber.
func TestPublish_RealBusRoundTrip(t *testing.T) {
	b := kitbus.New(kitbus.WithEnforce(kitbus.ModeOff))
	a := New(b)
	got := make(chan kitbus.Event, 1)
	b.Subscribe("ctxt.lateral.test.*", func(_ context.Context, e kitbus.Event) error {
		got <- e
		return nil
	})
	if err := a.Publish(context.Background(), "ctxt.lateral.test.roundtrip", "lateral.test", "hi"); err != nil {
		t.Fatalf("Publish err = %v", err)
	}
	select {
	case e := <-got:
		if string(e.Topic) != "ctxt.lateral.test.roundtrip" {
			t.Errorf("got topic %q", e.Topic)
		}
	default:
		t.Fatal("subscriber didn't receive event")
	}
	_ = b.Close(context.Background())
}
