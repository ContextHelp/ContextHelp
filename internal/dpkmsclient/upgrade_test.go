package dpkmsclient_test

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

// recorder collects every upgrade state an observer is handed.
type recorder struct {
	mu  sync.Mutex
	got []upgrade.Status
}

func (r *recorder) observe(st upgrade.Status) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, st)
}

func (r *recorder) seen() []upgrade.Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]upgrade.Status(nil), r.got...)
}

// A client going through ObserveUpgrade hands the observer the decoded
// state from every API response of an instance mid-upgrade, error
// responses included.
func TestObserveUpgradeFromFixture(t *testing.T) {
	m := upgrade.NewManager("")
	if err := m.Start(upgrade.BucketReingestSelective, 120); err != nil {
		t.Fatal(err)
	}
	if err := m.Tick(47); err != nil {
		t.Fatal(err)
	}
	srv := dpkmstest.Start(t, storageutil.NewTestDriver(t), dpkmstest.WithUpgrade(m))
	var rec recorder
	c := newClient(t, dpkmsclient.Endpoint{URL: srv.URL}, dpkmsclient.WithTransport(dpkmsclient.ObserveUpgrade(nil, rec.observe)))

	if _, _, err := c.Search(context.Background(), dpkmsclient.SearchRequest{Query: "type==text", Limit: 1}); err != nil {
		t.Fatalf("search: %v", err)
	}
	if err := c.Get(context.Background(), "/api/v1/objects/obj_missing", nil, nil); err == nil {
		t.Fatal("GET unknown object: want an error")
	}
	got := rec.seen()
	if len(got) != 2 {
		t.Fatalf("observer saw %d states, want one per response: %+v", len(got), got)
	}
	for _, st := range got {
		if st.State != upgrade.StateInProgress || st.Bucket != upgrade.BucketReingestSelective || st.Done != 47 || st.Total != 120 {
			t.Errorf("observed %+v", st)
		}
	}

	if err := m.Fail(errors.New("disk full")); err != nil {
		t.Fatal(err)
	}
	_, _, _ = c.Search(context.Background(), dpkmsclient.SearchRequest{Query: "type==text", Limit: 1})
	if got := rec.seen(); len(got) != 3 || got[2].State != upgrade.StateFailed || got[2].LastError != "disk full" {
		t.Fatalf("after Fail observer saw %+v", got)
	}
}

// No header, no call: an idle instance, a malformed value, and a
// response outside /api/v1 all leave the observer untouched.
func TestObserveUpgradeIgnores(t *testing.T) {
	srv, _ := countingServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/malformed":
			w.Header().Set(upgrade.HeaderName, "state=in_progress done=1")
		case "/elsewhere":
			w.Header().Set(upgrade.HeaderName, "state=in_progress, done=1, total=2, eta=0")
		}
		w.WriteHeader(http.StatusOK)
	})
	var rec recorder
	hc := &http.Client{Transport: dpkmsclient.ObserveUpgrade(nil, rec.observe)}
	for _, p := range []string{"/api/v1/idle", "/api/v1/malformed", "/elsewhere"} {
		resp, err := hc.Get(srv.URL + p)
		if err != nil {
			t.Fatalf("GET %s: %v", p, err)
		}
		resp.Body.Close()
	}
	if got := rec.seen(); len(got) != 0 {
		t.Fatalf("observer called for %+v", got)
	}
}

// A failed round trip reaches the caller unchanged and calls nothing.
func TestObserveUpgradeTransportError(t *testing.T) {
	var rec recorder
	hc := &http.Client{Transport: dpkmsclient.ObserveUpgrade(nil, rec.observe)}
	if _, err := hc.Get(dpkmstest.Start(t, nil, dpkmstest.Unreachable()).URL + "/api/v1/objects"); err == nil {
		t.Fatal("want a dial error")
	}
	if len(rec.seen()) != 0 {
		t.Fatal("observer called without a response")
	}
}

// An endpoint behind a path prefix (a reverse proxy mounting dpkms under
// /dpkms) still reports its upgrade.
func TestObserveUpgradeUnderBasePath(t *testing.T) {
	srv, _ := countingServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(upgrade.HeaderName, "state=in_progress, bucket=reindex_auto, done=1, total=2, eta=0")
		w.WriteHeader(http.StatusOK)
	})
	var rec recorder
	c := newClient(t, dpkmsclient.Endpoint{URL: srv.URL + "/dpkms"}, dpkmsclient.WithTransport(dpkmsclient.ObserveUpgrade(nil, rec.observe)))
	if err := c.Get(context.Background(), "/api/v1/objects", nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := rec.seen(); len(got) != 1 || got[0].Bucket != upgrade.BucketReindexAuto {
		t.Fatalf("observer saw %+v", got)
	}
}
