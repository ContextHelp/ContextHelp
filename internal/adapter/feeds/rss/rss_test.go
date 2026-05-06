package rss

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	xrr "hop.top/xrr"
	xhttp "hop.top/xrr/adapters/http"

	"github.com/ideacrafterslabs/ctxt/internal/adapter"
	"github.com/ideacrafterslabs/ctxt/internal/ingest"
)

// TestAdapterIdentity locks the protocol and backend identifiers the
// substrate's Registry uses to enforce one-platform-per-protocol.
func TestAdapterIdentity(t *testing.T) {
	a := New(Config{})
	if got := a.Protocol(); got != "feeds" {
		t.Errorf("Protocol = %q, want feeds", got)
	}
	if got := a.Backend(); got != "rss" {
		t.Errorf("Backend = %q, want rss", got)
	}
}

// TestAdapterDeclaresFetchOnly confirms the rss adapter is
// capability-honest: it only declares fetch + emit-events. The
// substrate's gating depends on this honesty so undeclared methods
// fail loud rather than silently no-oping.
func TestAdapterDeclaresFetchOnly(t *testing.T) {
	a := New(Config{})
	if !adapter.HasCapability(a, adapter.CapFetch) {
		t.Error("missing CapFetch")
	}
	if !adapter.HasCapability(a, adapter.CapEmitEvents) {
		t.Error("missing CapEmitEvents")
	}
	for _, c := range a.Capabilities() {
		if c == adapter.CapServe || c == adapter.CapSubmit {
			t.Errorf("unexpected capability %q on fetch-only sensor", c)
		}
	}
}

// TestAdapterRejectsUndeclaredCapabilities pins the substrate contract:
// methods for capabilities NOT declared MUST return
// ErrCapabilityNotDeclared (errors.Is matches).
func TestAdapterRejectsUndeclaredCapabilities(t *testing.T) {
	a := New(Config{})
	if err := a.Submit(context.Background(), ingest.Object{ID: "x"}); !errors.Is(err, adapter.ErrCapabilityNotDeclared) {
		t.Errorf("Submit: want ErrCapabilityNotDeclared, got %v", err)
	}
	if err := a.Serve(context.Background(), nil); !errors.Is(err, adapter.ErrCapabilityNotDeclared) {
		t.Errorf("Serve: want ErrCapabilityNotDeclared, got %v", err)
	}
}

// TestFetchRSS exercises the happy path against a recorded RSS 2.0
// cassette. Recording: set XRR_RECORD=1 to hit the fixture httptest
// server live and re-write the cassette under cassettes/rss_feed/.
// Default mode is replay — the cassette acts as a frozen fixture, no
// network or process interaction.
func TestFetchRSS(t *testing.T) {
	const body = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Example Feed</title>
    <link>https://example.com/</link>
    <description>An example feed.</description>
    <item>
      <title>First post</title>
      <link>https://example.com/1</link>
      <guid>https://example.com/1</guid>
      <pubDate>Mon, 05 May 2026 10:00:00 GMT</pubDate>
      <description>First body.</description>
    </item>
    <item>
      <title>Second post</title>
      <link>https://example.com/2</link>
      <guid>https://example.com/2</guid>
      <pubDate>Mon, 05 May 2026 11:00:00 GMT</pubDate>
      <description>Second body.</description>
    </item>
  </channel>
</rss>`
	rt, feedURL := newCassetteTransport(t, "cassettes/rss_feed", body, "application/rss+xml")
	a := New(Config{FeedURLs: []string{feedURL}, Transport: rt})
	objs, err := a.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(objs) != 2 {
		t.Fatalf("Fetch: got %d objects, want 2", len(objs))
	}
	if objs[0].Type != "feed-item" {
		t.Errorf("Type = %q, want feed-item", objs[0].Type)
	}
	if objs[0].ID == "" || objs[0].ID == objs[1].ID {
		t.Errorf("expected distinct non-empty IDs, got %q and %q", objs[0].ID, objs[1].ID)
	}
}

// TestFetchAtom exercises the parser on an Atom 1.0 feed. RSS and Atom
// share the same protocol slot per the spec; the adapter handles both.
func TestFetchAtom(t *testing.T) {
	const body = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>Atom Example</title>
  <id>https://example.com/atom</id>
  <updated>2026-05-05T10:00:00Z</updated>
  <entry>
    <title>Atom post</title>
    <id>tag:example.com,2026:atom-1</id>
    <link href="https://example.com/atom-1"/>
    <updated>2026-05-05T10:00:00Z</updated>
    <summary>Atom body.</summary>
  </entry>
</feed>`
	rt, feedURL := newCassetteTransport(t, "cassettes/atom_feed", body, "application/atom+xml")
	a := New(Config{FeedURLs: []string{feedURL}, Transport: rt})
	objs, err := a.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(objs) != 1 {
		t.Fatalf("Fetch: got %d objects, want 1", len(objs))
	}
	if objs[0].Type != "feed-item" {
		t.Errorf("Type = %q, want feed-item", objs[0].Type)
	}
}

