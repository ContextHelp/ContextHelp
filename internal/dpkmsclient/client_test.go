package dpkmsclient_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"hop.top/kit/go/console/output"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
	"github.com/ideacrafterslabs/ctxt/internal/testguard"
)

// dpkmsError writes the dpkms error envelope ({"error":{code,message}}).
func dpkmsError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{"code": code, "message": msg, "details": map[string]any{}},
	})
}

// countingServer answers every request with h and counts the hits.
func countingServer(t *testing.T, h http.HandlerFunc) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func newClient(t *testing.T, ep dpkmsclient.Endpoint, opts ...dpkmsclient.Option) *dpkmsclient.Client {
	t.Helper()
	c, err := dpkmsclient.New(ep, opts...)
	if err != nil {
		t.Fatalf("New(%q): %v", ep.URL, err)
	}
	return c
}

// envelope asserts err is a kit envelope and returns it.
func envelope(t *testing.T, err error) *output.Error {
	t.Helper()
	if err == nil {
		t.Fatal("want an error, got nil")
	}
	var e *output.Error
	if !errors.As(err, &e) {
		t.Fatalf("error %T (%v) is not a kit *output.Error", err, err)
	}
	return e
}

// TestStatusClasses pins ADR-077 §2's exit classes for every status class
// dpkms answers with, and that the dpkms code and message reach the
// envelope.
func TestStatusClasses(t *testing.T) {
	cases := []struct {
		status   int
		code     string
		wantCode string
		wantExit int
	}{
		{http.StatusBadRequest, "INVALID_REQUEST", output.CodeUsage, 2},
		{http.StatusUnauthorized, "UNAUTHORIZED", output.CodeUnauthorized, 5},
		{http.StatusForbidden, "ENTITLEMENT_REQUIRED", output.CodeUnauthorized, 5},
		{http.StatusNotFound, "NOT_FOUND", output.CodeNotFound, 3},
		{http.StatusConflict, "POLICY_DENIED", output.CodeConflict, 4},
		{http.StatusUnprocessableEntity, "PIPELINE_NOT_FOUND", output.CodeUsage, 2},
		{http.StatusTooManyRequests, "QUOTA_EXHAUSTED", output.CodeRateLimited, 64},
		{http.StatusInternalServerError, "INTERNAL_ERROR", output.CodeTransient, 6},
		{http.StatusBadGateway, "UPSTREAM", output.CodeTransient, 6},
		{http.StatusServiceUnavailable, "UNAVAILABLE", output.CodeTransient, 6},
		{http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", output.CodeGeneric, 1},
	}
	for _, tc := range cases {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			srv, _ := countingServer(t, func(w http.ResponseWriter, _ *http.Request) {
				dpkmsError(w, tc.status, tc.code, "the dpkms message")
			})
			c := newClient(t, dpkmsclient.Endpoint{URL: srv.URL})
			err := c.Get(context.Background(), "/api/v1/objects/x", nil, nil)
			e := envelope(t, err)
			if e.Code != tc.wantCode || e.ExitCode != tc.wantExit {
				t.Fatalf("status %d: got %s/%d, want %s/%d", tc.status, e.Code, e.ExitCode, tc.wantCode, tc.wantExit)
			}
			if !strings.Contains(e.Message, tc.code) || !strings.Contains(e.Message, "the dpkms message") {
				t.Errorf("envelope message %q lacks dpkms code %q and message", e.Message, tc.code)
			}
			var re *dpkmsclient.RemoteError
			if !errors.As(err, &re) {
				t.Fatalf("envelope does not retain *RemoteError: %v", err)
			}
			if re.StatusCode != tc.status || re.Code != tc.code || re.Message != "the dpkms message" {
				t.Errorf("RemoteError = %+v", re)
			}
		})
	}
}

// TestStatusNonEnvelopeBody keeps a non-JSON error body readable.
func TestStatusNonEnvelopeBody(t *testing.T) {
	srv, _ := countingServer(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "plain upstream failure", http.StatusBadGateway)
	})
	c := newClient(t, dpkmsclient.Endpoint{URL: srv.URL})
	err := c.Get(context.Background(), "/healthz", nil, nil)
	e := envelope(t, err)
	if e.ExitCode != 6 || !strings.Contains(e.Message, "plain upstream failure") {
		t.Fatalf("got %d %q", e.ExitCode, e.Message)
	}
	var re *dpkmsclient.RemoteError
	if !errors.As(err, &re) || !strings.Contains(string(re.Body), "plain upstream failure") {
		t.Fatalf("RemoteError body not kept: %v", err)
	}
}

