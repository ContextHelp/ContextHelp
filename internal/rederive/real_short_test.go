package rederive

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

const (
	shortPage  = "<html><body><p>Hello, a short page.</p></body></html>"
	portalPage = "<html><body><p>Welcome to the hotel network. Please sign in to continue.</p></body></html>"
)

// A stored page too short to compare is not refreshed at all: it is a skip
// at once, known before any request, and a page served by a portal can
// never replace it nor count as evidence that the network works.
func TestShortPages_AreLeftAsCapturedWithoutARequest(t *testing.T) {
	e := newRealEnv(t)
	e.respondWith(pageHandler(shortPage))
	short := e.ingest("url.generic", e.srv.URL+"/short", "")
	e.record()
	sig := e.bump("url.generic", "url_fetcher")
	before := e.snapshot(short.ID)
	hits := e.hits.Load()

	res, err := e.run()
	require.NoError(t, err)
	require.Equal(t, hits, e.hits.Load(), "no request is made for it")
	require.Equal(t, 0, res.Processed)
	require.Equal(t, before, e.snapshot(short.ID), "the stored text is unchanged")
	sk := skipRows(t, e.driver, sig)
	require.Len(t, sk, 1)
	require.Equal(t, storage.SkipRefreshRejected, sk[0].Reason)
	require.Equal(t, "stored text too short to compare with a refresh; left as captured", sk[0].Detail)
}

// The reviewer's probe: one host behind a portal, a long article, and one
// more short stale page each start. The article is never skipped; the short
// pages are skipped at once and their host receives no request for them.
func TestShortPages_ABehindAPortalNeverSkipsTheArticle(t *testing.T) {
	e := newRealEnv(t)
	article := e.ingest("url.generic", e.srv.URL+"/article", "")
	oldSig := e.oldSignature("url.generic")
	e.record()
	sig := e.bump("url.generic", "url_fetcher")
	e.respondWith(pageHandler(portalPage)) // every address now answers the portal page

	for start := 1; start <= 6; start++ {
		short := e.stalePage(fmt.Sprintf("%s/short%d", e.srv.URL, start), oldSig)
		before := e.snapshot(short.ID)
		hits := e.hits.Load()
		res, err := e.run()
		require.NoError(t, err, "start %d", start)
		require.Equal(t, int32(1), e.hits.Load()-hits, "start %d: only the article is requested", start)
		require.Equal(t, before, e.snapshot(short.ID), "start %d: the portal page did not replace it", start)
		require.Equal(t, storage.SkipRefreshRejected, e.skips(sig)[short.ID], "start %d: skipped at once", start)
		require.NotContains(t, e.skips(sig), article.ID, "start %d: the article is never skipped", start)
		require.Zero(t, res.Processed)
	}
}

// The API rule is unchanged: a short stored README is still refreshed.
func TestShortPages_AShortREADMEIsStillRefreshedUnderTheAPIRule(t *testing.T) {
	e := newRealEnv(t)
	e.gh.set("short")
	obj := e.ingest("url.github.repo", repoURL, "")
	e.record()
	sig := e.bump("url.github.repo", "github_repo_mapper")
	e.gh.set("a different short readme")

	res, err := e.run()
	require.NoError(t, err)
	require.Equal(t, 1, res.Processed, "%+v", res)
	require.Equal(t, sig, e.sigOf(obj.ID))
	require.Contains(t, e.get(obj.ID).RawContent, "a different short readme")
}

// A host that starts answering 429 after two successes: a rate limit is
// about the host, never the object. No strike, whatever else the host served.
func TestRateLimit_NeverStrikesAnObject(t *testing.T) {
	e := newRealEnv(t)
	limited := e.ingest("url.generic", e.srv.URL+"/limited", "")
	oldSig := e.oldSignature("url.generic")
	e.record()
	sig := e.bump("url.generic", "url_fetcher")
	e.respondWith(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/limited") {
			w.Header().Set("Retry-After", "60")
			http.Error(w, "slow down", http.StatusTooManyRequests)
			return
		}
		pageHandler(samePage(""))(w, r)
	})
	require.Zero(t, e.strikeStarts(sig, oldSig, e.srv.URL, 8, limited.ID))
}
