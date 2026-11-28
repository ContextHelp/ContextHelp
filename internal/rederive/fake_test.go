package rederive

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/events"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/indexsig"
	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

// Signatures and entries of the fake pipeline "alpha": version 2 of step
// "b" is the current one.
const (
	alphaOld = "alpha#0000000000000001"
	alphaNew = "alpha#0000000000000002"
)

var (
	alphaOldEntries = []string{"a@1", "b@1"}
	alphaNewEntries = []string{"a@1", "b@2"}
)

// fakeSvc is a Reanalyzer that answers from tables and records its calls.
type fakeSvc struct {
	mu    sync.Mutex
	plans map[string]service.ReanalysisPlan // by pipeline name and "net" suffix
	perrs map[string]error
	// result decides the outcome of a re-derive; nil means success.
	result func(id string, opts service.ReanalyzeOptions) error
	calls  []fakeCall
	// localityCalls counts ReanalysisLocality calls: a survey asks once
	// per group, whatever the rows.
	localityCalls int
	panicLocality bool
	// counts are the provider calls a successful re-derive reports it made.
	counts map[string]int
	// countsFor, when set, gives the calls a successful object made.
	countsFor func(id string) map[string]int
	// fetched are the hosts a successful re-derive reports it fetched from;
	// fetchedFor, when set, decides per object.
	fetched    []string
	fetchedFor func(id string) []string
	// onCall runs inside each re-derive, before it returns.
	onCall func(id string)
}

type fakeCall struct {
	ID      string
	Network bool
	Guarded bool
}

func planKey(name string, network bool) string {
	if network {
		return name + "/net"
	}
	return name
}

func (f *fakeSvc) ReanalysisLocality(_ context.Context, name string, opts service.ReanalyzeOptions) (service.ReanalysisPlan, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.localityCalls++
	if f.panicLocality {
		panic("locality boom")
	}
	k := planKey(name, opts.AllowNetwork)
	if err := f.perrs[k]; err != nil {
		return service.ReanalysisPlan{}, err
	}
	return f.plans[k], nil
}

func (f *fakeSvc) ReanalyzeObjectWith(_ context.Context, id string, opts service.ReanalyzeOptions) (string, float64, error) {
	f.mu.Lock()
	f.calls = append(f.calls, fakeCall{ID: id, Network: opts.AllowNetwork, Guarded: opts.Accept != nil})
	result, onCall := f.result, f.onCall
	f.mu.Unlock()
	if onCall != nil {
		onCall(id)
	}
	if result != nil {
		if err := result(id, opts); err != nil {
			return "", 0, err
		}
	}
	if opts.Calls != nil {
		if f.countsFor != nil {
			opts.Calls(f.countsFor(id))
		} else {
			opts.Calls(f.counts)
		}
	}
	if opts.Fetched != nil {
		hosts := f.fetched
		if f.fetchedFor != nil {
			hosts = f.fetchedFor(id)
		}
		opts.Fetched(hosts)
	}
	return "alpha@v1", 0, nil
}

func (f *fakeSvc) called() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var ids []string
	for _, c := range f.calls {
		ids = append(ids, c.ID)
	}
	return ids
}

// localPlan is a plan that may run unasked and runs the given steps.
func localPlan(steps ...string) service.ReanalysisPlan {
	return service.ReanalysisPlan{
		LocalityReport: pipeline.LocalityReport{Locality: pipeline.LocalityNone},
		Pipeline:       "alpha@v1",
		Steps:          steps,
	}
}

// fixture is a store with objects, the registry and a fake service.
type fixture struct {
	t     *testing.T
	drv   storage.StorageDriver
	store Store
	reg   pipeline.Registry
	svc   *fakeSvc
	job   *Job
	sleep []time.Duration // what the job asked to wait, in order
}

// newFixture registers pipeline "alpha" at its current signature, records
// the entries of the old one, and builds a Job over a real SQLite store.
// Waiting is recorded, never done.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	drv := storageutil.NewTestDriver(t)
	store, ok := drv.Objects().(Store)
	if !ok {
		t.Fatalf("%T is not a rederive.Store", drv.Objects())
	}
	reg := pipeline.NewRegistry()
	if err := reg.Register("alpha", &pipeline.Pipeline{
		PipelineName: "alpha", Signature: alphaNew, Entries: alphaNewEntries,
		BaselineSignature: alphaOld, BaselineEntries: alphaOldEntries,
	}); err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, drv: drv, store: store, reg: reg, svc: &fakeSvc{
		plans: map[string]service.ReanalysisPlan{"alpha": localPlan("a", "b"), "alpha/net": localPlan("fetch", "a", "b")},
		perrs: map[string]error{},
	}}
	ctx := context.Background()
	if err := store.UpsertDerivationSignature(ctx, alphaOld, "alpha", alphaOldEntries); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertDerivationSignature(ctx, alphaNew, "alpha", alphaNewEntries); err != nil {
		t.Fatal(err)
	}
	f.job = &Job{
		Store: store, Svc: f.svc, Registry: reg,
		Enabled: true,
		Wait:    func(_ context.Context, d time.Duration) error { f.sleep = append(f.sleep, d); return nil },
		Scope: func(ctx context.Context) context.Context {
			return sharing.WithViewer(ctx, sharing.Unrestricted)
		},
	}
	return f
}

