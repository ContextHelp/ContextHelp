//go:build e2e && unix

// Package uiauth_test drives web UI sign-in end to end: a dpkms and a
// ctxt binary built once per run, a protected dpkms on free loopback
// ports with a throwaway config and database, `ctxt ui open
// --no-browser` for the link, and headless Chrome (internal/browser/
// launch: mock keychain, throwaway profile) signing in with it.
//
// Run with:
//
//	go test -count=1 -tags 'fts5 e2e' ./test/e2e/uiauth/...
//
// Chrome reaches dpkms through a pass-through proxy that adds one probe
// script to the sign-in page. The web UI keeps its event stream open,
// and headless Chrome's virtual time never runs out while a request is
// pending, so the DOM would never be dumped; the probe records that the
// stream opened, fetches the API with the cookie, then closes the
// stream. Everything else is the real page talking to the real server.
package uiauth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/ideacrafterslabs/ctxt/internal/browser/launch"
)

const (
	token     = "e2e-uiauth-token-5f0c2a"
	principal = "e2e-ops"
	busToken  = "e2e-uiauth-bus-9d1"
	probePath = "/__e2e/probe.js"
)

var bins struct{ dpkms, ctxt string }

func TestMain(m *testing.M) {
	code, err := setup(m)
	if err != nil {
		fmt.Fprintf(os.Stderr, "uiauth e2e: %v\n", err)
		os.Exit(1)
	}
	os.Exit(code)
}

func setup(m *testing.M) (int, error) {
	work, err := os.MkdirTemp("", "ctxt-uiauth-e2e-")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(work)
	root, err := repoRoot()
	if err != nil {
		return 0, err
	}
	for name, dst := range map[string]*string{"dpkms": &bins.dpkms, "ctxt": &bins.ctxt} {
		*dst = filepath.Join(work, name)
		build := exec.Command("go", "build", "-tags", "fts5", "-buildvcs=false", "-o", *dst, "./cmd/"+name)
		build.Dir = root
		build.Env = append(os.Environ(), "CGO_ENABLED=1")
		if out, err := build.CombinedOutput(); err != nil {
			return 0, fmt.Errorf("go build ./cmd/%s: %w\n%s", name, err, out)
		}
	}
	return m.Run(), nil
}

func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}

// freePort reserves a loopback port and releases it.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// holdCookieBridgePort keeps dpkms's preferred cookie-bridge port busy
// for the test, so the instance under test takes another one and never
// sits where a developer's own dpkms expects to bind.
func holdCookieBridgePort(t *testing.T) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:9377")
	if err != nil {
		return // already taken: dpkms falls back on its own
	}
	t.Cleanup(func() { ln.Close() })
}

// hermeticEnv is the process environment with nothing ctxt, dpkms, kit
// or XDG inherited, and HOME and the XDG roots under dir.
func hermeticEnv(dir string, extra ...string) []string {
	var out []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		switch {
		case strings.HasPrefix(k, "CTXT_"), strings.HasPrefix(k, "CH_"),
			strings.HasPrefix(k, "DPKMS_"), strings.HasPrefix(k, "KIT_"),
			strings.HasPrefix(k, "XDG_"), strings.HasPrefix(k, "GIT_"),
			k == "HOME", k == "BUS_TOKEN":
			continue
		}
		out = append(out, kv)
	}
	return append(append(out,
		"HOME="+dir,
		"XDG_CONFIG_HOME="+filepath.Join(dir, "config"),
		"XDG_DATA_HOME="+filepath.Join(dir, "share"),
		"XDG_STATE_HOME="+filepath.Join(dir, "state"),
		"XDG_CACHE_HOME="+filepath.Join(dir, "cache"),
		"CTXT_EMBEDDING_ENDPOINT=http://127.0.0.1:1",
		"TERM=dumb",
	), extra...)
}

// instance is one protected dpkms behind the probe proxy.
type instance struct {
	dir, dpkmsCfg, ctxtCfg string
	api                    string // dpkms, direct
	proxy                  *probeProxy
}

