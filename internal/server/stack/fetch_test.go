package stack

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/safefetch"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// runURLFetcher runs the url.generic pipeline's url_fetcher, as built by
// the stack, against src.
func runURLFetcher(t *testing.T, st *Stack, src string) error {
	t.Helper()
	p, err := st.Pipelines.Get("url.generic")
	if err != nil {
		t.Fatalf("url.generic: %v", err)
	}
	for _, s := range p.Steps {
		if s.Name() == "url_fetcher" {
			_, err := s.Run(context.Background(), &storage.KnowledgeObject{Source: src})
			return err
		}
	}
	t.Fatal("url.generic has no url_fetcher")
	return nil
}

// The fetch.* config reaches the pipelines serve runs: by default a
// captured private-network URL is refused; fetch.allow_private and
// fetch.allowed_cidrs open it.
func TestBuildWiresFetchConfig(t *testing.T) {
	var hits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		fmt.Fprint(w, "intranet")
	}))
	defer target.Close()

	st := build(t, &config.Config{}, storageutil.NewTestDriver(t))
	if err := runURLFetcher(t, st, target.URL); !errors.Is(err, safefetch.ErrBlocked) {
		t.Fatalf("default config: err = %v, want safefetch.ErrBlocked", err)
	}
	if hits.Load() != 0 {
		t.Fatalf("default config: target received %d requests", hits.Load())
	}

	for name, f := range map[string]config.FetchConfig{
		"allow_private": {AllowPrivate: true},
		"allowed_cidrs": {AllowedCIDRs: []string{"127.0.0.1"}},
	} {
		t.Run(name, func(t *testing.T) {
			before := hits.Load()
			st := build(t, &config.Config{Fetch: f}, storageutil.NewTestDriver(t))
			if err := runURLFetcher(t, st, target.URL); err != nil {
				t.Fatalf("url_fetcher: %v", err)
			}
			if hits.Load() != before+1 {
				t.Fatalf("target hits %d, want %d", hits.Load(), before+1)
			}
		})
	}
}
