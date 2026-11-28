package rederive

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// A network outcome is a skip only on evidence that it is about this
// object: these tests start the job on networks that cannot reach what they
// should, and on networks that can.

func pageHandler(html string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, html)
	}
}

// otherHost is a page server of its own (another host:port than e.srv).
func otherHost(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

// stalePage ingests a page and returns it as an object still on oldSig:
// stale after a release, though it is ingested now.
func (e *realEnv) stalePage(source, oldSig string) *storage.KnowledgeObject {
	obj := e.ingest("url.generic", source, "")
	ds, ok := e.driver.Objects().(storage.DerivationStore)
	require.True(e.t, ok)
	require.NoError(e.t, ds.StampDerivation(ctxSystem(), obj.ID, oldSig))
	return obj
}

func (e *realEnv) oldSignature(name string) string {
	p, err := e.reg.Get(name)
	require.NoError(e.t, err)
	return p.Signature
}

// Off the VPN: an intranet page whose host cannot be reached among healthy
// pages on other hosts. Nothing here says the page is gone: never a skip.
func TestNet_OffTheVPNNeverSkipsTheIntranetPage(t *testing.T) {
	e := newRealEnv(t)
	vpn := otherHost(t, pageHandler(samePage("")))
	for i := 0; i < 5; i++ {
		e.ingest("url.generic", fmt.Sprintf("%s/p%d", e.srv.URL, i), "")
	}
	intranet := e.ingest("url.generic", vpn.URL+"/wiki", "")
	e.record()
	sig := e.bump("url.generic", "url_fetcher")
	vpn.Close() // the daemon starts off the VPN

	for start := 1; start <= 6; start++ {
		res, err := e.run()
		require.NoError(t, err, "start %d", start)
		if start == 1 {
			require.Equal(t, 5, res.Processed, "the healthy pages go on the first start: %+v", res)
		}
		require.Empty(t, e.skips(sig), "start %d", start)
	}
	require.NotEqual(t, sig, e.sigOf(intranet.ID))
}

// A captive portal answers 200 with a login page for every URL: the guard
// rejects every refresh. Nothing was fetched and accepted, so nothing is the
// object's fault: zero skips, and each start stops the group after five.
func TestNet_CaptivePortalSkipsNothing(t *testing.T) {
	e := newRealEnv(t)
	for i := 0; i < 8; i++ {
		e.ingest("url.generic", fmt.Sprintf("%s/p%d", e.srv.URL, i), "")
	}
	e.record()
	sig := e.bump("url.generic", "url_fetcher")
	e.setPage("<html><body><p>Welcome to the hotel network. Please sign in to continue.</p></body></html>")

	for start := 1; start <= 4; start++ {
		res, err := e.run()
		require.NoError(t, err)
		require.Len(t, res.Stopped, 1, "start %d: %+v", start, res)
		require.Equal(t, "refresh rejected", res.Stopped[0].Kind)
		require.Zero(t, res.Processed)
		require.Empty(t, e.skips(sig), "start %d", start)
	}
	require.Equal(t, 8, staleObjects(t, e.store(), e.reg))
}

// An expired token: the API answers 401 to every repository, while page
// refreshes succeed on another host. The pages are processed; the 401 is
// never the repository's fault, since no call to the API host worked.
func TestNet_ExpiredTokenSkipsNoRepository(t *testing.T) {
	e := newRealEnv(t)
	e.gh.set(longProse())
	repo := e.ingest("url.github.repo", repoURL, "")
	for i := 0; i < 3; i++ {
		e.ingest("url.generic", fmt.Sprintf("%s/p%d", e.srv.URL, i), "")
	}
	oldPage := e.oldSignature("url.generic")
	e.record()
	pageSig := e.bump("url.generic", "url_fetcher")
	repoSig := e.bump("url.github.repo", "github_repo_mapper")
	e.gh.fail(http.StatusUnauthorized, nil)

	for start := 1; start <= 6; start++ {
		if start > 1 {
			e.stalePage(fmt.Sprintf("%s/again%d", e.srv.URL, start), oldPage)
		}
		res, err := e.run()
		require.NoError(t, err, "start %d", start)
		if start == 1 {
			require.Equal(t, 3, res.Processed, "the pages go on the first start: %+v", res)
		}
		require.Empty(t, e.skips(repoSig), "start %d", start)
	}
	require.Empty(t, e.skips(pageSig))
	require.NotEqual(t, repoSig, e.sigOf(repo.ID))
}

// strikeStarts runs the job on n starts, adding one more healthy stale page
// before each, and returns the first start after which the object has a skip
// (0 when it never does).
func (e *realEnv) strikeStarts(sig, oldSig, healthyBase string, n int, id string) int {
	e.t.Helper()
	for start := 1; start <= n; start++ {
		e.stalePage(fmt.Sprintf("%s/healthy%d", healthyBase, start), oldSig)
		_, err := e.run()
		require.NoError(e.t, err)
		if _, skipped := e.skips(sig)[id]; skipped {
			return start
		}
	}
	return 0
}

// A page that is really gone (404), while other pages are fetched and
// accepted in the same runs: a skip on the fourth start, not before; not
// fetched on the fifth.
func TestNet_APageThatIsGoneIsSkippedOnTheFourthStart(t *testing.T) {
	e := newRealEnv(t)
	gone := e.ingest("url.generic", e.srv.URL+"/gone", "")
	oldSig := e.oldSignature("url.generic")
	e.record()
	sig := e.bump("url.generic", "url_fetcher")
	e.respondWith(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/gone" {
			http.NotFound(w, r)
			return
		}
		pageHandler(samePage(""))(w, r)
	})
	require.Equal(t, 4, e.strikeStarts(sig, oldSig, e.srv.URL, 8, gone.ID))
	sk := skipRows(t, e.driver, sig)
	require.Len(t, sk, 1)
	require.Equal(t, storage.SkipFetchFailed, sk[0].Reason)
	require.Contains(t, sk[0].Detail, "after 3 strikes")

	hits := e.hits.Load()
	e.stalePage(e.srv.URL+"/healthy-last", oldSig) // one request to ingest it
	_, err := e.run()                              // one more to refresh it
	require.NoError(t, err)
	require.Equal(t, hits+2, e.hits.Load(), "only the new healthy page was fetched: the gone one is skipped for good")
}

