package integration

// Shared xrr record/replay plumbing for the US-02xx capture suite.
//
// The capture tests (arXiv, Twitter, GitHub, LinkedIn, Wikipedia, OSINT)
// drive pipeline steps that fetch an upstream API over HTTP. Historically
// each test stood up an httptest server whose handler hand-built the
// response body, so the assertions checked the test's own idea of the
// upstream payload rather than a real round-trip.
//
// These helpers move the fetch onto an xrr session. The seam is the
// *http.Client each fetch step now accepts: in replay mode the client's
// transport answers from a cassette on disk and the fixture server is
// never contacted; in record mode the round-trip is executed and the
// request/response pair is written to testdata/cassettes/<name>/.
//
// SCOPE — what the committed cassettes actually contain:
//
// The cassettes in this repo were recorded against the in-repo fixture
// servers, NOT against live api.arxiv.org / api.github.com / etc. They
// therefore capture a real HTTP round-trip (wire bytes, status, headers
// as served) of a payload we authored. They do NOT prove the upstream
// API still looks like that. What they buy over the previous inline
// handlers is a single, named, reviewable artifact per interaction that
// (a) is diffable when someone changes the expected shape and (b) can be
// RE-RECORDED against the real upstream by pointing the fixture base URL
// at the live host and running with XRR_MODE=record. Until someone does
// that, treat fidelity as "recorded fixture", not "recorded upstream".
//
// Re-record (against the in-repo fixture servers):
//
//	INTEGRATION=1 XRR_MODE=record go test -count=1 ./test/integration/ -run TestUS02
//
// Default mode is replay.

import (
	"bytes"
	"errors"
	"io"
	gohttp "net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hop.top/xrr"
	xrrhttp "hop.top/xrr/adapters/http"
)

// captureTransport records/replays HTTP round-trips through an xrr session.
//
// It mirrors the xrrTransport in us0219_mcp_test.go but additionally
// preserves the response Content-Type, because the capture pipeline steps
// decode XML and JSON and a bare 200 with no headers is not enough to
// reproduce the original interaction faithfully.
type captureTransport struct {
	session *xrr.FileSession
	adapter *xrrhttp.Adapter
	base    gohttp.RoundTripper
}

func (t *captureTransport) RoundTrip(req *gohttp.Request) (*gohttp.Response, error) {
	var body []byte
	if req.Body != nil {
		body, _ = io.ReadAll(req.Body)
		_ = req.Body.Close()
	}

	xrrReq := &xrrhttp.Request{
		Method:  req.Method,
		URL:     req.URL.String(),
		Headers: captureRequestHeaders(req),
		Body:    string(body),
	}

	resp, err := t.session.Record(req.Context(), t.adapter, xrrReq, func() (xrr.Response, error) {
		req.Body = io.NopCloser(bytes.NewReader(body))
		req.ContentLength = int64(len(body))
		r, err := t.base.RoundTrip(req)
		if err != nil {
			return nil, err
		}
		var rb []byte
		if r.Body != nil {
			rb, _ = io.ReadAll(r.Body)
			_ = r.Body.Close()
		}
		return &xrrhttp.Response{
			Status:  r.StatusCode,
			Headers: captureResponseHeaders(r),
			Body:    string(rb),
		}, nil
	})
	if err != nil {
		return nil, err
	}

	switch v := resp.(type) {
	case *xrrhttp.Response:
		// Record path: xrr hands back the typed response verbatim.
		return captureHTTPResponse(v.Status, v.Headers, v.Body), nil
	case *xrr.RawResponse:
		// Replay path: payload is an untyped map decoded from YAML.
		return captureRawToHTTP(v), nil
	default:
		return nil, errors.New("xrr: unexpected response type from capture session")
	}
}

