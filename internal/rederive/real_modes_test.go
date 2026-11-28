package rederive

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

// A step stored content runs changed: the object is re-derived from what
// is stored, and the source is not asked.
func TestReal_StoredContentStepChangeMakesNoRequest(t *testing.T) {
	e := newRealEnv(t)
	obj := e.ingest("url.generic", e.srv.URL+"/p", "")
	e.record()
	require.Equal(t, int32(1), e.hits.Load(), "the first ingest fetches once")
	sig := e.bump("url.generic", "sectioner")
	require.NotEqual(t, sig, e.sigOf(obj.ID))

	res, err := e.run()
	require.NoError(t, err)
	require.Equal(t, 1, res.Processed, "%+v", res)
	require.Equal(t, int32(1), e.hits.Load(), "stored-content mode must not fetch")
	require.Equal(t, sig, e.sigOf(obj.ID))
}

// A model change is a stored-content re-derive like any other.
func TestReal_ModelChangeIsStoredContent(t *testing.T) {
	e := newRealEnv(t)
	obj := e.ingest("url.generic", e.srv.URL+"/p", "")
	e.record()
	// The model entry differs: same steps, another model.
	p, err := e.reg.Get("url.generic")
	require.NoError(t, err)
	next := *p
	next.Entries = append([]string(nil), p.Entries...)
	for i, en := range next.Entries {
		if strings.HasPrefix(en, "llm=") {
			next.Entries[i] = "llm=ollama/other-model"
		}
	}
	require.NotEqual(t, p.Entries, next.Entries, "url.generic has no model entry to change: %v", p.Entries)
	next.Signature = "url.generic#00000000000000aa"
	e.reg.Upsert("url.generic", &next)
	e.record()
	res, err := e.run()
	require.NoError(t, err)
	require.Equal(t, 1, res.Processed, "%+v", res)
	require.Equal(t, int32(1), e.hits.Load())
	require.Equal(t, next.Signature, e.sigOf(obj.ID))
}

// A step that fetches, or a step that only makes sense on fresh content,
// can only be applied by fetching again.
func TestReal_FetchSideStepChangeFetchesOnce(t *testing.T) {
	for _, step := range []string{"url_fetcher", "html_cleaner"} {
		t.Run(step, func(t *testing.T) {
			e := newRealEnv(t)
			obj := e.ingest("url.generic", e.srv.URL+"/p", "")
			e.record()
			e.setPage(samePage("second page text added"))
			sig := e.bump("url.generic", step)
			before := e.hits.Load()

			res, err := e.run()
			require.NoError(t, err)
			require.Equal(t, 1, res.Processed, "%+v", res)
			require.Equal(t, before+1, e.hits.Load(), "network mode fetches the page once")
			require.Equal(t, sig, e.sigOf(obj.ID))
			require.Contains(t, e.get(obj.ID).TextContent, "second page text")
		})
	}
}

// --- the gate ---

func TestReal_GateDefersWhatWouldNotStayLocal(t *testing.T) {
	hostname := config.ProviderBackendConfig{Backend: "ollama", Model: "fake-model", Endpoint: "http://llm.invalid:11434"}
	stub := config.ProviderBackendConfig{Backend: config.BackendStub}
	for name, llm := range map[string]config.ProviderBackendConfig{"hostname endpoint": hostname, "stub llm": stub} {
		t.Run(name, func(t *testing.T) {
			e := newRealEnv(t)
			e.useLLM(llm)
			obj := e.ingest("url.generic", e.srv.URL+"/p", "")
			e.record()
			sig := e.bump("url.generic", "sectioner")
			e.llm.calls.Store(0)
			hits := e.hits.Load()
			before := e.snapshot(obj.ID)

			res, err := e.run()
			require.NoError(t, err)
			require.Equal(t, 0, res.Processed)
			require.Len(t, res.Deferred, 1, "%+v", res)
			require.Equal(t, "url.generic", res.Deferred[0].Pipeline)
			require.Equal(t, 1, res.Deferred[0].Objects)
			require.NotEmpty(t, res.Deferred[0].Reason)
			require.Zero(t, e.llm.calls.Load(), "no provider call")
			require.Equal(t, hits, e.hits.Load(), "no fetch")
			require.Equal(t, before, e.snapshot(obj.ID), "no write")
			require.NotEqual(t, sig, e.sigOf(obj.ID), "still stale")
			require.Empty(t, e.skips(sig), "deferred objects get no record")

			// The operator points the model at a loopback endpoint: the
			// next start processes them.
			e.useLLM(config.ProviderBackendConfig{Backend: "ollama", Model: "fake-model", Endpoint: loopbackLLM})
			sig = e.bump("url.generic", "sectioner") // the new registry carries the same release
			res, err = e.run()
			require.NoError(t, err)
			require.Equal(t, 1, res.Processed, "%+v", res)
			require.Empty(t, res.Deferred)
			require.Equal(t, sig, e.sigOf(obj.ID))
		})
	}
}

