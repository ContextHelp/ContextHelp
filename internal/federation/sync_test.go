package federation

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// syncBase is when seeded source objects changed: well before any
// until a test passes.
var syncBase = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

// seedSource stores one object per id in a fresh source DB, the i-th
// changed at syncBase + i seconds.
func seedSource(t *testing.T, ids ...string) storage.StorageDriver {
	t.Helper()
	src, _ := newTestDB(t)
	for i, id := range ids {
		o := makeObject(id, "hash-"+id)
		o.CreatedAt = syncBase.Add(time.Duration(i) * time.Second)
		o.UpdatedAt = o.CreatedAt
		if err := src.Objects().Create(context.Background(), &o); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	return src
}

// touch bumps id's updated_at to at, as an edit does.
func touch(t *testing.T, src storage.StorageDriver, id string, at time.Time) {
	t.Helper()
	ctx := context.Background()
	o, err := src.Objects().Get(ctx, id)
	if err != nil {
		t.Fatalf("get %s: %v", id, err)
	}
	o.UpdatedAt = at
	if err := src.Objects().Update(ctx, o); err != nil {
		t.Fatalf("update %s: %v", id, err)
	}
}

// spyPusher records the object ids of every Push it forwards.
type spyPusher struct {
	Pusher
	batches [][]string
}

func (s *spyPusher) Push(ctx context.Context, objects []storage.KnowledgeObject, edges []storage.Edge, entities []storage.Entity) error {
	ids := make([]string, 0, len(objects))
	for _, o := range objects {
		ids = append(ids, o.ID)
	}
	s.batches = append(s.batches, ids)
	return s.Pusher.Push(ctx, objects, edges, entities)
}

// receiver is a federation push endpoint recording the object ids and
// encoded object bytes of each request. fail decides, per request
// number (from 1), whether to answer 400.
type receiver struct {
	mu       sync.Mutex
	requests [][]string
	sizes    []int
	fail     func(n int) bool
}

func (rv *receiver) serve(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Objects []json.RawMessage `json:"objects"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode push: %v", err)
		}
		ids, size := []string{}, 0
		for _, raw := range body.Objects {
			var o storage.KnowledgeObject
			if err := json.Unmarshal(raw, &o); err != nil {
				t.Errorf("decode object: %v", err)
			}
			ids = append(ids, o.ID)
			size += len(raw)
		}
		rv.mu.Lock()
		rv.requests = append(rv.requests, ids)
		rv.sizes = append(rv.sizes, size)
		n := len(rv.requests)
		rv.mu.Unlock()
		if rv.fail != nil && rv.fail(n) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (rv *receiver) sent() []string {
	rv.mu.Lock()
	defer rv.mu.Unlock()
	var all []string
	for _, ids := range rv.requests {
		all = append(all, ids...)
	}
	return all
}

var syncUntil = syncBase.Add(time.Hour)

func mustSync(t *testing.T, src storage.StorageDriver, p Pusher, batchSize int) int {
	t.Helper()
	n, err := Sync(context.Background(), src, p, syncUntil, batchSize)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	return n
}

// A tick with nothing changed since the last one pushes nothing: no
// Push for a local target, no request for a remote one.
func TestSync_UnchangedSourcePushesNothing(t *testing.T) {
	ids := []string{"s-1", "s-2", "s-3"}

	t.Run("local", func(t *testing.T) {
		src := seedSource(t, ids...)
		_, target := newTestDB(t)
		spy := &spyPusher{Pusher: NewLocalPusher("sync-local", target, nil, src.Blobs())}
		if n := mustSync(t, src, spy, 0); n != 3 {
			t.Fatalf("first sync pushed %d objects, want 3", n)
		}
		if n := mustSync(t, src, spy, 0); n != 0 {
			t.Errorf("second sync pushed %d objects, want 0", n)
		}
		if len(spy.batches) != 1 {
			t.Errorf("Push calls = %v, want only the first sync's", spy.batches)
		}
	})
	t.Run("remote", func(t *testing.T) {
		src := seedSource(t, ids...)
		rv := &receiver{}
		p := NewRemotePusherWithWatermarks("sync-remote", rv.serve(t).URL, "", src.Watermarks(), src.Blobs())
		mustSync(t, src, p, 0)
		if n := mustSync(t, src, p, 0); n != 0 {
			t.Errorf("second sync pushed %d objects, want 0", n)
		}
		if len(rv.requests) != 1 || !slices.Equal(rv.requests[0], ids) {
			t.Errorf("requests = %v, want one carrying %v", rv.requests, ids)
		}
	})
}

// After an edit, the next tick pushes the edited object and nothing else.
func TestSync_PushesOnlyChangedObject(t *testing.T) {
	src := seedSource(t, "c-1", "c-2", "c-3")
	rv := &receiver{}
	p := NewRemotePusherWithWatermarks("sync-changed", rv.serve(t).URL, "", src.Watermarks(), src.Blobs())
	mustSync(t, src, p, 0)

	touch(t, src, "c-2", syncBase.Add(time.Minute))
	if n := mustSync(t, src, p, 0); n != 1 {
		t.Fatalf("sync after an edit pushed %d objects, want 1", n)
	}
	if got := rv.requests[len(rv.requests)-1]; !slices.Equal(got, []string{"c-2"}) {
		t.Errorf("second request carried %v, want [c-2]", got)
	}
}

// A target that refuses a batch keeps its watermark, and the next tick
// sends the same objects again.
func TestSync_FailedPushKeepsWatermarkAndRetries(t *testing.T) {
	ids := []string{"f-1", "f-2"}
	src := seedSource(t, ids...)
	rv := &receiver{fail: func(n int) bool { return n == 1 }}
	p := NewRemotePusherWithWatermarks("sync-fail", rv.serve(t).URL, "", src.Watermarks(), src.Blobs())

	n, err := Sync(context.Background(), src, p, syncUntil, 0)
	if err == nil || n != 0 {
		t.Fatalf("Sync against a refusing target = %d, %v; want 0 and an error", n, err)
	}
	if wm, _ := p.Watermark(context.Background()); wm != (storage.ChangeCursor{}) {
		t.Fatalf("watermark moved on a refused batch: %+v", wm)
	}

	if n := mustSync(t, src, p, 0); n != 2 {
		t.Fatalf("retry pushed %d objects, want 2", n)
	}
	if want := [][]string{ids, ids}; len(rv.requests) != 2 || !slices.Equal(rv.requests[1], ids) {
		t.Errorf("requests = %v, want %v", rv.requests, want)
	}
	if wm, _ := p.Watermark(context.Background()); wm.ID != "f-2" {
		t.Errorf("watermark after the retry = %+v, want f-2", wm)
	}
}

// A sync carries at most batchSize objects per Push, and the watermark
// follows each batch.
func TestSync_BatchesByCount(t *testing.T) {
	src := seedSource(t, "b-1", "b-2", "b-3", "b-4", "b-5")
	_, target := newTestDB(t)
	spy := &spyPusher{Pusher: NewLocalPusher("sync-batches", target, nil, src.Blobs())}
	if n := mustSync(t, src, spy, 2); n != 5 {
		t.Fatalf("pushed %d objects, want 5", n)
	}
	want := [][]string{{"b-1", "b-2"}, {"b-3", "b-4"}, {"b-5"}}
	if !slices.EqualFunc(spy.batches, want, slices.Equal[[]string]) {
		t.Errorf("batches = %v, want %v", spy.batches, want)
	}
}

// seedBlobSource stores one object per id whose content, size bytes of
// it, lives in the source blob store.
func seedBlobSource(t *testing.T, size int, ids ...string) storage.StorageDriver {
	t.Helper()
	ctx := context.Background()
	src, _ := newTestDB(t)
	for i, id := range ids {
		key := fmt.Sprintf("%040x", i+1)
		content := strings.Repeat(id[len(id)-1:], size)
		if err := src.Blobs().Put(ctx, key, strings.NewReader(content), storage.BlobMeta{Size: int64(size)}); err != nil {
			t.Fatal(err)
		}
		o := makeObject(id, "hash-"+id)
		o.RawContent, o.TextContent = "blob://"+key, "blob://"+key
		o.CreatedAt = syncBase.Add(time.Duration(i) * time.Second)
		o.UpdatedAt = o.CreatedAt
		if err := src.Objects().Create(ctx, &o); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	return src
}

// Content travels inline, so one batch of large objects goes out in
// several requests, each within the byte cap; a request the target
// refuses leaves the watermark after the last acknowledged one, and the
// next tick sends only what is left.
func TestSync_SplitsLargeInlinePayloadAcrossRequests(t *testing.T) {
	ids := []string{"l-1", "l-2", "l-3", "l-4", "l-5"}
	const content = 1000 // raw + text inline: ~2KB encoded per object
	const capBytes = 5000

	t.Run("split within the cap", func(t *testing.T) {
		src := seedBlobSource(t, content, ids...)
		rv := &receiver{}
		p := NewRemotePusherWithWatermarks("sync-large", rv.serve(t).URL, "", src.Watermarks(), src.Blobs())
		p.maxRequestBytes = capBytes
		mustSync(t, src, p, 0)

		if len(rv.requests) < 2 {
			t.Fatalf("one batch went out in %d request(s), want it split", len(rv.requests))
		}
		for i, size := range rv.sizes {
			if size > capBytes {
				t.Errorf("request %d carried %d bytes of objects, cap %d", i+1, size, capBytes)
			}
		}
		if got := rv.sent(); !slices.Equal(got, ids) {
			t.Errorf("objects sent = %v, want each of %v once, in order", got, ids)
		}
		if wm, _ := p.Watermark(context.Background()); wm.ID != "l-5" {
			t.Errorf("watermark = %+v, want l-5", wm)
		}
	})
	t.Run("refused request resumes after the last acknowledged", func(t *testing.T) {
		src := seedBlobSource(t, content, ids...)
		rv := &receiver{fail: func(n int) bool { return n == 2 }}
		p := NewRemotePusherWithWatermarks("sync-large-fail", rv.serve(t).URL, "", src.Watermarks(), src.Blobs())
		p.maxRequestBytes = capBytes
		if _, err := Sync(context.Background(), src, p, syncUntil, 0); err == nil {
			t.Fatal("Sync with a refused request: want error")
		}
		acked := rv.requests[0]
		wm, _ := p.Watermark(context.Background())
		if wm.ID != acked[len(acked)-1] {
			t.Fatalf("watermark = %+v, want the first request's last object %s", wm, acked[len(acked)-1])
		}

		before := len(rv.requests)
		mustSync(t, src, p, 0)
		var resent []string
		for _, r := range rv.requests[before:] {
			resent = append(resent, r...)
		}
		if want := ids[len(acked):]; !slices.Equal(resent, want) {
			t.Errorf("retry sent %v, want only %v", resent, want)
		}
	})
}

// Edges and the entities they mention reach the target once even when
// their objects are split across requests.
func TestRemotePusher_SplitBatchSendsEachEdgeOnce(t *testing.T) {
	type body struct {
		Objects  []storage.KnowledgeObject `json:"objects"`
		Edges    []storage.Edge            `json:"edges"`
		Entities []storage.Entity          `json:"entities"`
	}
	var got []body
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b body
		_ = json.NewDecoder(r.Body).Decode(&b)
		got = append(got, b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	objects := []storage.KnowledgeObject{makeObject("e-1", "h-e-1"), makeObject("e-2", "h-e-2")}
	edges := []storage.Edge{
		makeEdge("edge-12", "e-1", "e-2"),
		makeEdge("edge-2x", "e-2", "elsewhere"),
		{ID: "edge-m", FromType: "object", FromID: "e-2", ToType: "entity", ToID: "acme", EdgeType: "mentions"},
	}
	entities := []storage.Entity{{Slug: "acme", Title: "Acme"}}
	p := NewRemotePusher("split-edges", srv.URL, "")
	p.maxRequestBytes = 1 // every object alone
	if err := p.Push(context.Background(), objects, edges, entities); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("requests = %d, want 2", len(got))
	}
	edgeIDs := func(b body) []string {
		var ids []string
		for _, e := range b.Edges {
			ids = append(ids, e.ID)
		}
		return ids
	}
	if ids := edgeIDs(got[0]); !slices.Equal(ids, []string{"edge-12"}) {
		t.Errorf("first request edges = %v, want [edge-12]", ids)
	}
	if ids := edgeIDs(got[1]); !slices.Equal(ids, []string{"edge-2x", "edge-m"}) {
		t.Errorf("second request edges = %v, want [edge-2x edge-m]", ids)
	}
	if len(got[0].Entities) != 1 || len(got[1].Entities) != 0 {
		t.Errorf("entities per request = %d, %d; want all in the first", len(got[0].Entities), len(got[1].Entities))
	}
}

// Each split request still decodes as the FederationPushRequest the
// receiver reads.
func TestRemotePusher_SplitBodyDecodesAsPushRequest(t *testing.T) {
	var reqs []FederationPushRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req FederationPushRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode: %v", err)
		}
		reqs = append(reqs, req)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	p := NewRemotePusher("decode", srv.URL, "")
	p.maxRequestBytes = 1
	objs := []storage.KnowledgeObject{makeObject("d-1", "h-d-1"), makeObject("d-2", "h-d-2")}
	if err := p.Push(context.Background(), objs, nil, nil); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if len(reqs) != 2 || reqs[0].Objects[0].ID != "d-1" || reqs[1].Objects[0].RawContent != "content-d-2" {
		t.Errorf("decoded requests = %+v", reqs)
	}
	if reqs[0].Edges == nil || reqs[0].Entities == nil {
		t.Error("empty edges/entities must encode as [], not null")
	}
}

// The local target database keeps the watermark, so a sync to a fresh
// target pushes everything and one to an up-to-date target nothing.
func TestSync_LocalWatermarkLivesInTarget(t *testing.T) {
	src := seedSource(t, "t-1", "t-2")
	dir := t.TempDir()
	first := filepath.Join(dir, "a.db")
	mustSync(t, src, NewLocalPusher("same-name", first, nil, src.Blobs()), 0)
	if n := mustSync(t, src, NewLocalPusher("same-name", first, nil, src.Blobs()), 0); n != 0 {
		t.Errorf("re-sync to an up-to-date target pushed %d", n)
	}
	other := filepath.Join(dir, "b.db")
	if n := mustSync(t, src, NewLocalPusher("same-name", other, nil, src.Blobs()), 0); n != 2 {
		t.Errorf("sync to a fresh target pushed %d, want 2", n)
	}
}
