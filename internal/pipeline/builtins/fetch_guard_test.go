package builtins

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/browser"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/safefetch"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// fetchStep returns the named step of the named pipeline, as serve
// builds the registry.
func fetchStep(t *testing.T, opts BuildOpts, pipelineName, stepName string) pipeline.PipelineStep {
	t.Helper()
	reg := ConfiguredRegistryWithPipelineOverrides(opts, config.ProvidersConfig{}, config.PipelinesConfig{})
	p, err := reg.Get(pipelineName)
	if err != nil {
		t.Fatalf("pipeline %s: %v", pipelineName, err)
	}
	for _, s := range p.Steps {
		if s.Name() == stepName {
			return s
		}
	}
	t.Fatalf("pipeline %s has no %s step", pipelineName, stepName)
	return nil
}

// Every builtin step that fetches a captured URL goes through
// BuildOpts.FetchGuard: by default a loopback (private) target is refused
// before any request leaves; a guard from fetch.allow_private reaches it.
func TestBuiltinFetchSteps_UseFetchGuard(t *testing.T) {
	var hits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		fmt.Fprint(w, "<html><body>internal</body></html>")
	}))
	defer target.Close()

	var daemonHits atomic.Int32
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		daemonHits.Add(1)
		fmt.Fprint(w, `{"extracts":[{"text":"internal"}]}`)
	}))
	defer daemon.Close()
	browserClient := browser.NewClient(daemon.URL, "test")

	cases := []struct{ pipeline, step string }{
		{"url.generic", "url_fetcher"},
		{"feed.sync", "feed_fetcher"},
		{"url.authenticated", "ibr_fetcher"},
	}
	for _, tc := range cases {
		t.Run(tc.pipeline+"/default", func(t *testing.T) {
			hits.Store(0)
			daemonHits.Store(0)
			s := fetchStep(t, BuildOpts{BrowserClient: browserClient}, tc.pipeline, tc.step)
			_, err := s.Run(context.Background(), &storage.KnowledgeObject{Source: target.URL + "/"})
			if !errors.Is(err, safefetch.ErrBlocked) {
				t.Fatalf("%s: err = %v, want safefetch.ErrBlocked", tc.step, err)
			}
			if hits.Load() != 0 || daemonHits.Load() != 0 {
				t.Fatalf("%s: target hits %d, browser hits %d; want none", tc.step, hits.Load(), daemonHits.Load())
			}
		})
		t.Run(tc.pipeline+"/allow_private", func(t *testing.T) {
			hits.Store(0)
			daemonHits.Store(0)
			guard := safefetch.New(safefetch.Options{Policy: safefetch.Policy{AllowPrivate: true}})
			s := fetchStep(t, BuildOpts{BrowserClient: browserClient, FetchGuard: guard}, tc.pipeline, tc.step)
			if _, err := s.Run(context.Background(), &storage.KnowledgeObject{Source: target.URL + "/"}); err != nil {
				t.Fatalf("%s: %v", tc.step, err)
			}
			if hits.Load()+daemonHits.Load() != 1 {
				t.Fatalf("%s: target hits %d, browser hits %d; want one", tc.step, hits.Load(), daemonHits.Load())
			}
		})
	}
}
