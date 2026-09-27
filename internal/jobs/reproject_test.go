package jobs

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/events"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/indexsig"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

// The re-projection scenarios take a driver and its ProjectionDB and run
// the same checks on SQLite here and on Postgres under the integration tag
// (reproject_pg_test.go).

func sqliteReprojectDriver(t *testing.T) (storage.StorageDriver, storagetest.ProjectionDB) {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "reproject.db") + "?_pragma=busy_timeout(5000)"
	d, err := sqlite.New(dsn)
	require.NoError(t, err)
	require.NoError(t, d.Init(context.Background()))
	t.Cleanup(func() { d.Close(context.Background()) })
	return d, storagetest.ProjectionDB{DB: d.DB(), Dialect: indexsig.DialectSQLite}
}

func TestReproject_StaleDatabaseOnStart_SQLite(t *testing.T) {
	drv, pdb := sqliteReprojectDriver(t)
	runReprojectStaleDatabaseOnStart(t, drv, pdb)
}

func TestReproject_ResumesAfterCrash_SQLite(t *testing.T) {
	drv, pdb := sqliteReprojectDriver(t)
	runReprojectResumesAfterCrash(t, drv, pdb)
}

func TestReproject_IngestDuringRun_SQLite(t *testing.T) {
	drv, pdb := sqliteReprojectDriver(t)
	runReprojectIngestDuringRun(t, drv, pdb)
}

// recordingBus records published event types.
type recordingBus struct {
	mu    sync.Mutex
	types []string
}

func (b *recordingBus) Publish(_ context.Context, e events.Event) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.types = append(b.types, e.Type)
	return nil
}
func (b *recordingBus) Subscribe(string, events.Handler) {}
func (b *recordingBus) Close() error                     { return nil }

func (b *recordingBus) has(topic string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, t := range b.types {
		if t == topic {
			return true
		}
	}
	return false
}

// hookStore wraps a ProjectionStore, counting Reproject calls and running
// hook before the n-th (1-based).
type hookStore struct {
	storage.ProjectionStore
	calls atomic.Int32
	hook  func(n int, id string)
}

func (h *hookStore) Reproject(ctx context.Context, id string) (bool, error) {
	n := int(h.calls.Add(1))
	if h.hook != nil {
		h.hook(n, id)
	}
	return h.ProjectionStore.Reproject(ctx, id)
}

// seedStale creates n objects and rewinds each to an older projection. It
// also rewinds the stored FTS signature, as a release that bumped
// indexsig.ProjectionVersion would find it.
func seedStale(t *testing.T, drv storage.StorageDriver, pdb storagetest.ProjectionDB, n int) []string {
	t.Helper()
	ctx := context.Background()
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("obj-%02d", i)
		obj := storagetest.ProjectionFixture(ids[i], fmt.Sprintf("stored object %d", i))
		require.NoError(t, drv.Objects().Create(ctx, obj))
		pdb.Stale(t, ids[i], storagetest.StaleBody(obj), "v1")
	}
	require.NoError(t, indexsig.Upsert(ctx, pdb.DB, pdb.Dialect, indexsig.FTSSignatureID, "stamped-by-v1", "projection=v1"))
	return ids
}

// assertProjected checks every object's stored body is its current
// projection and the signature verifies.
func assertProjected(t *testing.T, drv storage.StorageDriver, pdb storagetest.ProjectionDB) {
	t.Helper()
	ctx := context.Background()
	objs, _, err := drv.Objects().List(ctx, storage.ObjectFilter{Limit: 1000})
	require.NoError(t, err)
	for _, o := range objs {
		full, err := drv.Objects().Get(ctx, o.ID)
		require.NoError(t, err)
		require.Equal(t, projection.ProjectIndex(full).FTSBody, pdb.Body(t, o.ID), "body of %s", o.ID)
	}
	n, err := drv.Objects().(storage.ProjectionStore).CountStaleProjections(ctx)
	require.NoError(t, err)
	require.Zero(t, n, "stale objects left")
	storagetest.AssertFTS(t, drv, storagetest.LegacyToken, "[]")
	res, err := indexsig.VerifyFTS(ctx, pdb.DB, pdb.Dialect)
	require.NoError(t, err)
	require.True(t, res.Match, "FTS signature not stamped: %+v", res)
}