// --- the refresh guard ---

// bumpedFetch makes a url.generic object whose pipeline's fetch step then
// changes, and returns its id, the new signature and its stored snapshot.
func bumpedFetch(t *testing.T, e *realEnv) (id, sig, before string) {
	t.Helper()
	obj := e.ingest("url.generic", e.srv.URL+"/p", "")
	e.record()
	sig = e.bump("url.generic", "url_fetcher")
	return obj.ID, sig, e.snapshot(obj.ID)
}

func TestReal_GuardKeepsTheStoredObjectAndCountsATry(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body><p>" + strings.Repeat("A long page on another host. ", 40) + "</p></body></html>"))
	}))
	t.Cleanup(other.Close)
	// The stored source is on 127.0.0.1; "localhost" is another host.
	otherURL := strings.Replace(other.URL, "127.0.0.1", "localhost", 1)

	cases := map[string]func(e *realEnv){
		"an empty page":      func(e *realEnv) { e.setPage("<html><body></body></html>") },
		"a page that shrank": func(e *realEnv) { e.setPage("<html><body><p>Please log in to continue.</p></body></html>") },
		"another host": func(e *realEnv) {
			e.respondWith(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, otherURL+"/x", http.StatusFound) })
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			e := newRealEnv(t)
			id, sig, before := bumpedFetch(t, e)
			change(e)
			res, err := e.run()
			require.NoError(t, err)
			require.Equal(t, 0, res.Processed, "%+v", res)
			require.Equal(t, before, e.snapshot(id), "the stored object must be byte-identical")
			require.NotEqual(t, sig, e.sigOf(id))
			// A refresh the guard keeps out is a try: nothing says yet that
			// the object, and not the network, is why.
			require.Equal(t, 1, res.Transient, "%+v", res)
			require.Empty(t, e.skips(sig))
		})
	}
}

// A definition names "textcleaner" while the built step is
// "text_cleaner": the changed step is matched by the name the plan lists,
// so this stored-content step does not send the run to the network.
func TestReal_AliasedStepChangeIsStoredContent(t *testing.T) {
	for _, step := range []string{"textcleaner", "typedetector"} {
		t.Run(step, func(t *testing.T) {
			e := newRealEnv(t)
			obj := e.ingest("url.generic", e.srv.URL+"/p", "")
			e.record()
			sig := e.bump("url.generic", step)
			hits := e.hits.Load()
			res, err := e.run()
			require.NoError(t, err)
			require.Equal(t, 1, res.Processed, "%+v", res)
			require.Equal(t, hits, e.hits.Load(), "no fetch")
			require.Equal(t, sig, e.sigOf(obj.ID))
		})
	}
}

// A step listed twice (an inserted second embedding step) is a valid
// configuration: the signature changes, and the run over objects on the
// old signature must work, not panic.
func TestReal_RepeatedStepOverrideReDerives(t *testing.T) {
	e := newRealEnv(t)
	obj := e.ingest("text.short", "", "a short note about nothing in particular")
	e.record()
	e.pipelines = config.PipelinesConfig{Overrides: map[string]config.PipelineOverride{
		"text.short": {InsertSteps: []config.StepInsert{{Step: "embedding", After: "tagger"}}},
	}}
	e.useLLM(e.llmCfg)
	e.record()
	p, err := e.reg.Get("text.short")
	require.NoError(t, err)
	require.NotEqual(t, e.sigOf(obj.ID), p.Signature)

	res, err := e.run()
	require.NoError(t, err)
	require.Equal(t, 1, res.Processed, "%+v", res)
	require.Equal(t, p.Signature, e.sigOf(obj.ID))
}

// The service says an object is gone with the storage sentinel.
func TestReal_ReanalyzingADeletedObjectWrapsNotFound(t *testing.T) {
	e := newRealEnv(t)
	obj := e.ingest("text.short", "", "a short note about nothing in particular")
	require.NoError(t, e.driver.Objects().Delete(ctxSystem(), obj.ID))
	_, _, err := e.svc.ReanalyzeObjectWith(ctxSystem(), obj.ID, service.ReanalyzeOptions{})
	require.ErrorIs(t, err, upgrade.ErrObjectGone)
	require.ErrorIs(t, err, storage.ErrNotFound)
}
