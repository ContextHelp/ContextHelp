package service

import (
	"context"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/browser"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/safefetch"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// localLLMService is a service over the built-in pipelines with a local
// Ollama LLM (never called) and a browser client (never used).
func localLLMService(t *testing.T) *Service {
	t.Helper()
	for _, k := range []string{"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "no_proxy", "all_proxy"} {
		t.Setenv(k, "")
	}
	stub := config.ProviderBackendConfig{Backend: config.BackendStub}
	cfg := config.ProvidersConfig{
		Video: stub, Document: config.ProviderBackendConfig{Backend: "golib"}, OCR: stub, Transcription: stub,
		Vision: stub, Diarization: stub, Frames: stub, Summarization: stub,
		LLM: config.ProviderBackendConfig{Backend: "ollama", Model: "m", Endpoint: "http://127.0.0.1:11434"},
	}
	refuse := func(name string) (string, error) { return "", &providers.ToolNotInstalledError{Tool: name} }
	f := providers.NewFactory(cfg, nil).WithContext(providers.WithToolLookup(context.Background(), refuse))
	reg, err := builtins.ConfiguredRegistryWithOpts(builtins.BuildOpts{
		Factory:       f,
		BrowserClient: browser.NewClient("http://127.0.0.1:1", "t"),
		FetchGuard:    safefetch.New(safefetch.Options{Policy: safefetch.Policy{AllowPrivate: true}}),
	})
	require.NoError(t, err)
	driver := storageutil.NewTestDriver(t)
	return New(driver, jobs.NewQueue(driver.Jobs()), reg, search.NewEngine(driver), "", nil)
}

func uses(plan ReanalysisPlan, resource string) []string {
	var out []string
	for _, u := range plan.Uses {
		if u.Resource == resource {
			out = append(out, u.Step)
		}
	}
	return out
}

// url.generic is judged on its provider steps in stored-content mode; with
// the network allowed its fetch step is listed, without changing the
// locality.
func TestReanalysisLocality_FetchStepListedWithNetwork(t *testing.T) {
	s := localLLMService(t)
	stored, err := s.ReanalysisLocality(context.Background(), "url.generic", ReanalyzeOptions{})
	require.NoError(t, err)
	assert.Equal(t, pipeline.LocalityLocal, stored.Locality)
	assert.Empty(t, uses(stored, pipeline.ResourceNetwork))
	assert.NotContains(t, stored.Steps, "url_fetcher")

	net, err := s.ReanalysisLocality(context.Background(), "url.generic", ReanalyzeOptions{AllowNetwork: true})
	require.NoError(t, err)
	assert.Equal(t, pipeline.LocalityLocal, net.Locality, "a fetch of the object's own source does not change the locality")
	assert.Equal(t, []string{"url_fetcher"}, uses(net, pipeline.ResourceNetwork))
	assert.Contains(t, net.Steps, "url_fetcher")
}

// The plan's step list is the one ReanalyzeObject runs: both come from
// runnablePipeline.
func TestReanalysisLocality_AgreesWithReanalyzeOnSteps(t *testing.T) {
	s := localLLMService(t)
	for _, allow := range []bool{false, true} {
		plan, err := s.ReanalysisLocality(context.Background(), "url.generic", ReanalyzeOptions{AllowNetwork: allow})
		require.NoError(t, err)
		pipe, versioned, err := s.runnablePipeline("x", "url.generic", allow)
		require.NoError(t, err)
		var names []string
		for _, st := range pipe.Steps {
			names = append(names, st.Name())
		}
		assert.Equal(t, names, plan.Steps, "allow=%v", allow)
		assert.Equal(t, versioned, plan.Pipeline)
		assert.Equal(t, slices.Contains(names, "url_fetcher"), allow)
	}
}

// The browser pipelines hand the page to the ibr daemon, which may call a
// model: never automatic when the fetch would run. With the fetch skipped
// they are judged on what remains.
func TestReanalysisLocality_BrowserPipelinesAreNotAutomaticWithNetwork(t *testing.T) {
	s := localLLMService(t)
	for _, name := range []string{"url.authenticated", "url.interactive"} {
		net, err := s.ReanalysisLocality(context.Background(), name, ReanalyzeOptions{AllowNetwork: true})
		require.NoError(t, err, name)
		assert.Equal(t, pipeline.LocalityUnknown, net.Locality, name)
		assert.False(t, net.Locality.Automatic(), name)
		assert.Equal(t, []string{"ibr_fetcher"}, func() []string {
			var o []string
			for _, u := range net.Uses {
				if u.Opaque {
					o = append(o, u.Step)
				}
			}
			return o
		}(), name)

		stored, err := s.ReanalysisLocality(context.Background(), name, ReanalyzeOptions{})
		require.NoError(t, err, name)
		assert.True(t, stored.Locality.Automatic(), "%s stored-content: %s", name, stored.Locality)
	}
}

// A pipeline that cannot be re-derived is an error; one left out of the
// registry is unavailable with its reason; neither is automatic.
func TestReanalysisLocality_RefusedAndLeftOut(t *testing.T) {
	s := localLLMService(t)
	_, err := s.ReanalysisLocality(context.Background(), "feed.sync", ReanalyzeOptions{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), `pipeline "feed.sync"`)
	assert.NotContains(t, err.Error(), `object "`)
	plan, err := s.ReanalysisLocality(context.Background(), "image.analysis", ReanalyzeOptions{})
	require.NoError(t, err)
	assert.Equal(t, pipeline.LocalityUnavailable, plan.Locality)
	assert.NotEmpty(t, plan.Reason)
	_, err = s.ReanalysisLocality(context.Background(), "no.such.family", ReanalyzeOptions{})
	assert.Error(t, err)
}