func signatureMatches(t *testing.T, pdb storagetest.ProjectionDB) bool {
	t.Helper()
	res, err := indexsig.VerifyFTS(context.Background(), pdb.DB, pdb.Dialect)
	require.NoError(t, err)
	return res.Match
}

// runReprojectStaleDatabaseOnStart is the startup path: a database
// stamped by an older projection is verified, the job scheduled, and the
// worker pool runs it to completion.
func runReprojectStaleDatabaseOnStart(t *testing.T, drv storage.StorageDriver, pdb storagetest.ProjectionDB) {
	ctx := context.Background()
	ids := seedStale(t, drv, pdb, 7)
	storagetest.AssertFTS(t, drv, storagetest.LegacyToken, fmt.Sprint(ids))

	shadow := filepath.Join(t.TempDir(), "upgrade-state.json")
	mgr := upgrade.NewManager(shadow)
	bus := &recordingBus{}
	q := NewQueue(drv.Jobs())

	res, err := indexsig.VerifyFTS(ctx, pdb.DB, pdb.Dialect)
	require.NoError(t, err)
	require.False(t, res.Match)
	job, err := ScheduleReprojection(ctx, q, res, bus)
	require.NoError(t, err)
	require.NotNil(t, job, "mismatch scheduled nothing")
	again, err := ScheduleReprojection(ctx, q, res, bus)
	require.NoError(t, err)
	require.NotNil(t, again)
	require.Equal(t, job.ID, again.ID, "second start scheduled a duplicate job")

	r, err := NewReprojector(drv, mgr, bus)
	require.NoError(t, err)
	var sawStatus atomic.Bool
	r.Store = &hookStore{ProjectionStore: r.Store, hook: func(n int, _ string) {
		if n != 3 {
			return
		}
		st := mgr.Snapshot()
		disk, _, _ := upgrade.ReadShadow(shadow)
		if st.State == upgrade.StateInProgress && st.Bucket == upgrade.BucketReindexAuto &&
			st.Target == "projection@"+indexsig.ProjectionVersion && st.Total == len(ids) &&
			disk.Bucket == upgrade.BucketReindexAuto {
			sawStatus.Store(true)
		}
	}}
	pool := NewWorkerPool(q, builtins.Registry(), drv, 2, bus, defaultTestJobsCfg())
	pool.Handle(ReprojectJobType, r.Handle)
	pctx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- pool.Start(pctx) }()
	require.Eventually(t, func() bool {
		j, err := q.Get(ctx, job.ID)
		return err == nil && j.Status == storage.JobCompleted
	}, 20*time.Second, 20*time.Millisecond, "re-projection job never completed")
	cancel()
	<-done

	require.True(t, sawStatus.Load(), "upgrade status never showed the run")
	require.Equal(t, upgrade.StateIdle, mgr.Snapshot().State)
	assertProjected(t, drv, pdb)
	storagetest.AssertFTS(t, drv, "quokka", fmt.Sprint(ids))
	for _, topic := range []string{
		string(events.TopicDpkmsUpgradeReprojectionScheduled),
		string(events.TopicDpkmsUpgradeReprojectionStarted),
		string(events.TopicDpkmsUpgradeReprojectionCompleted),
	} {
		require.True(t, bus.has(topic), "missing event %s (got %v)", topic, bus.types)
	}

	// The next start verifies and schedules nothing.
	res, err = indexsig.VerifyFTS(ctx, pdb.DB, pdb.Dialect)
	require.NoError(t, err)
	job, err = ScheduleReprojection(ctx, q, res, bus)
	require.NoError(t, err)
	require.Nil(t, job)
}

// errCrash stands in for the process dying mid-run.
var errCrash = errors.New("crash")