// captureRequestHeaders selects the request headers that are part of the
// interaction's meaning. Cookie is included because US-0200 asserts the
// authenticated fetch actually sends one; everything else (User-Agent,
// Accept-Encoding, hop-by-hop) is noise that would churn the cassette.
//
// Note these headers are recorded for review only — the xrr http adapter
// fingerprints on method + path + query + body hash, so adding a header
// never changes which cassette a request matches.
func captureRequestHeaders(req *gohttp.Request) map[string]string {
	out := make(map[string]string)
	for _, k := range []string{"Content-Type", "Accept", "Cookie", "Authorization"} {
		if v := req.Header.Get(k); v != "" {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func captureResponseHeaders(resp *gohttp.Response) map[string]string {
	out := make(map[string]string)
	for _, k := range []string{"Content-Type"} {
		if v := resp.Header.Get(k); v != "" {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func captureHTTPResponse(status int, headers map[string]string, body string) *gohttp.Response {
	h := make(gohttp.Header)
	for k, v := range headers {
		h.Set(k, v)
	}
	return &gohttp.Response{
		StatusCode: status,
		Status:     gohttp.StatusText(status),
		Header:     h,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// captureRawToHTTP converts a replayed xrr payload back into an
// *http.Response. YAML decoding yields float64 for numbers and
// map[string]any for nested maps, so both shapes are handled.
func captureRawToHTTP(v *xrr.RawResponse) *gohttp.Response {
	status := gohttp.StatusOK
	body := ""
	headers := map[string]string{}

	if v != nil && v.Payload != nil {
		switch n := v.Payload["status"].(type) {
		case int:
			status = n
		case float64:
			status = int(n)
		}
		if bs, ok := v.Payload["body"].(string); ok {
			body = bs
		}
		if hs, ok := v.Payload["headers"].(map[string]any); ok {
			for k, raw := range hs {
				if s, ok := raw.(string); ok {
					headers[k] = s
				}
			}
		}
	}
	return captureHTTPResponse(status, headers, body)
}

// newCaptureSession builds an xrr session for the named cassette.
// XRR_MODE selects record/replay/passthrough; replay is the default so
// CI never depends on a fixture server being reachable.
func newCaptureSession(t *testing.T, cassetteName string) *xrr.FileSession {
	t.Helper()

	mode := os.Getenv("XRR_MODE")
	if mode == "" {
		mode = string(xrr.ModeReplay)
	}

	baseDir := os.Getenv("XRR_CASSETTE_DIR")
	if baseDir == "" {
		baseDir = filepath.Join("testdata", "cassettes")
	}
	cassetteDir := filepath.Join(baseDir, cassetteName)

	m := xrr.Mode(mode)
	if m == xrr.ModePassthrough {
		return xrr.NewSession(m, nil)
	}
	if m == xrr.ModeRecord {
		if err := os.MkdirAll(cassetteDir, 0o750); err != nil {
			t.Fatalf("xrr: mkdir cassette dir: %v", err)
		}
	}
	return xrr.NewSession(m, xrr.NewFileCassette(cassetteDir))
}

// captureClient returns an *http.Client whose transport records or
// replays through the named cassette. Pass it to a fetch step's Client
// field; the step then never talks to the fixture server on replay.
func captureClient(t *testing.T, cassetteName string) *gohttp.Client {
	t.Helper()
	return &gohttp.Client{
		Transport: &captureTransport{
			session: newCaptureSession(t, cassetteName),
			adapter: xrrhttp.NewAdapter(),
			base:    gohttp.DefaultTransport,
		},
	}
}

// captureGet issues a GET through the supplied client, falling back to
// the default client when nil. Fetch steps call this so a step with no
// injected client keeps its previous behaviour.
func captureGet(c *gohttp.Client, url string) (*gohttp.Response, error) {
	if c == nil {
		c = gohttp.DefaultClient
	}
	return c.Get(url)
}

// ---------------------------------------------------------------------------
// Cassette recording
// ---------------------------------------------------------------------------

// captureFixture describes one recordable interaction: the handler that
// serves it, the request that provokes it, and the cassette it lands in.
//
// The capture tests build their fixture server from the same Handler, so
// a cassette and the fixture it was recorded from cannot drift apart
// without the compiler noticing.
type captureFixture struct {
	// Cassette is the directory name under testdata/cassettes/.
	Cassette string
	// Handler serves the fixture response.
	Handler gohttp.Handler
	// Path is the request path+query, relative to the fixture server.
	// Use Paths instead when one cassette covers several round-trips
	// (e.g. a step that fetches a summary and then an article).
	Path string
	// Paths lists several request path+query values recorded into the
	// same cassette directory, in order. Each gets its own fingerprint,
	// so replay matches whichever the step asks for.
	Paths []string
	// Method defaults to GET.
	Method string
	// Body is the request body, if any.
	Body string
	// Header carries request headers that the handler inspects.
	Header gohttp.Header
	// Cookies are attached to the request before it is sent. US-0200's
	// authenticated fetch depends on one.
	Cookies []*gohttp.Cookie
}

// recordCaptureFixtures (re-)records each fixture into its cassette.
//
// It is a no-op unless XRR_MODE=record, so it costs nothing on a normal
// run. It deliberately does NOT call startTestEnv: only the HTTP seam is
// exercised, so recording needs no Postgres or Redis. That is what makes
// the cassettes refreshable on a laptop.
func recordCaptureFixtures(t *testing.T, fixtures []captureFixture) {
	t.Helper()

	if os.Getenv("XRR_MODE") != string(xrr.ModeRecord) {
		t.Skip("set XRR_MODE=record to re-record cassettes")
	}

	for _, f := range fixtures {
		t.Run(f.Cassette, func(t *testing.T) {
			srv := httptest.NewServer(f.Handler)
			defer srv.Close()

			method := f.Method
			if method == "" {
				method = gohttp.MethodGet
			}

			paths := f.Paths
			if len(paths) == 0 {
				paths = []string{f.Path}
			}

			// One client, so every path in this fixture lands in the
			// same cassette directory.
			client := captureClient(t, f.Cassette)

			for _, path := range paths {
				var body io.Reader
				if f.Body != "" {
					body = strings.NewReader(f.Body)
				}
				req, err := gohttp.NewRequest(method, srv.URL+path, body)
				if err != nil {
					t.Fatalf("build request %s: %v", path, err)
				}
				for k, vs := range f.Header {
					for _, v := range vs {
						req.Header.Add(k, v)
					}
				}
				for _, c := range f.Cookies {
					req.AddCookie(c)
				}

				resp, err := client.Do(req)
				if err != nil {
					t.Fatalf("record %s %s: %v", f.Cassette, path, err)
				}
				if _, err := io.Copy(io.Discard, resp.Body); err != nil {
					_ = resp.Body.Close()
					t.Fatalf("drain %s %s: %v", f.Cassette, path, err)
				}
				_ = resp.Body.Close()
			}
		})
	}
}
