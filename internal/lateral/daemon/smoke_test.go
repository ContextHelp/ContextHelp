package daemon_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"hop.top/kit/go/runtime/bus"

	lateral "github.com/ideacrafterslabs/ctxt/internal/lateral"
	adapterbus "github.com/ideacrafterslabs/ctxt/internal/lateral/adapters/bus"
	adapterllm "github.com/ideacrafterslabs/ctxt/internal/lateral/adapters/llm"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/daemon"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/github"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/jit"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/roster"

	kitllm "hop.top/kit/go/ai/llm"
)

// TestSmoke_DaemonE2E_JIT proves the full daemon shape composes:
//
//  1. config (T-0313) builds a Config bundle with JIT enabled.
//  2. adapters (T-0314 LLM, T-0315 fetcher, T-0318 bus) plug into the
//     daemon's Deps.
//  3. wiring (T-0320) registers JIT into the registry.
//  4. lifecycle (T-0321) subscribes to the bus and dispatches.
//  5. JIT's pipeline runs end-to-end: stub LLM proposes paths,
//     stub fetcher returns bodies, candidates flow back.
//
// No real network calls, no real LLM. The smoke test runs in-process
// with all-stub deps so it's deterministic and fast (<100ms).
func TestSmoke_DaemonE2E_JIT(t *testing.T) {
	// 1. Bus + publisher.
	b := bus.New(bus.WithEnforce(bus.ModeOff))
	pub := adapterbus.New(b)
	defer b.Close(context.Background())

	// 2. LLM adapter — stub Completer returns a fixed sub-path list.
	stubLLM := &fixedCompleter{content: "/about\n/team\n/blog"}
	proposer := adapterllm.NewProposer(stubLLM, adapterllm.Options{Model: "test-model"})

	// 3. Fetcher — stub that records URLs and returns canned bodies.
	stubFetcher := &recordingFetcher{
		bodies: map[string]string{
			"https://x.example/about": "<html>about</html>",
			"https://x.example/team":  "<html>team</html>",
			"https://x.example/blog":  "<html>blog</html>",
		},
	}

	// 4. Build config with JIT on, every other strategy off.
	cfg := daemon.Config{
		GitHub: github.Config{}, // off
		Roster: allRosterDisabled(),
		JIT:    jit.Config{Enabled: true},
	}

	// 5. Build the registry.
	reg, snap := daemon.Build(cfg, daemon.Deps{
		Publisher:   pub,
		JITProposer: proposer,
		JITCache:    jit.NewMemoryProposalCache(),
		JITFetcher:  stubFetcher,
		JITOutage:   jit.NoOutage(),
	})
	if !snap.JIT {
		t.Fatal("JIT didn't register")
	}
	if snap.Roster != 0 {
		t.Errorf("Roster snapshot = %d; want 0 (all disabled)", snap.Roster)
	}

	// 6. Wire lifecycle and start subscription.
	lc, err := daemon.NewLifecycle(daemon.LifecycleOptions{
		Bus:       b,
		Registry:  reg,
		Publisher: pub,
		WorkerID:  "smoke-test",
	})
	if err != nil {
		t.Fatalf("NewLifecycle err = %v", err)
	}
	if err := lc.Start(context.Background()); err != nil {
		t.Fatalf("Start err = %v", err)
	}
	defer lc.Stop()

	// 7. Set up a subscriber to capture scan.failed events (none
	// expected, but we verify the bus is wired correctly by looking
	// at the candidate emission topic instead — JIT doesn't emit a
	// candidate-found event, so the proof is the absence of failures
	// + the fetcher recording the proposed paths).

	// 8. Publish a CapturedEvent on the bus.
	err = b.Publish(context.Background(), bus.NewEvent(
		bus.Topic(daemon.CapturedEventTopic),
		"smoke-test",
		map[string]any{
			"object_id":  "obj-smoke",
			"namespace":  "default",
			"source_url": "https://x.example/post/1",
		},
	))
	if err != nil {
		t.Fatalf("Publish capture event err = %v", err)
	}

	// 9. Verify the JIT pipeline ran by checking the fetcher saw all
	// three proposed paths.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) && stubFetcher.count() < 3 {
		time.Sleep(10 * time.Millisecond)
	}
	urls := stubFetcher.urls()
	if len(urls) != 3 {
		t.Fatalf("fetcher got %d URLs; want 3 (stubLLM proposed 3 paths). got: %v", len(urls), urls)
	}
	wantURLs := map[string]bool{
		"https://x.example/about": true,
		"https://x.example/team":  true,
		"https://x.example/blog":  true,
	}
	for _, u := range urls {
		if !wantURLs[u] {
			t.Errorf("unexpected fetched URL: %q", u)
		}
	}
}

