package rederive

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// A rate limit is not a fact about the object: nothing is recorded and the
// object is fetched again at the next run.
func TestReal_RateLimitIsTransient(t *testing.T) {
	e := newRealEnv(t)
	id, sig, before := bumpedFetch(t, e)
	e.setStatus(http.StatusTooManyRequests)

	res, err := e.run()
	require.NoError(t, err)
	require.Equal(t, 1, res.Transient, "%+v", res)
	require.Zero(t, res.SkippedTotal())
	require.Empty(t, e.skips(sig))
	require.Equal(t, before, e.snapshot(id))
	require.NotEqual(t, sig, e.sigOf(id))

	e.setPage(samePage("after the limit"))
	res, err = e.run()
	require.NoError(t, err)
	require.Equal(t, 1, res.Processed, "%+v", res)
	require.Equal(t, sig, e.sigOf(id))
}

// Five connection failures in a row stop the run as interrupted, with no
// skip rows, and the next run resumes.
func TestReal_ConnectionFailuresStopTheRun(t *testing.T) {
	e := newRealEnv(t)
	var ids []string
	for i := 0; i < 7; i++ {
		ids = append(ids, e.ingest("url.generic", e.srv.URL+"/p"+string(rune('0'+i)), "").ID)
	}
	e.record()
	sig := e.bump("url.generic", "url_fetcher")
	e.respondWith(func(w http.ResponseWriter, _ *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
	})
	hits := e.hits.Load()

	res, err := e.run()
	require.NoError(t, err)
	require.Len(t, res.Stopped, 1)
	require.Equal(t, failureStreak, res.Transient, "%+v", res)
	// The HTTP client may retry a dropped connection once per object.
	require.GreaterOrEqual(t, e.hits.Load(), hits+int32(failureStreak))
	require.Empty(t, e.skips(sig))
	for _, id := range ids {
		require.NotEqual(t, sig, e.sigOf(id))
	}

	e.setPage(samePage(""))
	res, err = e.run()
	require.NoError(t, err)
	require.Equal(t, 7, res.Processed, "%+v", res)
}

// url.authenticated and url.interactive need a browser provider. Without
// it they are left out of the registry, so their work is deferred and
// nothing is fetched; with a browser their locality is unknown, which the
// gate also defers (TestRun_DefersWhatWouldNotStayLocal, unknown).
func TestReal_BrowserPipelinesAreDeferred(t *testing.T) {
	e := newRealEnv(t)
	for _, name := range []string{"url.authenticated", "url.interactive"} {
		t.Run(name, func(t *testing.T) {
			rec, ok := pipeline.CurrentRecord(e.reg, name)
			require.True(t, ok, "%s is not known to the registry", name)
			old := make([]string, len(rec.Entries))
			for i, en := range rec.Entries {
				old[i] = strings.Replace(en, "ibr_fetcher@1", "ibr_fetcher@0", 1)
			}
			oldSig := name + "#00000000000000ff"
			require.NoError(t, e.store().UpsertDerivationSignature(context.Background(), oldSig, name, old))
			id := "browser-" + name
			require.NoError(t, e.driver.Objects().Create(ctxSystem(), &storage.KnowledgeObject{
				ID: id, Type: "note", Pipeline: name + "@v1", Source: e.srv.URL + "/secret",
				RawContent: "stored page", TextContent: "stored page", DerivationSig: oldSig,
				Status: storage.ObjectStatusActive,
			}))
			hits, before := e.hits.Load(), e.snapshot(id)

			res, err := e.run()
			require.NoError(t, err)
			var found bool
			for _, d := range res.Deferred {
				found = found || d.Pipeline == name
			}
			require.True(t, found, "%s not deferred: %+v", name, res)
			require.Zero(t, res.Processed)
			require.Equal(t, hits, e.hits.Load(), "no fetch")
			require.Equal(t, before, e.snapshot(id))
			require.Empty(t, e.skips(rec.Sig))
		})
	}
}
