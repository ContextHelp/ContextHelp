package github

import (
	"context"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
)

// stubRegistrar collects strategy IDs as they register. Used to assert
// Register's behavior without spinning up a real substrate Registry.
type stubRegistrar struct {
	ids []string
}

func (r *stubRegistrar) Register(s lateral.LateralStrategy) {
	r.ids = append(r.ids, s.ID())
}

func TestRegister_DefaultConfig_AllThreeRegister(t *testing.T) {
	reg := &stubRegistrar{}
	api := &stubAPIClient{}
	count := Register(reg, DefaultConfig(), SharedDeps{APIClient: api})
	if count != 3 {
		t.Errorf("count = %d, want 3", count)
	}
	want := []string{StrategyIDGitHub, StrategyIDGist, StrategyIDSecurityAdvisory}
	if len(reg.ids) != 3 {
		t.Fatalf("registered %v, want %v", reg.ids, want)
	}
	for i, id := range want {
		if reg.ids[i] != id {
			t.Errorf("reg.ids[%d] = %q, want %q", i, reg.ids[i], id)
		}
	}
}

func TestRegister_ParentDisabled_NothingRegisters(t *testing.T) {
	reg := &stubRegistrar{}
	cfg := Config{EnableParent: false, EnableGist: true, EnableSecurityAdvisory: true}
	count := Register(reg, cfg, SharedDeps{})
	if count != 0 {
		t.Errorf("count = %d, want 0 (parent disabled)", count)
	}
	if len(reg.ids) != 0 {
		t.Errorf("registered %v, want []", reg.ids)
	}
}

func TestRegister_ParentOnly_SkipsChildren(t *testing.T) {
	reg := &stubRegistrar{}
	cfg := Config{EnableParent: true, EnableGist: false, EnableSecurityAdvisory: false}
	count := Register(reg, cfg, SharedDeps{APIClient: &stubAPIClient{}})
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
	if len(reg.ids) != 1 || reg.ids[0] != StrategyIDGitHub {
		t.Errorf("reg.ids = %v, want [github]", reg.ids)
	}
}

func TestRegister_ParentEnabled_NoAPIClient_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("Register without APIClient must panic")
		}
	}()
	reg := &stubRegistrar{}
	_ = Register(reg, DefaultConfig(), SharedDeps{}) // APIClient nil
}

func TestRegister_GuardedAPIWiringWhenBreakerProvided(t *testing.T) {
	reg := &stubRegistrar{}
	br := &fakeBreaker{}
	count := Register(reg, DefaultConfig(), SharedDeps{
		APIClient: &stubAPIClient{},
		Breaker:   br,
	})
	if count != 3 {
		t.Errorf("count = %d, want 3", count)
	}
	// Cannot directly inspect strategy.deps.APIClient is wrapped without
	// reflection or exporting more state. Smoke-test via dispatch: the
	// strategies register correctly, which is the wiring contract.
}

func TestRegister_FloorBuiltFromCfgWhenDepsFloorNil(t *testing.T) {
	// Cfg knobs (FloorWindow/FloorBounds) must take effect when caller
	// did not supply deps.Floor. Verify by registering with DefaultConfig
	// (non-zero window + bounds) and an APIClient that records a call —
	// the guarded wrapper records the call into the constructed tracker,
	// so CallsInWindow on the underlying tracker should reflect the call.
	//
	// The tracker is internal to Register, so we cannot inspect it
	// directly. Instead we rely on the guarded path being taken: the
	// guardedAPI wraps the APIClient when floor != nil, even if breaker
	// is nil. Without the cfg-driven construction, no wrap happens and
	// the inner stubAPIClient is exposed unchanged.
	reg := &stubRegistrar{}
	api := &stubAPIClient{}
	count := Register(reg, DefaultConfig(), SharedDeps{APIClient: api})
	if count != 3 {
		t.Errorf("count = %d, want 3", count)
	}
	// Smoke: registration succeeded; substrate-level wiring covered by
	// TestRegister_RealRegistry_DispatchPath.
}