// TestSmoke_DaemonE2E_Direct exercises the synchronous EnqueueDirect
// path used by tests + status subcommands. Same wiring shape, no
// bus pub/sub timing.
func TestSmoke_DaemonE2E_Direct(t *testing.T) {
	b := bus.New(bus.WithEnforce(bus.ModeOff))
	pub := adapterbus.New(b)
	defer b.Close(context.Background())

	stubLLM := &fixedCompleter{content: "/x"}
	proposer := adapterllm.NewProposer(stubLLM, adapterllm.Options{Model: "test"})
	stubFetcher := &recordingFetcher{
		bodies: map[string]string{"https://example.com/x": "ok"},
	}

	reg, _ := daemon.Build(daemon.Config{
		GitHub: github.Config{},
		Roster: allRosterDisabled(),
		JIT:    jit.Config{Enabled: true},
	}, daemon.Deps{
		Publisher:   pub,
		JITProposer: proposer,
		JITFetcher:  stubFetcher,
	})

	lc, err := daemon.NewLifecycle(daemon.LifecycleOptions{
		Bus: b, Registry: reg, Publisher: pub,
	})
	if err != nil {
		t.Fatalf("NewLifecycle err = %v", err)
	}

	err = lc.EnqueueDirect(context.Background(), lateral.CapturedEvent{
		ObjectID:  "obj-direct",
		SourceURL: "https://example.com/index",
	})
	if err != nil {
		t.Fatalf("EnqueueDirect err = %v", err)
	}
	if stubFetcher.count() != 1 {
		t.Errorf("fetcher count = %d; want 1", stubFetcher.count())
	}
}

// allRosterDisabled returns a roster.Gates with every flag set to
// false, ensuring only JIT fires in smoke-test scenarios.
func allRosterDisabled() roster.Gates {
	off := func() *bool { v := false; return &v }
	return roster.Gates{
		GoogleStrategy: off(), GoogleSearchStrategy: off(),
		GoogleScholarStrategy: off(), GoogleTrendsStrategy: off(),
		GoogleNewsStrategy: off(), XStrategy: off(),
		LinkedInStrategy: off(), ArxivStrategy: off(),
		WikipediaStrategy: off(), MediumStrategy: off(),
		MediumPublicationStrategy: off(), MediumProfileStrategy: off(),
		SubstackStrategy: off(), SubstackPublicationStrategy: off(),
		SubstackPostStrategy: off(), SubstackNotesStrategy: off(),
		BeehiivStrategy: off(), BeehiivPublicationStrategy: off(),
		BeehiivPostStrategy: off(), YouTubeStrategy: off(),
	}
}

// fixedCompleter returns the same content on every call. Sufficient
// for adapter-shape smoke tests; production wires a real Completer.
type fixedCompleter struct {
	content string
}

func (f *fixedCompleter) Complete(_ context.Context, _ kitllm.Request) (kitllm.Response, error) {
	return kitllm.Response{Content: f.content}, nil
}

// recordingFetcher tracks fetched URLs so the smoke test can assert
// the JIT pipeline reached all proposed paths.
type recordingFetcher struct {
	mu     sync.Mutex
	bodies map[string]string
	seen   []string
}

func (r *recordingFetcher) Fetch(_ context.Context, url string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = append(r.seen, url)
	return r.bodies[url], nil
}

func (r *recordingFetcher) urls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.seen))
	copy(out, r.seen)
	return out
}

func (r *recordingFetcher) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.seen)
}