// runReprojectResumesAfterCrash stops a run part-way and checks the next
// run re-projects only what is left, and that the signature is stamped
// only by the run that finishes.
func runReprojectResumesAfterCrash(t *testing.T, drv storage.StorageDriver, pdb storagetest.ProjectionDB) {
	const total, crashAfter = 10, 4
	seedStale(t, drv, pdb, total)
	mgr := upgrade.NewManager("")
	bus := &recordingBus{}

	// A start verifies first: the mismatch must survive it.
	res, err := indexsig.VerifyFTS(context.Background(), pdb.DB, pdb.Dialect)
	require.NoError(t, err)
	require.False(t, res.Match)

	r, err := NewReprojector(drv, mgr, bus)
	require.NoError(t, err)
	r.Batch = 3
	inner := r.Store
	ctx, cancel := context.WithCancelCause(context.Background())
	first := &hookStore{ProjectionStore: inner, hook: func(n int, _ string) {
		if n == crashAfter+1 {
			cancel(errCrash)
		}
	}}
	r.Store = first
	_, err = r.Run(ctx, "job-1")
	require.ErrorIs(t, err, errCrash)
	require.False(t, signatureMatches(t, pdb), "signature stamped before the run finished")
	require.Equal(t, upgrade.StateFailed, mgr.Snapshot().State)
	require.True(t, bus.has(string(events.TopicDpkmsUpgradeReprojectionFailed)))
	stale, err := inner.CountStaleProjections(context.Background())
	require.NoError(t, err)
	require.Equal(t, total-crashAfter, stale, "rows re-projected before the crash")

	second := &hookStore{ProjectionStore: inner}
	r.Store = second
	out, err := r.Run(context.Background(), "job-1")
	require.NoError(t, err)
	require.Equal(t, int32(total-crashAfter), second.calls.Load(), "resume redid finished objects")
	require.Equal(t, total-crashAfter, out.Reprojected)
	require.Equal(t, upgrade.StateIdle, mgr.Snapshot().State)
	assertProjected(t, drv, pdb)
}

// runReprojectIngestDuringRun writes objects while the job runs: new
// objects, and an update to a stale object the job has not reached. None
// may end up stale, reverted, or missing from FTS.
func runReprojectIngestDuringRun(t *testing.T, drv storage.StorageDriver, pdb storagetest.ProjectionDB) {
	ctx := context.Background()
	ids := seedStale(t, drv, pdb, 20)
	r, err := NewReprojector(drv, upgrade.NewManager(""), &recordingBus{})
	require.NoError(t, err)
	r.Batch = 5

	// Background ingest for the whole run (the race detector's target).
	stop := make(chan struct{})
	var wg sync.WaitGroup
	var created []string
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			id := fmt.Sprintf("bg-%03d", i)
			if err := drv.Objects().Create(ctx, storagetest.ProjectionFixture(id, "background ingest")); err != nil {
				t.Errorf("background create: %v", err)
				return
			}
			created = append(created, id)
			time.Sleep(time.Millisecond)
		}
	}()

	const updated = "obj-15"
	r.Store = &hookStore{ProjectionStore: r.Store, hook: func(n int, _ string) {
		if n != 3 {
			return
		}
		require.NoError(t, drv.Objects().Create(ctx, storagetest.ProjectionFixture("fg-new", "foreground ingest")))
		obj, err := drv.Objects().Get(ctx, updated)
		require.NoError(t, err)
		obj.TextContent = "quokka freshtoken rewritten"
		obj.RawContent = obj.TextContent
		obj.Summaries = []string{obj.TextContent}
		require.NoError(t, drv.Objects().Update(ctx, obj))
	}}
	_, err = r.Run(ctx, "job-ingest")
	close(stop)
	wg.Wait()
	require.NoError(t, err)

	assertProjected(t, drv, pdb)
	storagetest.AssertFTS(t, drv, "freshtoken", "["+updated+"]")
	all := append(append([]string{}, ids...), "fg-new")
	all = append(all, created...)
	sort.Strings(all)
	storagetest.AssertFTS(t, drv, "quokka", fmt.Sprint(all))
	require.True(t, strings.Contains(pdb.Body(t, updated), "freshtoken"), "update reverted by re-projection")
}

// fakeProjections is an in-memory ProjectionStore for the run's edge
// cases: per-object failures and an empty corpus.
type fakeProjections struct {
	mu    sync.Mutex
	stale map[string]bool
	fail  map[string]bool
}

func (f *fakeProjections) CountStaleProjections(context.Context) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, s := range f.stale {
		if s {
			n++
		}
	}
	return n, nil
}