func startInstance(t *testing.T) *instance {
	t.Helper()
	holdCookieBridgePort(t)
	dir := t.TempDir()
	port, grpcPort := freePort(t), freePort(t)
	in := &instance{
		dir:      dir,
		dpkmsCfg: filepath.Join(dir, "dpkms.yaml"),
		ctxtCfg:  filepath.Join(dir, "ctxt.yaml"),
		api:      "http://127.0.0.1:" + strconv.Itoa(port),
	}
	in.proxy = newProbeProxy(t, in.api)
	proxyHost := strings.TrimPrefix(in.proxy.URL, "http://")

	writeFile(t, in.dpkmsCfg, fmt.Sprintf(`storage:
  type: sqlite
  path: %q
server:
  access: protected
  allowed_hosts:
    - %q
  auth:
    provider: static
    static:
      tokens:
        - token: %q
          principal: %q
          roles: [admin]
browser:
  enabled: false
`, filepath.Join(dir, "dpkms.db"), proxyHost, token, principal))
	writeFile(t, in.ctxtCfg, fmt.Sprintf(`storage:
  path: %q
server:
  url: %q
  token: %q
`, filepath.Join(dir, "ctxt.db"), in.proxy.URL, token))

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, bins.dpkms, "--config", in.dpkmsCfg, "serve", //nolint:gosec // the binary under test
		"--port", strconv.Itoa(port), "--grpc-port", strconv.Itoa(grpcPort), "--name", "uiauth-e2e")
	cmd.Env = hermeticEnv(dir, "BUS_TOKEN="+busToken)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
	var log bytes.Buffer
	cmd.Stdout, cmd.Stderr = &log, &log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		_ = cmd.Wait()
		if t.Failed() {
			t.Logf("dpkms log:\n%s", log.String())
		}
	})
	deadline := time.Now().Add(60 * time.Second)
	for {
		resp, err := http.Get(in.api + "/health")
		if err == nil {
			resp.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("dpkms never answered /health:\n%s", log.String())
		}
		time.Sleep(200 * time.Millisecond)
	}
	return in
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// run executes a built binary in the instance's hermetic environment.
func (in *instance) run(t *testing.T, bin string, args ...string) (stdout, stderr string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...) //nolint:gosec // the binary under test
	cmd.Env = hermeticEnv(in.dir)
	cmd.Dir = in.dir
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %s: %v\nstdout:\n%s\nstderr:\n%s", filepath.Base(bin), strings.Join(args, " "), err, out.String(), errb.String())
	}
	return out.String(), errb.String()
}

// probeJS records that the web UI's event stream opened, fetches the
// API with the session cookie, notes whether script can see the cookie,
// then closes the stream so headless Chrome's virtual time can run out.
// It runs before the app's own script and wraps EventSource.
const probeJS = `(() => {
  const root = document.documentElement;
  const Native = window.EventSource;
  window.EventSource = class extends Native {
    constructor(url, init) {
      super(url, init);
      this.addEventListener('open', async () => {
        root.dataset.probeSse = 'open';
        try {
          const r = await fetch('/api/v1/objects', { credentials: 'same-origin' });
          root.dataset.probeObjects = String(r.status);
        } catch (e) {
          root.dataset.probeObjects = 'error';
        }
        root.dataset.probeCookieVisible = String(document.cookie.includes('__Host-dpkms_'));
        this.close();
      });
    }
  };
})();`

// probeProxy passes every request through to dpkms with its Host header,
// adds the probe to the sign-in page, and logs what reached the server.
type probeProxy struct {
	*httptest.Server
	mu   sync.Mutex
	seen []seenRequest
}

type seenRequest struct {
	method, uri, referer string
	cookie               bool
	status               int
}

