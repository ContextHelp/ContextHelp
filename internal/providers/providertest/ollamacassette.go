// Package providertest holds test-only helpers for provider packages.
//
// Ollama interactions in tests are recorded, never faked: OllamaClient
// returns an *http.Client whose transport replays committed xrr cassettes.
// Recording runs the same tests against a real Ollama once:
//
//	XRR_MODE=record go test -tags fts5 -count=1 ./internal/providers/ ./internal/embeddings/
//
// XRR_OLLAMA_UPSTREAM selects the Ollama that answers while recording
// (default http://127.0.0.1:11434). The recorded models must be pulled
// there first (ollama pull snowflake-arctic-embed2).
//
// Import from _test.go files only.
package providertest

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"hop.top/xrr"
	xhttp "hop.top/xrr/adapters/http"
)

// DefaultOllamaUpstream is the Ollama recorded against when
// XRR_OLLAMA_UPSTREAM is unset.
const DefaultOllamaUpstream = "http://127.0.0.1:11434"

// OllamaCalls records the requests a provider issued, as seen before the
// cassette answers them. Tests use it to assert where a provider pointed
// (for example a tunnel port) even though replay never dials.
type OllamaCalls struct {
	mu   sync.Mutex
	urls []string
}

// URLs returns the request URLs in call order.
func (c *OllamaCalls) URLs() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.urls...)
}

func (c *OllamaCalls) add(u string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.urls = append(c.urls, u)
}

// OllamaClient returns an HTTP client that replays Ollama interactions from
// cassetteDir, or records them when XRR_MODE=record.
//
// Replay never opens a connection. Record forwards each request to the
// recording upstream, keeping path, query and body: the request's own host
// is swapped for the upstream, which is exactly what an SSH tunnel does, so
// a provider pointed at a tunnel port records against the real Ollama.
// Cassettes keep only the Content-Type response header; Date and other
// per-run headers are dropped so re-recording yields a minimal diff.
func OllamaClient(t testing.TB, cassetteDir string) (*http.Client, *OllamaCalls) {
	t.Helper()

	mode := xrr.ModeReplay
	if os.Getenv(xrr.EnvMode) == string(xrr.ModeRecord) {
		mode = xrr.ModeRecord
		if err := os.MkdirAll(cassetteDir, 0o750); err != nil {
			t.Fatalf("mkdir %s: %v", cassetteDir, err)
		}
	}
	upstream := os.Getenv("XRR_OLLAMA_UPSTREAM")
	if upstream == "" {
		upstream = DefaultOllamaUpstream
	}
	up, err := url.Parse(upstream)
	if err != nil {
		t.Fatalf("XRR_OLLAMA_UPSTREAM %q: %v", upstream, err)
	}

	sess := xrr.NewSession(mode, xrr.NewFileCassette(cassetteDir))
	adapter := xhttp.NewAdapter()
	calls := &OllamaCalls{}

	rt := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		calls.add(req.URL.String())
		var body []byte
		if req.Body != nil {
			b, err := io.ReadAll(req.Body)
			if err != nil {
				return nil, err
			}
			_ = req.Body.Close()
			body = b
		}
		xreq := &xhttp.Request{Method: req.Method, URL: req.URL.String(), Body: string(body)}

		resp, err := sess.Record(req.Context(), adapter, xreq, func() (xrr.Response, error) {
			target := *req.URL
			target.Scheme, target.Host = up.Scheme, up.Host
			// #nosec G704 -- record mode only, in tests: the target is the
			// operator-chosen recording upstream (XRR_OLLAMA_UPSTREAM).
			live, err := http.NewRequestWithContext(req.Context(), req.Method, target.String(), bytes.NewReader(body))
			if err != nil {
				return nil, err
			}
			live.Header = req.Header.Clone()
			r, err := http.DefaultTransport.RoundTrip(live)
			if err != nil {
				return nil, err
			}
			defer r.Body.Close()
			rb, err := io.ReadAll(r.Body)
			if err != nil {
				return nil, err
			}
			hdr := map[string]string{}
			if ct := r.Header.Get("Content-Type"); ct != "" {
				hdr["Content-Type"] = ct
			}
			return &xhttp.Response{Status: r.StatusCode, Headers: hdr, Body: string(rb)}, nil
		})
		if err != nil {
			return nil, err
		}
		return toHTTP(resp, req)
	})
	return &http.Client{Transport: rt}, calls
}

// toHTTP converts a recorded (*xhttp.Response) or replayed
// (*xrr.RawResponse) interaction into an *http.Response.
func toHTTP(resp xrr.Response, req *http.Request) (*http.Response, error) {
	var (
		status  int
		headers map[string]string
		body    string
	)
	switch r := resp.(type) {
	case *xhttp.Response:
		status, headers, body = r.Status, r.Headers, r.Body
	case *xrr.RawResponse:
		status = http.StatusOK
		switch n := r.Payload["status"].(type) {
		case int:
			status = n
		case float64:
			status = int(n)
		}
		body, _ = r.Payload["body"].(string)
		if h, ok := r.Payload["headers"].(map[string]any); ok {
			headers = make(map[string]string, len(h))
			for k, v := range h {
				if s, ok := v.(string); ok {
					headers[k] = s
				}
			}
		}
	default:
		return nil, errors.New("providertest: unexpected xrr response type")
	}
	hdr := http.Header{}
	for k, v := range headers {
		hdr.Set(k, v)
	}
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     hdr,
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
