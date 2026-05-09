package eval

import (
	"context"
	"testing"
	"time"

	"hop.top/kit/go/runtime/bus"
)

func TestConversionAggregator_TracksEmitsAndPromotions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	a := NewConversionAggregator(AggregatorOptions{
		Window: 14 * 24 * time.Hour,
		Now:    func() time.Time { return now },
	})
	b := bus.New(bus.WithEnforce(bus.ModeOff))
	defer b.Close(context.Background())
	cancels := a.Subscribe(b)
	defer func() {
		for _, c := range cancels {
			c()
		}
	}()

	// Publish 3 created + 1 promoted for "github".
	for i := 0; i < 3; i++ {
		_ = b.Publish(context.Background(),
			bus.NewEvent("ctxt.lateral.candidate.created", "test",
				map[string]any{"strategy_id": "github"}))
	}
	_ = b.Publish(context.Background(),
		bus.NewEvent("ctxt.lateral.candidate.promoted", "test",
			map[string]any{"strategy_id": "github"}))

	// Publish 2 created for "jit", 0 promoted.
	for i := 0; i < 2; i++ {
		_ = b.Publish(context.Background(),
			bus.NewEvent("ctxt.lateral.candidate.created", "test",
				map[string]any{"strategy_id": "jit"}))
	}

	snap := a.Snapshot()
	gh := snap["github"]
	if gh.Emitted != 3 || gh.Promoted != 1 {
		t.Errorf("github = %+v; want emitted=3 promoted=1", gh)
	}
	if gh.Rate < 0.33 || gh.Rate > 0.34 {
		t.Errorf("github rate = %v; want ~0.333", gh.Rate)
	}
	jit := snap["jit"]
	if jit.Emitted != 2 || jit.Promoted != 0 {
		t.Errorf("jit = %+v; want emitted=2 promoted=0", jit)
	}
	if jit.Rate != 0 {
		t.Errorf("jit rate = %v; want 0 on zero promotions", jit.Rate)
	}
}

func TestConversionAggregator_TrimsOutOfWindow(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	cur := now
	a := NewConversionAggregator(AggregatorOptions{
		Window: 1 * time.Hour,
		Now:    func() time.Time { return cur },
	})
	b := bus.New(bus.WithEnforce(bus.ModeOff))
	defer b.Close(context.Background())
	cancels := a.Subscribe(b)
	defer func() {
		for _, c := range cancels {
			c()
		}
	}()

	// At t=0: emit 1, promote 1.
	_ = b.Publish(context.Background(),
		bus.NewEvent("ctxt.lateral.candidate.created", "test",
			map[string]any{"strategy_id": "x"}))
	_ = b.Publish(context.Background(),
		bus.NewEvent("ctxt.lateral.candidate.promoted", "test",
			map[string]any{"strategy_id": "x"}))

	// Advance clock past the window.
	cur = now.Add(2 * time.Hour)
	snap := a.Snapshot()
	if snap["x"].Emitted != 0 || snap["x"].Promoted != 0 {
		t.Errorf("expected window-trimmed counts; got %+v", snap["x"])
	}
}

func TestConversionAggregator_IgnoresMalformedPayload(t *testing.T) {
	t.Parallel()

	a := NewConversionAggregator(AggregatorOptions{})
	b := bus.New(bus.WithEnforce(bus.ModeOff))
	defer b.Close(context.Background())
	a.Subscribe(b)

	// Wrong shape — string payload, no map. Aggregator must drop.
	_ = b.Publish(context.Background(),
		bus.NewEvent("ctxt.lateral.candidate.created", "test", "not-a-map"))
	// Map without strategy_id.
	_ = b.Publish(context.Background(),
		bus.NewEvent("ctxt.lateral.candidate.created", "test",
			map[string]any{"foo": 1}))

	snap := a.Snapshot()
	if len(snap) != 0 {
		t.Errorf("expected empty snapshot on malformed payloads; got %v", snap)
	}
}