// TestAuthRejectionIsFinal: a 401 or 403 ends the request. One request
// reaches dpkms, nothing is retried, and no second instance exists to
// fall over to.
func TestAuthRejectionIsFinal(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv, hits := countingServer(t, func(w http.ResponseWriter, _ *http.Request) {
				dpkmsError(w, status, "UNAUTHORIZED", "invalid token")
			})
			c := newClient(t, dpkmsclient.Endpoint{URL: srv.URL, Token: "wrong"})
			_, err := c.Analyze(context.Background(), dpkmsclient.AnalyzeRequest{Content: "x"})
			e := envelope(t, err)
			if e.ExitCode != output.ExitUnauthorized {
				t.Fatalf("exit %d, want %d", e.ExitCode, output.ExitUnauthorized)
			}
			if e.Transience != output.TransiencePermanent {
				t.Errorf("transience %q, want permanent", e.Transience)
			}
			if n := hits.Load(); n != 1 {
				t.Fatalf("dpkms saw %d requests, want exactly 1", n)
			}
			if strings.Contains(e.Message+e.SuggestedFix+e.Cause, "wrong") {
				t.Errorf("envelope leaks the token: %+v", e)
			}
		})
	}
}

// TestUnreachable: nothing answering at the endpoint is PREREQUISITE (70),
// however the dial fails.
func TestUnreachable(t *testing.T) {
	blockingDial := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	dnsFail := &http.Transport{
		DialContext: func(_ context.Context, _, addr string) (net.Conn, error) {
			return nil, &net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{
				Err: "no such host", Name: addr, IsNotFound: true,
			}}
		},
	}
	tlsSrv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	tlsSrv.Config.ErrorLog = discardLog()
	t.Cleanup(tlsSrv.Close)

	cases := []struct {
		name string
		url  string
		opts []dpkmsclient.Option
	}{
		{"connection refused", testguard.ClosedServerURL, nil},
		{"dns failure", "http://dpkms.example.invalid:7700", []dpkmsclient.Option{dpkmsclient.WithTransport(dnsFail)}},
		{"dial timeout", "http://192.0.2.1:7700", []dpkmsclient.Option{
			dpkmsclient.WithTransport(blockingDial), dpkmsclient.WithTimeout(100 * time.Millisecond),
		}},
		{"untrusted certificate", tlsSrv.URL, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newClient(t, dpkmsclient.Endpoint{URL: tc.url, Token: "tok"}, tc.opts...)
			e := envelope(t, c.Get(context.Background(), "/healthz", nil, nil))
			if e.Code != output.CodePrerequisite || e.ExitCode != 70 {
				t.Fatalf("got %s/%d (%s), want PREREQUISITE/70", e.Code, e.ExitCode, e.Message)
			}
			if !strings.Contains(e.Message, tc.url) {
				t.Errorf("message %q does not name the endpoint", e.Message)
			}
			if e.SuggestedFix == "" {
				t.Error("no suggested fix")
			}
		})
	}
}

// TestFailureAfterConnecting: once a connection is up, a timeout or a
// dropped connection is TRANSIENT (6), not "unreachable".
func TestFailureAfterConnecting(t *testing.T) {
	release := make(chan struct{})
	slow, _ := countingServer(t, func(http.ResponseWriter, *http.Request) { <-release })
	t.Cleanup(func() { close(release) })
	hangup, _ := countingServer(t, func(w http.ResponseWriter, _ *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
	})

	cases := []struct {
		name string
		url  string
	}{
		{"timeout", slow.URL},
		{"connection dropped", hangup.URL},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newClient(t, dpkmsclient.Endpoint{URL: tc.url}, dpkmsclient.WithTimeout(200*time.Millisecond))
			e := envelope(t, c.Post(context.Background(), "/api/v1/analyze", map[string]string{"content": "x"}, nil))
			if e.Code != output.CodeTransient || e.ExitCode != 6 {
				t.Fatalf("got %s/%d (%s), want TRANSIENT/6", e.Code, e.ExitCode, e.Message)
			}
		})
	}
}

