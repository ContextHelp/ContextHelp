package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/searchgraph/viewer"
)

const (
	testViewerToken = "tok_0123456789abcdefghijklmnopqrstuvwxyzABCDEFG"
	testViewerHost  = "127.0.0.1:4242"
)

var testViewerDoc = []byte(`{"graph":{"metadata":{"query":"q <b>"}}}`)

func newTestViewerHandler(t *testing.T) *viewerHandler {
	t.Helper()
	h, err := newViewerHandler(testViewerToken, testViewerDoc, []string{testViewerHost, "localhost:4242"})
	if err != nil {
		t.Fatalf("newViewerHandler: %v", err)
	}
	return h
}

func serveViewer(h http.Handler, method, target, host string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	req.Host = host
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func readAsset(t *testing.T, name string) []byte {
	t.Helper()
	b, err := fs.ReadFile(viewer.Assets(), name)
	if err != nil {
		t.Fatalf("read asset %s: %v", name, err)
	}
	return b
}

// assertViewerHeaders pins the hardening headers every response carries,
// hits and misses alike.
func assertViewerHeaders(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	want := map[string]string{
		"Cache-Control":                "no-store",
		"X-Content-Type-Options":       "nosniff",
		"Referrer-Policy":              "no-referrer",
		"Content-Security-Policy":      viewerCSP,
		"Cross-Origin-Resource-Policy": "same-origin",
		"X-Frame-Options":              "DENY",
	}
	for k, v := range want {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
}

// TestViewerHandler_ServesAssetsAndDocument asserts every file under the
// token prefix is served with its fixed content type, the index at the
// prefix itself, and the data file byte-for-byte as the document.
func TestViewerHandler_ServesAssetsAndDocument(t *testing.T) {
	h := newTestViewerHandler(t)
	prefix := "/" + testViewerToken + "/"
	cases := []struct {
		path, contentType string
		body              []byte
	}{
		{prefix, "text/html; charset=utf-8", readAsset(t, viewer.IndexFile)},
		{prefix + viewer.IndexFile, "text/html; charset=utf-8", readAsset(t, viewer.IndexFile)},
		{prefix + viewer.ScriptFile, "text/javascript; charset=utf-8", readAsset(t, viewer.ScriptFile)},
		{prefix + viewer.StyleFile, "text/css; charset=utf-8", readAsset(t, viewer.StyleFile)},
		{prefix + viewer.NoticesFile, "text/plain; charset=utf-8", readAsset(t, viewer.NoticesFile)},
		{prefix + viewer.DataFile, "application/json", testViewerDoc},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			rec := serveViewer(h, http.MethodGet, tc.path, testViewerHost)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if got := rec.Header().Get("Content-Type"); got != tc.contentType {
				t.Errorf("Content-Type = %q, want %q", got, tc.contentType)
			}
			if !bytes.Equal(rec.Body.Bytes(), tc.body) {
				t.Errorf("body differs from the source (%d vs %d bytes)", rec.Body.Len(), len(tc.body))
			}
			assertViewerHeaders(t, rec)
		})
	}
}

// TestViewerHandler_NotFound asserts that anything but a known file
// under the exact token prefix, on an accepted Host, is a 404 that
// still carries the hardening headers: no listing, no traversal, no
// token prefix match.
func TestViewerHandler_NotFound(t *testing.T) {
	h := newTestViewerHandler(t)
	tok := testViewerToken
	cases := []struct{ name, path, host string }{
		{"root", "/", testViewerHost},
		{"asset without token", "/" + viewer.ScriptFile, testViewerHost},
		{"graph without token", "/" + viewer.DataFile, testViewerHost},
		{"wrong token", "/wrong/", testViewerHost},
		{"wrong token graph", "/wrong/" + viewer.DataFile, testViewerHost},
		{"token prefix only", "/" + tok[:len(tok)-1] + "/" + viewer.DataFile, testViewerHost},
		{"token with suffix", "/" + tok + "x/" + viewer.DataFile, testViewerHost},
		{"empty token", "//" + viewer.DataFile, testViewerHost},
		{"unknown file", "/" + tok + "/secret.txt", testViewerHost},
		{"subdirectory", "/" + tok + "/dist/" + viewer.IndexFile, testViewerHost},
		{"directory", "/" + tok + "/dist/", testViewerHost},
		{"traversal", "/" + tok + "/../" + tok + "/" + viewer.DataFile, testViewerHost},
		{"wrong host", "/" + tok + "/" + viewer.DataFile, "evil.example:4242"},
		{"wrong port", "/" + tok + "/" + viewer.DataFile, "127.0.0.1:4243"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := serveViewer(h, http.MethodGet, tc.path, tc.host)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("GET %s (Host %s) = %d, want 404", tc.path, tc.host, rec.Code)
			}
			if strings.Contains(rec.Body.String(), `"graph"`) || strings.Contains(rec.Body.String(), "<html") {
				t.Errorf("404 body leaks content: %q", rec.Body.String())
			}
			assertViewerHeaders(t, rec)
		})
	}
}

