package steps

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync/atomic"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/browser"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/safefetch"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// staticResolver answers host lookups from a fixed table, so no test
// here reaches a real DNS server.
type staticResolver map[string][]netip.Addr

func (r staticResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	if ips, ok := r[host]; ok {
		return ips, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
}

func countingServer(t *testing.T, body string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func assertBlockedPermanent(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, safefetch.ErrBlocked) {
		t.Fatalf("err = %v, want safefetch.ErrBlocked", err)
	}
	var perm *pipeline.PermanentError
	if !errors.As(err, &perm) {
		t.Fatalf("err = %v, want a PermanentError (no retry)", err)
	}
}

func allowPrivate() *safefetch.Guard {
	return safefetch.New(safefetch.Options{Policy: safefetch.Policy{AllowPrivate: true}})
}

// The httptest server binds loopback: the default fetcher must refuse it
// without sending a request.
func TestURLFetcher_DefaultRefusesPrivateAddress(t *testing.T) {
	srv, hits := countingServer(t, "internal")
	_, err := NewURLFetcher().Run(context.Background(), &storage.KnowledgeObject{Source: srv.URL + "/admin"})
	assertBlockedPermanent(t, err)
	if hits.Load() != 0 {
		t.Fatalf("private server received %d requests", hits.Load())
	}
}

func TestURLFetcher_FetchGuardAllowsPrivateWhenConfigured(t *testing.T) {
	srv, hits := countingServer(t, "intranet page")
	got, err := NewURLFetcher(WithURLFetchGuard(allowPrivate())).Run(context.Background(), &storage.KnowledgeObject{Source: srv.URL})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.RawContent != "intranet page" || hits.Load() != 1 {
		t.Fatalf("content %q, hits %d", got.RawContent, hits.Load())
	}
}

func TestFeedFetcher_DefaultRefusesPrivateAddress(t *testing.T) {
	srv, hits := countingServer(t, "<rss/>")
	_, err := NewFeedFetcher().Run(context.Background(), &storage.KnowledgeObject{Source: srv.URL + "/feed.xml"})
	assertBlockedPermanent(t, err)
	if hits.Load() != 0 {
		t.Fatalf("private server received %d requests", hits.Load())
	}
}

func TestFeedFetcher_FetchGuardAllowsPrivateWhenConfigured(t *testing.T) {
	srv, hits := countingServer(t, "<rss/>")
	got, err := NewFeedFetcher(WithFeedFetchGuard(allowPrivate())).Run(context.Background(), &storage.KnowledgeObject{Source: srv.URL})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.RawContent != "<rss/>" || hits.Load() != 1 {
		t.Fatalf("content %q, hits %d", got.RawContent, hits.Load())
	}
}

func TestIBRFetcher_RefusesPrivateURLBeforeBrowser(t *testing.T) {
	daemon, hits := countingServer(t, `{"extracts":[]}`)
	g := safefetch.New(safefetch.Options{Resolver: staticResolver{
		"intranet.example": {netip.MustParseAddr("10.0.0.7")},
	}})
	f := NewIBRFetcher(browser.NewClient(daemon.URL, "test"), WithIBRFetchGuard(g))
	for _, src := range []string{
		"http://169.254.169.254/latest/meta-data/",
		"http://[::ffff:127.0.0.1]:8080/",
		"https://intranet.example/wiki",
	} {
		_, err := f.Run(context.Background(), &storage.KnowledgeObject{Source: src})
		assertBlockedPermanent(t, err)
	}
	if hits.Load() != 0 {
		t.Fatalf("browser daemon received %d commands for private URLs", hits.Load())
	}
}
