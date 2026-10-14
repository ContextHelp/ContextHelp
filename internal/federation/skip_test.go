package federation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// seedWithLostBlob stores one inline object per id, except lost, whose
// content refs a blob the source store no longer holds.
func seedWithLostBlob(t *testing.T, lost string, ids ...string) storage.StorageDriver {
	t.Helper()
	ctx := context.Background()
	src, _ := newTestDB(t)
	for _, id := range ids {
		o := makeObject(id, "hash-"+id)
		if id == lost {
			o.RawContent, o.TextContent = "blob://"+lostKey, "blob://"+lostKey
		}
		if err := src.Objects().Create(ctx, &o); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	return src
}

const lostKey = "0badc0de0badc0de0badc0de0badc0de0badc0de"

// syncTimes runs s n times, returning each run's pushed count and error.
func syncTimes(s *Syncer, n int) ([]int, []error) {
	var counts []int
	var errs []error
	for range n {
		c, err := s.Sync(context.Background())
		counts = append(counts, c)
		errs = append(errs, err)
	}
	return counts, errs
}

// An object whose source blob is gone fails every sync; after
// maxObjectFailures in a row the Syncer skips it, records it, and
// pushes everything after it. An edit makes it eligible again.
func TestSyncer_SkipsObjectThatKeepsFailing(t *testing.T) {
	for _, tc := range []struct {
		name   string
		pusher func(t *testing.T, src storage.StorageDriver) (Pusher, func() []string)
	}{
		{"local", func(t *testing.T, src storage.StorageDriver) (Pusher, func() []string) {
			_, target := newTestDB(t)
			return NewLocalPusher("skip-local", target, nil, src.Blobs()), func() []string {
				return objectIDs(t, openPath(t, target))
			}
		}},
		{"remote", func(t *testing.T, src storage.StorageDriver) (Pusher, func() []string) {
			rv := &receiver{}
			p := NewRemotePusherWithWatermarks("skip-remote", rv.serve(t).URL, "", src.Watermarks(), src.Blobs())
			return p, func() []string {
				ids := slices.Clone(rv.sent())
				slices.Sort(ids)
				return slices.Compact(ids)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := seedWithLostBlob(t, "k-2", "k-1", "k-2", "k-3")
			p, delivered := tc.pusher(t, src)
			checkSkipsLostBlob(t, src, p, delivered)
		})
	}
}

// checkSkipsLostBlob drives TestSyncer_SkipsObjectThatKeepsFailing
// against p; delivered lists the ids the target holds, sorted.
func checkSkipsLostBlob(t *testing.T, src storage.StorageDriver, p Pusher, delivered func() []string) {
	t.Helper()
	s := NewSyncer(src, p, 0)

	_, errs := syncTimes(s, maxObjectFailures-1)
	for i, err := range errs {
		var oe *ObjectError
		if !errors.As(err, &oe) || oe.ID != "k-2" {
			t.Fatalf("sync %d: err = %v, want an ObjectError for k-2", i+1, err)
		}
	}
	if wm := mustWatermark(t, p); wm != 0 {
		t.Fatalf("watermark moved while k-2 still fails: %d", wm)
	}
	if got := s.Skipped(); len(got) != 0 {
		t.Fatalf("skipped before %d failures: %+v", maxObjectFailures, got)
	}

	n, err := s.Sync(context.Background())
	if err != nil || n != 2 {
		t.Fatalf("sync %d = %d, %v; want k-1 and k-3 pushed, k-2 skipped", maxObjectFailures, n, err)
	}
	if got, want := delivered(), []string{"k-1", "k-3"}; !slices.Equal(got, want) {
		t.Errorf("target holds %v, want %v", got, want)
	}
	if wm, want := mustWatermark(t, p), seqOf(t, src, "k-3"); wm != want {
		t.Errorf("watermark = %d, want k-3's %d", wm, want)
	}
	skipped := s.Skipped()
	if len(skipped) != 1 || skipped[0].ID != "k-2" || skipped[0].ChangeSeq != seqOf(t, src, "k-2") ||
		!strings.Contains(skipped[0].Err, lostKey) {
		t.Errorf("Skipped() = %+v, want k-2 with its blob error", skipped)
	}
	if n, err := s.Sync(context.Background()); err != nil || n != 0 {
		t.Errorf("sync after the skip = %d, %v; want nothing left", n, err)
	}

	// The blob comes back and k-2 is edited: it goes out again.
	if err := src.Blobs().Put(context.Background(), lostKey, strings.NewReader("found"), storage.BlobMeta{Size: 5}); err != nil {
		t.Fatal(err)
	}
	touch(t, src, "k-2", syncBase)
	if n, err := s.Sync(context.Background()); err != nil || n != 1 {
		t.Errorf("sync after editing k-2 = %d, %v; want it pushed", n, err)
	}
	if got, want := delivered(), []string{"k-1", "k-2", "k-3"}; !slices.Equal(got, want) {
		t.Errorf("target holds %v, want %v", got, want)
	}
}

// A skipped object last in its batch still moves the watermark past
// it, so the next sync does not list it again.
func TestSyncer_SkipAtEndOfBatchAdvancesWatermark(t *testing.T) {
	src := seedWithLostBlob(t, "e-2", "e-1", "e-2")
	_, target := newTestDB(t)
	spy := &spyPusher{Pusher: NewLocalPusher("skip-end", target, nil, src.Blobs())}
	s := NewSyncer(src, spy, 0)
	syncTimes(s, maxObjectFailures)

	if wm, want := mustWatermark(t, spy), seqOf(t, src, "e-2"); wm != want {
		t.Fatalf("watermark = %d, want e-2's %d", wm, want)
	}
	calls := len(spy.batches)
	if n, err := s.Sync(context.Background()); err != nil || n != 0 || len(spy.batches) != calls {
		t.Errorf("next sync = %d, %v with %d more Push calls; want nothing", n, err, len(spy.batches)-calls)
	}
}

// A failing target is not an object's fault: however many syncs it
// refuses, nothing is skipped.
func TestSyncer_TargetFailuresNeverSkip(t *testing.T) {
	src := seedSource(t, "t-1", "t-2")
	rv := &receiver{fail: func(int) bool { return true }}
	s := NewSyncer(src, NewRemotePusherWithWatermarks("refusing", rv.serve(t).URL, "", src.Watermarks(), src.Blobs()), 0)
	_, errs := syncTimes(s, 2*maxObjectFailures)
	for i, err := range errs {
		if err == nil {
			t.Fatalf("sync %d against a refusing target: no error", i+1)
		}
	}
	if got := s.Skipped(); len(got) != 0 {
		t.Errorf("skipped %+v on target failures", got)
	}
}

// An object over the request cap goes alone. A receiver that refuses
// it as too large (413) fails that object, not the target: it is
// skipped like a lost blob and the objects around it go through.
func TestSyncer_SkipsObjectReceiverRefusesAsTooLarge(t *testing.T) {
	const limit = 4096
	ids := []string{"o-1", "o-big", "o-3"}
	ctx := context.Background()
	src, _ := newTestDB(t)
	for _, id := range ids {
		o := makeObject(id, "hash-"+id)
		if id == "o-big" {
			o.RawContent = strings.Repeat("x", 2*limit)
		}
		if err := src.Objects().Create(ctx, &o); err != nil {
			t.Fatal(err)
		}
	}

	var mu sync.Mutex
	var accepted [][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if len(b) > limit {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		var req FederationPushRequest
		_ = json.Unmarshal(b, &req)
		var got []string
		for _, o := range req.Objects {
			got = append(got, o.ID)
		}
		mu.Lock()
		accepted = append(accepted, got)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	p := NewRemotePusherWithWatermarks("too-large", srv.URL, "", src.Watermarks(), src.Blobs())
	p.maxRequestBytes = limit / 2
	s := NewSyncer(src, p, 0)

	_, errs := syncTimes(s, maxObjectFailures-1)
	for i, err := range errs {
		var oe *ObjectError
		if !errors.As(err, &oe) || oe.ID != "o-big" || !strings.Contains(err.Error(), "413") {
			t.Fatalf("sync %d: err = %v, want an ObjectError for o-big naming HTTP 413", i+1, err)
		}
	}
	// o-1 went out with the first sync, before o-big's request.
	if n, err := s.Sync(ctx); err != nil || n != 1 {
		t.Fatalf("sync %d = %d, %v; want o-3 pushed", maxObjectFailures, n, err)
	}
	if skipped := s.Skipped(); len(skipped) != 1 || skipped[0].ID != "o-big" || !strings.Contains(skipped[0].Err, "too large") {
		t.Errorf("Skipped() = %+v, want o-big refused as too large", skipped)
	}
	if wm, want := mustWatermark(t, p), seqOf(t, src, "o-3"); wm != want {
		t.Errorf("watermark = %d, want o-3's %d", wm, want)
	}
	mu.Lock()
	defer mu.Unlock()
	if got := fmt.Sprint(accepted[len(accepted)-1]); got != "[o-3]" {
		t.Errorf("last accepted request = %s, want [o-3]", got)
	}
}

// A worker keeps its Syncer across ticks, so a failing object is
// skipped after maxObjectFailures ticks, and the WorkerSet reports it
// under the target's name.
func TestWorkerSet_ReportsSkippedObjects(t *testing.T) {
	src := seedWithLostBlob(t, "w-2", "w-1", "w-2")
	_, target := newTestDB(t)
	ws, _ := newWorkerSetWithPushers(t, src, []FederationTarget{{Name: "fed-skip", SyncMode: "async"}},
		map[string]Pusher{"fed-skip": NewLocalPusher("fed-skip", target, nil, src.Blobs())})
	for range maxObjectFailures {
		_ = ws.workers[0].tick(context.Background())
	}
	got := ws.Skipped()
	if len(got) != 1 || len(got["fed-skip"]) != 1 || got["fed-skip"][0].ID != "w-2" {
		t.Errorf("Skipped() = %+v, want w-2 under fed-skip", got)
	}
}