// TestViewerHandler_Localhost accepts the localhost spelling of the
// bound address, which is the same loopback socket.
func TestViewerHandler_Localhost(t *testing.T) {
	rec := serveViewer(newTestViewerHandler(t), http.MethodGet, "/"+testViewerToken+"/"+viewer.DataFile, "localhost:4242")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

// TestViewerHandler_RedirectsBareToken sends /<token> to /<token>/ so the
// page's relative asset URLs resolve under the prefix.
func TestViewerHandler_RedirectsBareToken(t *testing.T) {
	rec := serveViewer(newTestViewerHandler(t), http.MethodGet, "/"+testViewerToken, testViewerHost)
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/"+testViewerToken+"/" {
		t.Errorf("Location = %q", got)
	}
}

// TestViewerHandler_Methods allows GET and HEAD only; any other method
// on a real file is 405, and on a wrong token still 404.
func TestViewerHandler_Methods(t *testing.T) {
	h := newTestViewerHandler(t)
	graph := "/" + testViewerToken + "/" + viewer.DataFile

	head := serveViewer(h, http.MethodHead, graph, testViewerHost)
	if head.Code != http.StatusOK || head.Body.Len() != 0 {
		t.Errorf("HEAD = %d with %d body bytes, want 200 and none", head.Code, head.Body.Len())
	}
	if got, want := head.Header().Get("Content-Length"), strconv.Itoa(len(testViewerDoc)); got != want {
		t.Errorf("HEAD Content-Length = %q, want %s", got, want)
	}

	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodOptions} {
		rec := serveViewer(h, m, graph, testViewerHost)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s = %d, want 405", m, rec.Code)
		}
		if got := rec.Header().Get("Allow"); got != "GET, HEAD" {
			t.Errorf("%s Allow = %q", m, got)
		}
		if wrong := serveViewer(h, m, "/wrong/"+viewer.DataFile, testViewerHost); wrong.Code != http.StatusNotFound {
			t.Errorf("%s with wrong token = %d, want 404", m, wrong.Code)
		}
	}
}

// TestViewerCSP_MatchesPage asserts the header policy is the page's own
// meta policy plus frame-ancestors, so the header never forbids what
// the page needs to render.
func TestViewerCSP_MatchesPage(t *testing.T) {
	m := regexp.MustCompile(`<meta http-equiv="Content-Security-Policy" content="([^"]+)">`).
		FindSubmatch(readAsset(t, viewer.IndexFile))
	if m == nil {
		t.Fatal("index.html has no CSP meta tag")
	}
	if want := string(m[1]) + "; frame-ancestors 'none'"; viewerCSP != want {
		t.Errorf("viewerCSP = %q\nwant       %q", viewerCSP, want)
	}
}

func TestNewViewerToken(t *testing.T) {
	a, err := newViewerToken()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := newViewerToken()
	if a == b {
		t.Error("two tokens are equal")
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`).MatchString(a) {
		t.Errorf("token %q is not 32 url-safe base64 bytes", a)
	}
}

// fakeOpener records the URLs it was asked to open.
type fakeOpener struct {
	mu   sync.Mutex
	urls []string
	err  error
	then func(url string)
}

func (f *fakeOpener) Open(_ context.Context, url string) error {
	f.mu.Lock()
	f.urls = append(f.urls, url)
	f.mu.Unlock()
	if f.then != nil {
		f.then(url)
	}
	return f.err
}

func (f *fakeOpener) opened() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.urls...)
}

var viewerURLPattern = regexp.MustCompile(`^http://127\.0\.0\.1:\d+/[A-Za-z0-9_-]{43}/$`)

// runViewer starts s in the background and returns its URL, a cancel
// func and a channel carrying Run's result.
func runViewer(t *testing.T, s graphViewerServer) (string, context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	urls := make(chan string, 1)
	s.ready = func(u string) { urls <- u }
	done := make(chan error, 1)
	finished := make(chan struct{})
	go func() {
		done <- s.Run(ctx, testViewerDoc)
		close(finished)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("viewer did not stop within 5s of cancel")
		}
	})
	select {
	case u := <-urls:
		return u, cancel, done
	case err := <-done:
		t.Fatalf("viewer exited before ready: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("viewer not ready within 5s")
	}
	return "", cancel, done
}

func httpGet(t *testing.T, url string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

func waitDone(t *testing.T, done <-chan error, within time.Duration) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(within):
		t.Fatalf("viewer still running after %s", within)
		return nil
	}
}