// A page whose content changed since it was captured: the guard rejects it
// while other refreshes are accepted: skipped on the fourth start.
func TestNet_APageThatChangedIsSkippedOnTheFourthStart(t *testing.T) {
	e := newRealEnv(t)
	changed := e.ingest("url.generic", e.srv.URL+"/changed", "")
	oldSig := e.oldSignature("url.generic")
	e.record()
	sig := e.bump("url.generic", "url_fetcher")
	e.respondWith(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/changed" {
			pageHandler("<html><body><p>"+words("different", 0, 300)+"</p></body></html>")(w, r)
			return
		}
		pageHandler(samePage(""))(w, r)
	})
	require.Equal(t, 4, e.strikeStarts(sig, oldSig, e.srv.URL, 8, changed.ID))
	require.Equal(t, storage.SkipRefreshRejected, e.skips(sig)[changed.ID])
}

// Two objects on one host, one answering 500 for ever and one healthy: the
// failing one is struck by the healthy one's success on the same host. Alone
// on its host, it never is.
func TestNet_AnotherStatusNeedsASuccessOnTheSameHost(t *testing.T) {
	t.Run("with a healthy page on the same host", func(t *testing.T) {
		e := newRealEnv(t)
		bad := e.ingest("url.generic", e.srv.URL+"/bad", "")
		oldSig := e.oldSignature("url.generic")
		e.record()
		sig := e.bump("url.generic", "url_fetcher")
		e.respondWith(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/bad" {
				http.Error(w, "boom", http.StatusInternalServerError)
				return
			}
			pageHandler(samePage(""))(w, r)
		})
		require.Equal(t, 4, e.strikeStarts(sig, oldSig, e.srv.URL, 8, bad.ID))
	})
	t.Run("alone on its host", func(t *testing.T) {
		e := newRealEnv(t)
		var sickNow atomic.Bool
		sick := otherHost(t, func(w http.ResponseWriter, r *http.Request) {
			if sickNow.Load() {
				http.Error(w, "boom", http.StatusInternalServerError)
				return
			}
			pageHandler(samePage(""))(w, r)
		})
		healthy := otherHost(t, pageHandler(samePage("")))
		bad := e.ingest("url.generic", sick.URL+"/x", "")
		e.record()
		oldSig := e.oldSignature("url.generic")
		sig := e.bump("url.generic", "url_fetcher")
		sickNow.Store(true)
		// The sick host answered 200 at capture; it answers 500 now. Pages on
		// other hosts succeed in every run, which proves nothing about it.
		require.Zero(t, e.strikeStarts(sig, oldSig, healthy.URL, 8, bad.ID))
	})
}