func (f *fakeProjections) ListStaleProjections(_ context.Context, after string, limit int) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var ids []string
	for id, s := range f.stale {
		if s && id > after {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if len(ids) > limit {
		ids = ids[:limit]
	}
	return ids, nil
}

func (f *fakeProjections) Reproject(_ context.Context, id string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail[id] {
		return false, fmt.Errorf("corrupt row %s", id)
	}
	was := f.stale[id]
	f.stale[id] = false
	return was, nil
}

func TestReproject_ObjectFailureLeavesSignatureUnstamped(t *testing.T) {
	store := &fakeProjections{
		stale: map[string]bool{"a": true, "b": true, "c": true},
		fail:  map[string]bool{"b": true},
	}
	var stamped atomic.Int32
	mgr := upgrade.NewManager("")
	bus := &recordingBus{}
	r := &Reprojector{Store: store, Stamp: func(context.Context) error { stamped.Add(1); return nil }, Progress: mgr, Bus: bus}
	out, err := r.Run(context.Background(), "job")
	require.NoError(t, err)
	require.Equal(t, 2, out.Reprojected)
	require.Equal(t, 1, out.Failed)
	require.Equal(t, []string{"b"}, out.FailedObjects)
	require.Zero(t, stamped.Load(), "signature stamped with a stale object left")
	st := mgr.Snapshot()
	require.Equal(t, upgrade.StateFailed, st.State)
	require.Equal(t, 1, st.Failed)
	require.Contains(t, st.LastError, "corrupt row b")
	require.True(t, bus.has(string(events.TopicDpkmsUpgradeReprojectionFailed)))
	require.False(t, bus.has(string(events.TopicDpkmsUpgradeReprojectionCompleted)))
}

func TestReproject_NothingStaleStampsSignature(t *testing.T) {
	var stamped atomic.Int32
	mgr := upgrade.NewManager("")
	r := &Reprojector{Store: &fakeProjections{stale: map[string]bool{}}, Stamp: func(context.Context) error { stamped.Add(1); return nil }, Progress: mgr}
	out, err := r.Run(context.Background(), "job")
	require.NoError(t, err)
	require.Zero(t, out.Total)
	require.Equal(t, int32(1), stamped.Load())
	require.Equal(t, upgrade.StateIdle, mgr.Snapshot().State)
}

// Another upgrade run (an embedding migration) holds the status: the
// re-projection waits for it instead of failing.
func TestReproject_WaitsForAnotherUpgradeRun(t *testing.T) {
	mgr := upgrade.NewManager("")
	require.NoError(t, mgr.StartTarget(upgrade.BucketEmbeddingsMigrate, "m@1", 1))
	var stamped atomic.Int32
	r := &Reprojector{
		Store:    &fakeProjections{stale: map[string]bool{"a": true}},
		Stamp:    func(context.Context) error { stamped.Add(1); return nil },
		Progress: mgr,
		BusyPoll: 5 * time.Millisecond,
	}
	done := make(chan error, 1)
	go func() {
		_, err := r.Run(context.Background(), "job")
		done <- err
	}()
	time.Sleep(50 * time.Millisecond)
	require.Zero(t, stamped.Load(), "ran while another upgrade held the status")
	require.NoError(t, mgr.Complete())
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("re-projection never started after the other run finished")
	}
	require.Equal(t, int32(1), stamped.Load())
}

func TestScheduleReprojection_OnlyOnMismatch(t *testing.T) {
	drv, _ := sqliteReprojectDriver(t)
	q := NewQueue(drv.Jobs())
	ctx := context.Background()
	for _, res := range []*indexsig.VerifyResult{
		nil,
		{Match: true},
		{FirstBoot: true, NewHash: "h"},
	} {
		job, err := ScheduleReprojection(ctx, q, res, nil)
		require.NoError(t, err)
		require.Nil(t, job, "scheduled for %+v", res)
	}
	job, err := ScheduleReprojection(ctx, q, &indexsig.VerifyResult{OldHash: "a", NewHash: "b"}, nil)
	require.NoError(t, err)
	require.NotNil(t, job)
	require.Equal(t, ReprojectJobType, job.Type)
}