// TestGraphViewerServer_OpensBrowser asserts the opener gets the
// tokenized URL, which the server then answers.
func TestGraphViewerServer_OpensBrowser(t *testing.T) {
	op := &fakeOpener{}
	log := &syncBuffer{}
	url, cancel, done := runViewer(t, graphViewerServer{Log: log, Opener: op, OpenBrowser: true, Idle: time.Minute})

	if !viewerURLPattern.MatchString(url) {
		t.Fatalf("url %q is not a tokenized loopback URL", url)
	}
	if got := op.opened(); len(got) != 1 || got[0] != url {
		t.Fatalf("opener got %v, want [%s]", got, url)
	}
	if !strings.Contains(log.String(), url) || !strings.Contains(log.String(), "Ctrl-C") {
		t.Errorf("log lacks the URL or how to stop:\n%s", log)
	}
	if code, body := httpGet(t, url+viewer.DataFile); code != http.StatusOK || !bytes.Equal(body, testViewerDoc) {
		t.Errorf("graph.json = %d %q", code, body)
	}
	origin := url[:len("http://")+strings.Index(url[len("http://"):], "/")]
	if code, _ := httpGet(t, origin+"/wrong/"+viewer.DataFile); code != http.StatusNotFound {
		t.Errorf("wrong token = %d, want 404", code)
	}

	cancel()
	if err := waitDone(t, done, 5*time.Second); err != nil {
		t.Fatalf("Run after cancel = %v, want nil", err)
	}
	if !strings.Contains(log.String(), "stopped (interrupted)") {
		t.Errorf("log lacks the stop reason:\n%s", log)
	}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if resp, err := http.DefaultClient.Do(req); err == nil {
		resp.Body.Close()
		t.Error("server still answers after Run returned")
	}
}

// TestGraphViewerServer_NoBrowser asserts nothing is opened when the
// caller did not ask for a browser; the URL is still printed.
func TestGraphViewerServer_NoBrowser(t *testing.T) {
	op := &fakeOpener{}
	log := &syncBuffer{}
	url, _, _ := runViewer(t, graphViewerServer{Log: log, Opener: op, OpenBrowser: false, Idle: time.Minute})
	if got := op.opened(); len(got) != 0 {
		t.Errorf("opener called with %v, want no call", got)
	}
	if !strings.Contains(log.String(), url) {
		t.Errorf("log lacks the URL:\n%s", log)
	}
}

// TestGraphViewerServer_OpenerFailureKeepsServing asserts a failed
// opener is a warning, not a stop.
func TestGraphViewerServer_OpenerFailureKeepsServing(t *testing.T) {
	op := &fakeOpener{err: errors.New("no display")}
	log := &syncBuffer{}
	url, _, done := runViewer(t, graphViewerServer{Log: log, Opener: op, OpenBrowser: true, Idle: time.Minute})
	if code, _ := httpGet(t, url); code != http.StatusOK {
		t.Errorf("index = %d, want 200", code)
	}
	select {
	case err := <-done:
		t.Fatalf("Run returned %v after an opener failure", err)
	default:
	}
	if !strings.Contains(log.String(), "no display") {
		t.Errorf("log lacks the opener warning:\n%s", log)
	}
}

// TestGraphViewerServer_IdleTimeout asserts the server stops by itself,
// cleanly, once nothing has asked it for anything for Idle.
func TestGraphViewerServer_IdleTimeout(t *testing.T) {
	log := &syncBuffer{}
	_, _, done := runViewer(t, graphViewerServer{Log: log, Idle: 100 * time.Millisecond})
	if err := waitDone(t, done, 5*time.Second); err != nil {
		t.Fatalf("Run after idle = %v, want nil", err)
	}
	if !strings.Contains(log.String(), "stopped (idle for 100ms)") {
		t.Errorf("log lacks the idle stop:\n%s", log)
	}
}

// TestGraphViewerServer_ActivityDefersIdle asserts requests reset the
// idle clock: a server polled more often than Idle keeps running.
func TestGraphViewerServer_ActivityDefersIdle(t *testing.T) {
	const idle = 400 * time.Millisecond
	url, _, done := runViewer(t, graphViewerServer{Log: io.Discard, Idle: idle})
	for deadline := time.Now().Add(3 * idle); time.Now().Before(deadline); {
		httpGet(t, url)
		time.Sleep(idle / 4)
		select {
		case <-done:
			t.Fatal("server stopped while being polled")
		default:
		}
	}
	if err := waitDone(t, done, 5*time.Second); err != nil {
		t.Fatalf("Run = %v", err)
	}
}

// TestShouldOpenGraphBrowser asserts the browser opens only when both
// output streams are terminals and --no-browser is off.
func TestShouldOpenGraphBrowser(t *testing.T) {
	tty, pipe := &bytes.Buffer{}, &bytes.Buffer{}
	orig := graphTerminal
	t.Cleanup(func() { graphTerminal = orig })
	graphTerminal = func(w io.Writer) bool { return w == tty }

	cases := []struct {
		name           string
		noBrowser      bool
		stdout, stderr io.Writer
		want           bool
	}{
		{"terminal", false, tty, tty, true},
		{"terminal, --no-browser", true, tty, tty, false},
		{"stdout captured", false, pipe, tty, false},
		{"stderr captured", false, tty, pipe, false},
		{"both captured", false, pipe, pipe, false},
	}
	for _, tc := range cases {
		if got := shouldOpenGraphBrowser(tc.noBrowser, tc.stdout, tc.stderr); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
	if isTerminal(&bytes.Buffer{}) {
		t.Error("isTerminal(buffer) = true")
	}
}