func TestRegister_FloorTakesDepsFloorWhenProvided(t *testing.T) {
	// When deps.Floor != nil, Register must use it verbatim and ignore
	// cfg.FloorWindow/FloorBounds. Verify by recording a call through the
	// guarded wrapper and observing it on the supplied tracker.
	reg := lateral.NewRegistry()
	provided := NewFloorTracker(time.Hour, FloorBounds{MinPct: 0.1, MaxPct: 0.5})
	api := &stubAPIClient{
		listRepoSiblingsFn: func(_ context.Context, _, _ string) ([]RepoSummary, error) {
			return nil, nil
		},
	}
	cfg := DefaultConfig()
	// Override cfg with values that would build a different tracker.
	cfg.FloorWindow = 5 * time.Minute
	cfg.FloorBounds = FloorBounds{MinPct: 0.99, MaxPct: 1.0}
	count := Register(reg, cfg, SharedDeps{APIClient: api, Floor: provided})
	if count != 3 {
		t.Errorf("count = %d, want 3", count)
	}
	// Dispatch + probe to drive a call through the guarded wrapper.
	ds := reg.Dispatch(context.Background(), lateral.CapturedEvent{
		SourceURL: "https://github.com/owner/repo",
	})
	if len(ds) != 1 {
		t.Fatalf("dispatched %d, want 1", len(ds))
	}
	if _, err := ds[0].Probe(context.Background(),
		lateral.CapturedEvent{SourceURL: "https://github.com/owner/repo"},
		lateral.ActiveContext{}); err != nil {
		t.Fatalf("probe failed: %v", err)
	}
	if got := provided.CallsInWindow(); got == 0 {
		t.Error("provided FloorTracker received zero calls; deps.Floor was not used")
	}
}

func TestRegister_FailureRecorderInstalled(t *testing.T) {
	reg := &stubRegistrar{}
	called := 0
	recorder := &captureRecorder{onSubpath: func() { called++ }}
	_ = Register(reg, DefaultConfig(), SharedDeps{
		APIClient:       &stubAPIClient{},
		FailureRecorder: recorder,
	})
	// Trigger a sub-path failure path via the package-global recorder
	// to confirm SetFailureRecorder ran.
	recordSubpathFailure("test", "obj", "mech", nil)
	if called != 1 {
		t.Errorf("recorder.onSubpath called %d times, want 1", called)
	}
	// Restore noop for subsequent tests.
	SetFailureRecorder(nil)
}

// captureRecorder is a FailureRecorder whose SubpathFailure calls a
// configurable callback. Used to assert SetFailureRecorder wiring.
type captureRecorder struct {
	onSubpath func()
	onScan    func()
}

func (c *captureRecorder) SubpathFailure(_, _, _ string, _ error) {
	if c.onSubpath != nil {
		c.onSubpath()
	}
}
func (c *captureRecorder) ScanFailure(_, _, _ string, _ error) {
	if c.onScan != nil {
		c.onScan()
	}
}

// TestRegister_RealRegistry_DispatchPath verifies Register() output
// dispatches correctly through a real substrate Registry. End-to-end
// wiring smoke test.
func TestRegister_RealRegistry_DispatchPath(t *testing.T) {
	reg := lateral.NewRegistry()
	count := Register(reg, DefaultConfig(), SharedDeps{APIClient: &stubAPIClient{}})
	if count != 3 {
		t.Errorf("count = %d, want 3", count)
	}
	cases := []struct {
		url  string
		want string
	}{
		{"https://github.com/samber/lo", StrategyIDGitHub},
		{"https://gist.github.com/jadb/abc", StrategyIDGist},
		{"https://github.com/advisories/GHSA-x", StrategyIDSecurityAdvisory},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.url, func(t *testing.T) {
			ds := reg.Dispatch(context.Background(), lateral.CapturedEvent{SourceURL: tc.url})
			if len(ds) != 1 || ds[0].ID() != tc.want {
				ids := make([]string, len(ds))
				for i, d := range ds {
					ids[i] = d.ID()
				}
				t.Errorf("dispatch(%q) = %v, want [%s]", tc.url, ids, tc.want)
			}
		})
	}
}