// add stores an object of pipeline stamp pipe carrying signature sig.
func (f *fixture) add(id, pipe, sig string) {
	f.t.Helper()
	obj := storagetest.ProjectionFixture(id, "rederive "+id)
	obj.Pipeline, obj.DerivationSig = pipe, sig
	if err := f.drv.Objects().Create(context.Background(), obj); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) sigOf(id string) string {
	return storagetest.ProjectionDB{DB: sqliteDB(f.t, f.drv), Dialect: indexsig.DialectSQLite}.DerivationSig(f.t, id)
}

func (f *fixture) skips(target string) map[string]string {
	f.t.Helper()
	got := skipRows(f.t, f.drv, target) // fails the test itself on a read error
	out := map[string]string{}
	for _, k := range got {
		out[k.ObjectID] = k.Reason
	}
	return out
}

func (f *fixture) run() (Result, error) {
	f.t.Helper()
	return f.job.Run(context.Background(), "job-1")
}

// stale counts what a run would still pick up.
func (f *fixture) stale() int {
	f.t.Helper()
	return staleObjects(f.t, f.store, f.reg)
}

// recordingBus remembers the events published on it.
type recordingBus struct {
	bus.Bus
	mu     sync.Mutex
	topics []bus.Topic
	events []events.RederivePayload
}

func (b *recordingBus) Publish(_ context.Context, ev bus.Event) error {
	b.mu.Lock()
	b.topics = append(b.topics, ev.Topic)
	if p, ok := ev.Payload.(events.RederivePayload); ok {
		b.events = append(b.events, p)
	}
	b.mu.Unlock()
	return nil
}

// fakeProgress is a Progress that records what it was told; busy holds
// StartTarget refusing until released.
type fakeProgress struct {
	mu       sync.Mutex
	busy     int // StartTarget fails with ErrBusy this many times
	started  []string
	total    int
	ticks    [][2]int
	deferred []upgrade.Deferred
	skipped  map[string]int
	stopped  []upgrade.Stopped
	done     *upgrade.RunSummary
	failed   error
}

func (p *fakeProgress) StartTarget(_ upgrade.Bucket, target string, total int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.busy > 0 {
		p.busy--
		return fmt.Errorf("%w: reproject", upgrade.ErrBusy)
	}
	p.started, p.total = append(p.started, target), total
	return nil
}
func (p *fakeProgress) TickFailed(done, failed int) error {
	p.mu.Lock()
	p.ticks = append(p.ticks, [2]int{done, failed})
	p.mu.Unlock()
	return nil
}
func (p *fakeProgress) SetDeferred(d []upgrade.Deferred) error {
	p.mu.Lock()
	p.deferred = d
	p.mu.Unlock()
	return nil
}
func (p *fakeProgress) SetStopped(st []upgrade.Stopped) error {
	p.mu.Lock()
	p.stopped = st
	p.mu.Unlock()
	return nil
}
func (p *fakeProgress) SetSkipped(c map[string]int) error {
	p.mu.Lock()
	p.skipped = c
	p.mu.Unlock()
	return nil
}
func (p *fakeProgress) CompleteRun(s upgrade.RunSummary) error {
	p.mu.Lock()
	p.done = &s
	p.mu.Unlock()
	return nil
}
func (p *fakeProgress) Fail(err error) error {
	p.mu.Lock()
	p.failed = err
	p.mu.Unlock()
	return nil
}

// staleObjects counts the stale rows not skipped for a current signature.
func staleObjects(t *testing.T, store storage.RederiveStore, reg pipeline.Registry) int {
	t.Helper()
	groups, err := store.ListStaleGroups(context.Background(), pipeline.CurrentSignatures(reg))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, g := range groups {
		n += g.Objects
	}
	return n
}

// useLLM makes the fixture's pipeline one whose successful re-derives call
// the LLM: the calls a success reports, which are what prove it works.
func (f *fixture) useLLM() {
	f.svc.plans["alpha"] = llmPlan()
	f.svc.counts = map[string]int{"llm": 1}
}

// stoppedKind reports whether the run stopped exactly one group, for that
// kind of failure (llm, fetch, unknown, "llm unusable reply", ...).
func stoppedKind(res Result, kind string) bool {
	return len(res.Stopped) == 1 && res.Stopped[0].Kind == kind
}