// newCassetteTransport returns an http.RoundTripper that drives the
// adapter's HTTP calls through an xrr session, plus a stable feed URL
// the cassette is keyed against. In record mode (XRR_RECORD=1) it
// stands up a httptest fixture serving body and writes the resulting
// HTTP exchange into cassetteDir. In replay mode it never touches the
// fixture — the recorded cassette IS the fixture.
//
// The feed URL is hard-coded to https://feeds.test/<dir> so the
// fingerprint stays stable across runs and machines (httptest.URL
// changes per-run; xrr fingerprints include the URL path).
func newCassetteTransport(t *testing.T, cassetteDir, body, contentType string) (http.RoundTripper, string) {
	t.Helper()

	feedURL := "https://feeds.test/" + strings.TrimPrefix(cassetteDir, "cassettes/")

	mode := xrr.ModeReplay
	if os.Getenv("XRR_RECORD") != "" {
		mode = xrr.ModeRecord
		if err := os.MkdirAll(cassetteDir, 0o755); err != nil {
			t.Fatalf("mkdir cassettes: %v", err)
		}
	}
	sess := xrr.NewSession(mode, xrr.NewFileCassette(cassetteDir))
	httpAdapter := xhttp.NewAdapter()

	// Live fixture: only stood up in record mode. In replay mode it is
	// nil and the round-tripper path skips the live call entirely.
	var liveSrv *httptest.Server
	if mode == xrr.ModeRecord {
		liveSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", contentType)
			_, _ = w.Write([]byte(body))
		}))
		t.Cleanup(liveSrv.Close)
	}

	return roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		// Build xrr request keyed off the stable feed URL, NOT the
		// httptest URL — the cassette must replay across machines and
		// across record runs that pick a different httptest port.
		var bodyBytes []byte
		if req.Body != nil {
			b, err := io.ReadAll(req.Body)
			if err != nil {
				return nil, err
			}
			bodyBytes = b
			_ = req.Body.Close()
		}
		xreq := &xhttp.Request{
			Method: req.Method,
			URL:    feedURL,
			Body:   string(bodyBytes),
		}

		do := func() (xrr.Response, error) {
			// Re-target the request at the httptest fixture so the
			// recording captures the body served by `body`.
			liveURL := liveSrv.URL + req.URL.Path
			liveReq, err := http.NewRequestWithContext(req.Context(), req.Method, liveURL, http.NoBody)
			if err != nil {
				return nil, err
			}
			for k, vs := range req.Header {
				for _, v := range vs {
					liveReq.Header.Add(k, v)
				}
			}
			httpResp, err := http.DefaultTransport.RoundTrip(liveReq)
			if err != nil {
				return nil, err
			}
			defer httpResp.Body.Close()
			respBody, err := io.ReadAll(httpResp.Body)
			if err != nil {
				return nil, err
			}
			headers := map[string]string{}
			for k, vs := range httpResp.Header {
				if len(vs) > 0 {
					headers[k] = vs[0]
				}
			}
			return &xhttp.Response{
				Status:  httpResp.StatusCode,
				Headers: headers,
				Body:    string(respBody),
			}, nil
		}

		resp, err := sess.Record(req.Context(), httpAdapter, xreq, do)
		if err != nil {
			return nil, err
		}
		return xrrResponseToHTTP(resp)
	}), feedURL
}

// xrrResponseToHTTP converts an xrr.Response (either *xhttp.Response
// in record mode or *xrr.RawResponse in replay mode) into an
// *http.Response usable by the adapter under test.
func xrrResponseToHTTP(resp xrr.Response) (*http.Response, error) {
	var (
		status  int
		headers map[string]string
		body    string
	)
	switch r := resp.(type) {
	case *xhttp.Response:
		status, headers, body = r.Status, r.Headers, r.Body
	case *xrr.RawResponse:
		// Cassette numeric fields come back as int via YAML decode but
		// can decode as float64 via JSON round-trip on other replay
		// paths — accept both. Mirrors test/integration/us0219_mcp_test.go's
		// rawToHTTP. Default to 200 if absent so the converter is lenient
		// for cassettes that omit the field.
		status = 200
		switch n := r.Payload["status"].(type) {
		case int:
			status = n
		case float64:
			status = int(n)
		}
		if v, ok := r.Payload["body"].(string); ok {
			body = v
		}
		if h, ok := r.Payload["headers"].(map[string]any); ok {
			headers = make(map[string]string, len(h))
			for k, v := range h {
				if s, ok := v.(string); ok {
					headers[k] = s
				}
			}
		}
	default:
		return nil, errors.New("rss: unsupported xrr response type")
	}
	hdr := http.Header{}
	for k, v := range headers {
		hdr.Set(k, v)
	}
	return &http.Response{
		StatusCode: status,
		Header:     hdr,
		Body:       io.NopCloser(strings.NewReader(body)),
	}, nil
}

// roundTripperFunc lets a function value satisfy http.RoundTripper.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