func newProbeProxy(t *testing.T, target string) *probeProxy {
	t.Helper()
	u, err := url.Parse(target)
	if err != nil {
		t.Fatal(err)
	}
	p := &probeProxy{}
	rp := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(u)
			r.Out.Host = r.In.Host
		},
		FlushInterval: -1,
		ModifyResponse: func(resp *http.Response) error {
			req := resp.Request
			p.mu.Lock()
			p.seen = append(p.seen, seenRequest{
				method: req.Method, uri: req.URL.RequestURI(), referer: req.Header.Get("Referer"),
				cookie: strings.Contains(req.Header.Get("Cookie"), "__Host-dpkms_"), status: resp.StatusCode,
			})
			p.mu.Unlock()
			if req.Method != http.MethodGet || req.URL.Path != "/ui/auth" ||
				!strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
				return nil
			}
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				return err
			}
			body = bytes.Replace(body, []byte("<head>"), []byte(`<head><script src="`+probePath+`"></script>`), 1)
			resp.Body = io.NopCloser(bytes.NewReader(body))
			resp.ContentLength = int64(len(body))
			resp.Header.Set("Content-Length", strconv.Itoa(len(body)))
			return nil
		},
	}
	p.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == probePath {
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			_, _ = io.WriteString(w, probeJS)
			return
		}
		rp.ServeHTTP(w, r)
	}))
	t.Cleanup(p.Close)
	return p
}

func (p *probeProxy) requests() []seenRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]seenRequest(nil), p.seen...)
}

func chromePath(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("headless browser: -short")
	}
	p, err := launch.FindChrome()
	switch {
	case errors.Is(err, launch.ErrDisabled), errors.Is(err, launch.ErrNotFound):
		t.Skip(err)
	case err != nil:
		t.Fatal(err)
	}
	return p
}

var linkRE = regexp.MustCompile(`http://127\.0\.0\.1:\d+/ui/auth#code=[A-Za-z0-9_-]{43}`)

func attr(dom, name string) string {
	m := regexp.MustCompile(name + `="([^"]*)"`).FindStringSubmatch(dom)
	if m == nil {
		return ""
	}
	return m[1]
}

// codeOf returns the login code in a sign-in link's fragment.
func codeOf(t *testing.T, link string) string {
	t.Helper()
	_, code, ok := strings.Cut(link, "#code=")
	if !ok {
		t.Fatalf("link %q has no code fragment", link)
	}
	return code
}

