package daemon_test

import (
	"context"
	"testing"

	"hop.top/kit/go/runtime/bus"

	lateral "github.com/ideacrafterslabs/ctxt/internal/lateral"
	adapterbus "github.com/ideacrafterslabs/ctxt/internal/lateral/adapters/bus"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/daemon"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/rollout"
)

// TestLifecycle_GateSkipsDisabledStrategy proves Lifecycle's
// per-strategy gate cuts off Probe invocations when a strategy is
// flipped to disabled mid-run. Substrate Registry registration is
// untouched; only Dispatch-time filtering changes.
func TestLifecycle_GateSkipsDisabledStrategy(t *testing.T) {
	t.Parallel()

	reg := lateral.NewRegistry()
	st := &countingStrategy{id: "test", family: lateral.FamilyPlatform}
	reg.Register(st)

	b := bus.New(bus.WithEnforce(bus.ModeOff))
	defer b.Close(context.Background())
	pub := adapterbus.New(b)

	gate := rollout.NewStrategyGate()
	lc, err := daemon.NewLifecycle(daemon.LifecycleOptions{
		Bus: b, Registry: reg, Publisher: pub, Gate: gate,
	})
	if err != nil {
		t.Fatalf("NewLifecycle err = %v", err)
	}

	// Default: gate empty → strategy fires.
	if err := lc.EnqueueDirect(context.Background(), lateral.CapturedEvent{ObjectID: "1", SourceURL: "https://x"}); err != nil {
		t.Fatalf("first EnqueueDirect err = %v", err)
	}
	if st.calls != 1 {
		t.Fatalf("first dispatch: probe calls = %d; want 1", st.calls)
	}

	// Flip the strategy off — subsequent dispatches must skip.
	gate.Set("test", false)
	if err := lc.EnqueueDirect(context.Background(), lateral.CapturedEvent{ObjectID: "2", SourceURL: "https://x"}); err != nil {
		t.Fatalf("second EnqueueDirect err = %v", err)
	}
	if st.calls != 1 {
		t.Errorf("second dispatch: probe calls = %d; want still 1 (gate off)", st.calls)
	}

	// Re-enable — strategy fires again.
	gate.Set("test", true)
	if err := lc.EnqueueDirect(context.Background(), lateral.CapturedEvent{ObjectID: "3", SourceURL: "https://x"}); err != nil {
		t.Fatalf("third EnqueueDirect err = %v", err)
	}
	if st.calls != 2 {
		t.Errorf("third dispatch: probe calls = %d; want 2 after re-enable", st.calls)
	}
}

// countingStrategy claims any event and counts probe calls. Test-only.
type countingStrategy struct {
	id     string
	family lateral.StrategyFamily
	calls  int
}

func (s *countingStrategy) ID() string                                   { return s.id }
func (s *countingStrategy) Family() lateral.StrategyFamily               { return s.family }
func (s *countingStrategy) Applies(_ context.Context, _ lateral.CapturedEvent) lateral.AppliesResult {
	return lateral.AppliesResult{Matches: true, Specificity: 1}
}
func (s *countingStrategy) Probe(_ context.Context, _ lateral.CapturedEvent, _ lateral.ActiveContext) ([]lateral.Candidate, error) {
	s.calls++
	return nil, nil
}
func (*countingStrategy) Preconditions() []string { return nil }