// A connection that cannot be made (DNS, refused) needs a success on the
// same host too: a host that is down is not the object's fault.
func TestNet_ARefusedConnectionAmongHealthyPagesOnOtherHostsNeverSkips(t *testing.T) {
	e := newRealEnv(t)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := httptest.NewUnstartedServer(pageHandler(samePage("")))
	srv.Listener.Close()
	srv.Listener = l
	srv.Start()
	obj := e.ingest("url.generic", srv.URL+"/x", "")
	oldSig := e.oldSignature("url.generic")
	e.record()
	sig := e.bump("url.generic", "url_fetcher")
	srv.Close()
	require.Zero(t, e.strikeStarts(sig, oldSig, e.srv.URL, 8, obj.ID))
}

// Group streak: a group whose five objects all fail stops, and the run goes
// on with the next group in name order; the status completes and the stopped
// group is reported with its cause.
func TestGroupStreak_AStoppedGroupDoesNotStarveTheNext(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < 7; i++ {
		f.add(fmt.Sprintf("a%d", i), "alpha@v1", alphaOld)
	}
	betaOld, betaNew := betaPipeline(f)
	f.svc.plans["beta"] = localPlan("x")
	for i := 0; i < 3; i++ {
		f.add(fmt.Sprintf("b%d", i), "beta@v1", betaOld)
	}
	f.svc.result = func(id string, _ service.ReanalyzeOptions) error {
		if strings.HasPrefix(id, "a") {
			return fetchErr("dial tcp: i/o timeout")
		}
		return nil
	}
	prog := &fakeProgress{}
	f.job.Progress = prog
	bus := &recordingBus{}
	f.job.Bus = bus
	res, err := f.run()
	require.NoError(t, err)
	require.Equal(t, 3, res.Processed, "the next group is processed in the same run: %+v", res)
	require.Len(t, res.Stopped, 1)
	require.Equal(t, "alpha", res.Stopped[0].Pipeline)
	require.Equal(t, "fetch", res.Stopped[0].Kind)
	require.Contains(t, res.Stopped[0].Cause, "i/o timeout")
	require.Nil(t, prog.failed, "a stopped group is not a failed run")
	require.NotNil(t, prog.done)
	require.Equal(t, res.Stopped, prog.stopped)
	require.Len(t, bus.events, 1)
	require.Len(t, bus.events[0].Stopped, 1)
	require.Equal(t, "alpha", bus.events[0].Stopped[0].Pipeline)
	require.Equal(t, betaNew, f.sigOf("b0"))
	// The stopped group's rest stays stale, fewest tries first.
	require.Equal(t, 7, f.stale())
}

// A 404 needs only some fetch accepted anywhere in the run: the network
// reaches sources, so this source is gone.
func TestNet_APageThatIsGoneIsStruckByAcceptedFetchesOnOtherHosts(t *testing.T) {
	e := newRealEnv(t)
	gone := e.ingest("url.generic", e.srv.URL+"/gone", "")
	oldSig := e.oldSignature("url.generic")
	e.record()
	sig := e.bump("url.generic", "url_fetcher")
	e.respondWith(func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	healthy := otherHost(t, pageHandler(samePage("")))
	require.Equal(t, 4, e.strikeStarts(sig, oldSig, healthy.URL, 8, gone.ID))
}