// signInLink runs ctxt ui open --no-browser and returns the printed link.
func (in *instance) signInLink(t *testing.T) string {
	t.Helper()
	stdout, stderr := in.run(t, bins.ctxt, "--config", in.ctxtCfg, "ui", "open", "--no-browser")
	link := linkRE.FindString(stdout)
	if link == "" || !strings.HasPrefix(link, in.proxy.URL+"/ui/auth#code=") {
		t.Fatalf("no sign-in link on stdout:\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	return link
}

// ctxt ui open --no-browser prints a link; headless Chrome follows it,
// the web UI trades the code for the cookie, shows who it signed in as,
// and its API fetches and event stream work with the cookie alone.
func TestSignInWithLinkFromCLI(t *testing.T) {
	chrome := chromePath(t)
	in := startInstance(t)
	link := in.signInLink(t)

	profileTmp := t.TempDir()
	dom, err := launch.DumpDOM(context.Background(), link, launch.Options{
		Chrome:   chrome,
		Args:     []string{"--virtual-time-budget=15000"},
		Timeout:  60 * time.Second,
		Attempts: 1, // the code is single-use
		Env:      hermeticEnv(profileTmp),
		Dir:      profileTmp,
		TempDir:  profileTmp,
	})
	if err != nil {
		t.Fatalf("headless chrome: %v", err)
	}
	assertSignedInPage(t, dom, codeOf(t, link))
	assertRequestLog(t, in.proxy.requests())

	id := in.onlyChromeSession(t)
	out, _ := in.run(t, bins.dpkms, "--config", in.dpkmsCfg, "session", "revoke", id)
	if !strings.Contains(out, "Revoked "+id) {
		t.Errorf("revoke output: %s", out)
	}
}

// assertSignedInPage checks the dumped sign-in page: who signed in, the
// sidebar's whoami, the open event stream, the probe's API fetch, and
// that neither the cookie nor the code is visible to the page.
func assertSignedInPage(t *testing.T, dom, code string) {
	t.Helper()
	for name, want := range map[string]string{
		"data-auth":                 "signed-in",
		"data-principal":            principal,
		"data-live":                 "open",
		"data-probe-sse":            "open",
		"data-probe-objects":        "200",
		"data-probe-cookie-visible": "false",
	} {
		if got := attr(dom, name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if !strings.Contains(dom, "Signed in as <strong>"+principal+"</strong>") {
		t.Errorf("page does not say who signed in")
	}
	if strings.Contains(dom, code) {
		t.Errorf("the code is still in the page")
	}
	if t.Failed() {
		t.Logf("dom:\n%s", dom)
	}
}

// assertRequestLog checks what reached dpkms: the code never in a URL,
// no Referer on API calls, a successful exchange, and an event stream
// answered 200 to the cookie.
func assertRequestLog(t *testing.T, reqs []seenRequest) {
	t.Helper()
	var sawStream, sawExchange bool
	for _, r := range reqs {
		if strings.Contains(r.uri, "code=") {
			t.Errorf("the code reached the server in a URL: %s %s", r.method, r.uri)
		}
		if strings.HasPrefix(r.uri, "/api/") && r.referer != "" {
			t.Errorf("%s %s sent Referer %q", r.method, r.uri, r.referer)
		}
		sawExchange = sawExchange || (r.method == http.MethodPost && r.uri == "/ui/auth/session" && r.status == http.StatusOK)
		sawStream = sawStream || (r.method == http.MethodGet && r.uri == "/api/v1/events" && r.status == http.StatusOK && r.cookie)
	}
	if !sawExchange || !sawStream {
		t.Errorf("exchange ok = %v, event stream answered 200 to the cookie = %v; requests: %+v", sawExchange, sawStream, reqs)
	}
}

// onlyChromeSession returns the ID of the one session dpkms lists, and
// fails unless it is an active Chrome session of the principal.
func (in *instance) onlyChromeSession(t *testing.T) string {
	t.Helper()
	out, _ := in.run(t, bins.dpkms, "--config", in.dpkmsCfg, "session", "list", "--format", "json")
	var doc struct {
		Sessions []struct {
			ID          string `json:"id"`
			PrincipalID string `json:"principal_id"`
			Status      string `json:"status"`
			UserAgent   string `json:"user_agent"`
		} `json:"sessions"`
	}
	_, rest, ok := strings.Cut(out, "{")
	if !ok || json.Unmarshal([]byte("{"+rest), &doc) != nil {
		t.Fatalf("session list: not JSON:\n%s", out)
	}
	if len(doc.Sessions) != 1 || doc.Sessions[0].PrincipalID != principal || doc.Sessions[0].Status != "active" ||
		!strings.Contains(doc.Sessions[0].UserAgent, "Chrome") {
		t.Fatalf("sessions = %+v, want one active Chrome session for %s", doc.Sessions, principal)
	}
	return doc.Sessions[0].ID
}

// exchange trades code for a session cookie the way the web UI does.
func exchange(t *testing.T, base, code string) (*http.Cookie, int) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, base+"/ui/auth/session", strings.NewReader(`{"code":"`+code+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("X-Ctxt-CSRF", "1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	for _, c := range resp.Cookies() {
		if strings.HasPrefix(c.Name, "__Host-dpkms_") {
			return c, resp.StatusCode
		}
	}
	return nil, resp.StatusCode
}

// sendWithCookie sends a same-origin web UI request carrying cookie.
func sendWithCookie(t *testing.T, cookie *http.Cookie, base, method, path, body string) int {
	t.Helper()
	status, _ := sendWithCookieBody(t, cookie, base, method, path, body)
	return status
}

// sendWithCookieBody is sendWithCookie returning the response body too.
func sendWithCookieBody(t *testing.T, cookie *http.Cookie, base, method, path, body string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, base+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(cookie)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Origin", base)
	req.Header.Set("X-Ctxt-CSRF", "1")
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, out
}

// busAuth opens /ws/bus carrying cookie and offers secret as the bus
// token; nil means the bus acknowledged it.
func busAuth(base string, cookie *http.Cookie, secret string) error {
	hdr := http.Header{}
	hdr.Set("Cookie", cookie.Name+"="+cookie.Value)
	conn, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(base, "http")+"/ws/bus", hdr)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	if err := conn.WriteJSON(map[string]string{"auth": secret}); err != nil {
		return err
	}
	var ack map[string]string
	if err := conn.ReadJSON(&ack); err != nil {
		return err
	}
	if ack["type"] != "auth_ok" {
		return fmt.Errorf("unexpected reply %v", ack)
	}
	return nil
}

// A session cookie opens neither the MCP mount, federation push, the
// audit log, server-side watches, login-code minting nor the
// cross-process bus, and a spent code does not sign in again. Each
// refusal is the route scope the session lacks, although the admin token
// that minted it holds them all.
func TestSessionCookieRefusedOffScope(t *testing.T) {
	in := startInstance(t)
	base := in.proxy.URL
	code := codeOf(t, in.signInLink(t))

	cookie, status := exchange(t, base, code)
	if status != http.StatusOK || cookie == nil {
		t.Fatalf("exchange: %d, cookie %v", status, cookie)
	}
	if _, status := exchange(t, base, code); status != http.StatusUnauthorized {
		t.Errorf("replayed code: %d, want 401", status)
	}
	if got := sendWithCookie(t, cookie, base, http.MethodGet, "/api/v1/objects", ""); got != http.StatusOK {
		t.Fatalf("cookie read: %d, want 200", got)
	}
	for _, tc := range []struct{ method, path, body, scope string }{
		{http.MethodPost, "/api/v1/mcp", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, "read:mcp"},
		{http.MethodPost, "/api/v1/federation/push", `{}`, "write:objects"},
		{http.MethodGet, "/api/v1/audit-log", "", "admin:audit"},
		{http.MethodGet, "/api/v1/watches", "", "read:watches"},
		{http.MethodPost, "/api/v1/ui/login-codes", "", "read:ui"},
	} {
		got, body := sendWithCookieBody(t, cookie, base, tc.method, tc.path, tc.body)
		var env struct {
			Error struct {
				Code    string         `json:"code"`
				Details map[string]any `json:"details"`
			} `json:"error"`
		}
		_ = json.Unmarshal(body, &env)
		if got != http.StatusForbidden || env.Error.Code != "INSUFFICIENT_SCOPE" || env.Error.Details["required_scope"] != tc.scope {
			t.Errorf("%s %s with a session cookie: %d %s, want 403 INSUFFICIENT_SCOPE for %s", tc.method, tc.path, got, body, tc.scope)
		}
	}
	for _, path := range []string{"/api/v1/steps/registries", "/api/v1/whoami"} {
		if got := sendWithCookie(t, cookie, base, http.MethodGet, path, ""); got != http.StatusOK {
			t.Errorf("GET %s with a session cookie: %d, want 200", path, got)
		}
	}
	_, who := sendWithCookieBody(t, cookie, base, http.MethodGet, "/api/v1/whoami", "")
	var whoami struct {
		Via     string   `json:"via"`
		Scopes  []string `json:"scopes"`
		Session *struct {
			Kind string `json:"kind"`
		} `json:"session"`
	}
	if err := json.Unmarshal(who, &whoami); err != nil || whoami.Via != "session" || whoami.Session == nil ||
		whoami.Session.Kind != "ui" || slices.Contains(whoami.Scopes, "admin:audit") || !slices.Contains(whoami.Scopes, "delete:objects") {
		t.Errorf("whoami with a session cookie: %s, want via session, kind ui, the ui scope set", who)
	}

	// The bus accepts the WebSocket upgrade and authenticates the first
	// message against its own bus token; a session cookie, or its secret
	// offered as that token, is not one.
	if err := busAuth(base, cookie, busToken); err != nil {
		t.Fatalf("bus with its token (control): %v", err)
	}
	err := busAuth(base, cookie, cookie.Value)
	var ce *websocket.CloseError
	if !errors.As(err, &ce) || ce.Code != websocket.ClosePolicyViolation {
		t.Errorf("bus with a session cookie: %v, want a policy-violation close", err)
	}
}