// TestCanceledIsNotClassified: an operator interrupt is not a dpkms
// failure and keeps context.Canceled matchable.
func TestCanceledIsNotClassified(t *testing.T) {
	release := make(chan struct{})
	srv, _ := countingServer(t, func(http.ResponseWriter, *http.Request) { <-release })
	t.Cleanup(func() { close(release) })
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	c := newClient(t, dpkmsclient.Endpoint{URL: srv.URL})
	err := c.Get(ctx, "/api/v1/objects", nil, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
}

// TestRequestShape: every request carries the bearer token and speaks
// JSON; the token is never overridable by a caller header.
func TestRequestShape(t *testing.T) {
	type seen struct {
		method, path, query, auth, ctype, accept, note string
		body                                           map[string]any
	}
	var got seen
	srv, _ := countingServer(t, func(w http.ResponseWriter, r *http.Request) {
		got = seen{
			method: r.Method, path: r.URL.Path, query: r.URL.RawQuery,
			auth: r.Header.Get("Authorization"), ctype: r.Header.Get("Content-Type"),
			accept: r.Header.Get("Accept"), note: r.Header.Get("X-Ctxt-Note"),
		}
		if r.Body != nil {
			raw, _ := io.ReadAll(r.Body)
			if len(raw) > 0 {
				_ = json.Unmarshal(raw, &got.body)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"obj-1"}`))
	})
	c := newClient(t, dpkmsclient.Endpoint{URL: srv.URL + "/", Token: "s3cret"})

	var out struct {
		ID string `json:"id"`
	}
	if err := c.Get(context.Background(), "/api/v1/objects", url.Values{"limit": {"5"}}, &out); err != nil {
		t.Fatal(err)
	}
	if got.method != http.MethodGet || got.path != "/api/v1/objects" || got.query != "limit=5" {
		t.Errorf("GET went to %s %s?%s", got.method, got.path, got.query)
	}
	if got.auth != "Bearer s3cret" || got.accept != "application/json" || got.ctype != "" {
		t.Errorf("GET headers: auth=%q accept=%q content-type=%q", got.auth, got.accept, got.ctype)
	}
	if out.ID != "obj-1" {
		t.Errorf("decoded %+v", out)
	}

	if err := c.Post(context.Background(), "/api/v1/feeds", map[string]string{"url": "u"}, nil); err != nil {
		t.Fatal(err)
	}
	if got.method != http.MethodPost || got.auth != "Bearer s3cret" || got.ctype != "application/json" || got.body["url"] != "u" {
		t.Errorf("POST: %+v", got)
	}

	err := c.Do(context.Background(), dpkmsclient.Request{
		Method: http.MethodDelete, Path: "/api/v1/feeds/f1",
		Header: http.Header{"X-Ctxt-Note": {"why"}, "Authorization": {"Bearer attacker"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.method != http.MethodDelete || got.note != "why" || got.auth != "Bearer s3cret" {
		t.Errorf("DELETE: %+v", got)
	}
}

// TestNoTokenNoHeader: an endpoint without a token sends no Authorization.
func TestNoTokenNoHeader(t *testing.T) {
	var auth []string
	srv, _ := countingServer(t, func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Values("Authorization")
		w.WriteHeader(http.StatusNoContent)
	})
	c := newClient(t, dpkmsclient.Endpoint{URL: srv.URL})
	var out map[string]any
	if err := c.Get(context.Background(), "/api/v1/x", nil, &out); err != nil {
		t.Fatal(err)
	}
	if len(auth) != 0 {
		t.Fatalf("Authorization sent without a token: %v", auth)
	}
}

// TestUndecodableSuccess: a 2xx body that is not the expected JSON is a
// protocol failure, not success.
func TestUndecodableSuccess(t *testing.T) {
	srv, _ := countingServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html>captive portal</html>"))
	})
	c := newClient(t, dpkmsclient.Endpoint{URL: srv.URL})
	var out map[string]any
	e := envelope(t, c.Get(context.Background(), "/api/v1/x", nil, &out))
	if e.ExitCode != output.ExitGeneric || !strings.Contains(e.Message, srv.URL) {
		t.Fatalf("got %d %q", e.ExitCode, e.Message)
	}
}

func TestNewRejectsBadURL(t *testing.T) {
	for _, u := range []string{"", "127.0.0.1:8080", "ftp://host", "http://"} {
		_, err := dpkmsclient.New(dpkmsclient.Endpoint{URL: u})
		e := envelope(t, err)
		if e.ExitCode != output.ExitUsage {
			t.Errorf("New(%q): exit %d, want %d", u, e.ExitCode, output.ExitUsage)
		}
	}
}

func TestURLHidesToken(t *testing.T) {
	c := newClient(t, dpkmsclient.Endpoint{URL: "https://dpkms.example.ts.net:7700/", Token: "s3cret"})
	if got := c.URL(); got != "https://dpkms.example.ts.net:7700" {
		t.Fatalf("URL() = %q", got)
	}
}
