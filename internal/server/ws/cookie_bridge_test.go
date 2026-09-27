package ws

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	httpserver "github.com/ideacrafterslabs/ctxt/internal/server/http"
)

// loopbackHosts returns the Host allowlist serve builds for the bridge:
// the loopback names on the port it bound, nothing else.
func loopbackHosts(t *testing.T, port string) *httpserver.HostAllowlist {
	t.Helper()
	hosts, err := httpserver.NewHostAllowlist([]string{
		net.JoinHostPort("127.0.0.1", port),
		net.JoinHostPort("localhost", port),
	})
	if err != nil {
		t.Fatalf("allowlist: %v", err)
	}
	return hosts
}

func listenLoopback(t *testing.T) (net.Listener, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	return ln, strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
}

func TestCookieBridgeServe_ReturnsListenerFailure(t *testing.T) {
	ln, port := listenLoopback(t)
	_ = ln.Close()

	srv := NewCookieBridgeServer(NewCookieCache(), loopbackHosts(t, port))
	if err := srv.Serve(context.Background(), ln); err == nil {
		t.Fatal("Serve on a dead listener returned nil; the failure must reach the caller")
	}
}

func TestCookieBridgeServe_StopsOnCancel(t *testing.T) {
	ln, port := listenLoopback(t)
	addr := ln.Addr().String()

	srv := NewCookieBridgeServer(NewCookieCache(), loopbackHosts(t, port))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx, ln) }()

	// Serving on the listener we handed over, not a re-bound port.
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	_ = conn.Close()

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Serve after cancel: %v", err)
		}
	case <-time.After(cookieBridgeShutdownTimeout + time.Second):
		t.Fatal("Serve did not return after cancel")
	}
	if c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond); err == nil {
		_ = c.Close()
		t.Error("listener still accepting after Serve returned")
	}
}

// bridgeUnderTest runs a CookieBridgeServer through Serve on a real
// loopback listener. The Host allowlist names the listener's own port,
// as serve wires it for the port the bridge binds.
type bridgeUnderTest struct {
	cache *CookieCache
	port  string
}

func startBridge(t *testing.T, hosts func(port string) *httpserver.HostAllowlist) *bridgeUnderTest {
	t.Helper()
	ln, port := listenLoopback(t)
	cache := NewCookieCache()
	srv := NewCookieBridgeServer(cache, hosts(port))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx, ln) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(cookieBridgeShutdownTimeout + time.Second):
			t.Error("bridge did not stop")
		}
	})
	return &bridgeUnderTest{cache: cache, port: port}
}

func startAllowedBridge(t *testing.T) *bridgeUnderTest {
	t.Helper()
	return startBridge(t, func(port string) *httpserver.HostAllowlist { return loopbackHosts(t, port) })
}

// dial opens a WebSocket to the bridge. origin "" sends no Origin
// header; host "" sends the dialed 127.0.0.1:<port>.
func (b *bridgeUnderTest) dial(t *testing.T, origin, host string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	h := http.Header{}
	if origin != "" {
		h.Set("Origin", origin)
	}
	if host != "" {
		h.Set("Host", host)
	}
	d := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	return d.Dial("ws://"+net.JoinHostPort("127.0.0.1", b.port)+"/", h)
}

func syncMsg(t *testing.T, domain string) []byte {
	t.Helper()
	b, err := json.Marshal(SyncMessage{
		Type:    "cookie_sync",
		Domain:  domain,
		Cookies: []CookieEntry{{Name: "sid", Value: "planted", Domain: domain, Path: "/"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// expectRefused asserts the handshake was answered 403 and that
// nothing reached the cache.
func (b *bridgeUnderTest) expectRefused(t *testing.T, origin, host string) {
	t.Helper()
	conn, resp, err := b.dial(t, origin, host)
	if err == nil {
		// Push a cookie so a wrongly accepted socket shows up in the cache too.
		_ = conn.WriteMessage(websocket.TextMessage, syncMsg(t, "victim.example"))
		time.Sleep(100 * time.Millisecond)
		conn.Close()
		t.Fatalf("origin %q host %q: handshake accepted, want 403 (cache: %v)",
			origin, host, b.cache.Get("victim.example"))
	}
	if resp == nil {
		t.Fatalf("origin %q host %q: no HTTP response: %v", origin, host, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("origin %q host %q: status %d, want 403", origin, host, resp.StatusCode)
	}
	if got := b.cache.Get("victim.example"); got != nil {
		t.Fatalf("origin %q host %q: cache written: %v", origin, host, got)
	}
}

func TestCookieBridge_RejectsForeignOrigin(t *testing.T) {
	b := startAllowedBridge(t)
	for _, origin := range []string{
		"https://evil.example",
		"http://127.0.0.1:8080",         // another local service
		"http://localhost:5173",         // Vite dev server: trusted by --dev CORS only
		"null",                          // sandboxed iframe, file://
		"https://chrome-extension.evil", // look-alike, not the scheme
		"http://evil.example/chrome-extension://x",
	} {
		b.expectRefused(t, origin, "")
	}
}

func TestCookieBridge_RejectsMissingOrigin(t *testing.T) {
	b := startAllowedBridge(t)
	b.expectRefused(t, "", "")
}

func TestCookieBridge_AcceptsExtensionOrigin(t *testing.T) {
	b := startAllowedBridge(t)
	cases := []struct{ origin, host, domain string }{
		{"chrome-extension://abcdefghijklmnopabcdefghijklmnop", "", "chrome.example"},
		{"moz-extension://0b5f8a8e-7a3c-4f57-9a1a-3b0f7c1d2e4f", "localhost:" + b.port, "firefox.example"},
		{"safari-web-extension://4E2A1C5B-0000-0000-0000-000000000000", "", "safari.example"},
	}
	for _, tc := range cases {
		conn, resp, err := b.dial(t, tc.origin, tc.host)
		if err != nil {
			status := 0
			if resp != nil {
				status = resp.StatusCode
				resp.Body.Close()
			}
			t.Fatalf("origin %q host %q: handshake failed (status %d): %v", tc.origin, tc.host, status, err)
		}
		if err := conn.WriteMessage(websocket.TextMessage, syncMsg(t, tc.domain)); err != nil {
			t.Fatalf("write: %v", err)
		}
		deadline := time.Now().Add(2 * time.Second)
		for b.cache.Get(tc.domain) == nil && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		conn.Close()
		got := b.cache.Get(tc.domain)
		if len(got) != 1 || got[0].Value != "planted" {
			t.Fatalf("origin %q: cache for %s = %v, want the synced cookie", tc.origin, tc.domain, got)
		}
	}
}

func TestCookieBridge_RejectsRebindingHost(t *testing.T) {
	b := startAllowedBridge(t)
	ext := "chrome-extension://abcdefghijklmnopabcdefghijklmnop"
	for _, host := range []string{
		"evil.example:" + b.port, // rebinding: hostile name resolved to 127.0.0.1
		"evil.example",
		"localhost:1", // loopback name, wrong port
		"[::1]:" + b.port,
	} {
		// An extension origin isolates the Host rule from the Origin rule.
		b.expectRefused(t, ext, host)
		// What a rebinding page actually sends.
		b.expectRefused(t, "http://"+host, host)
	}
}

func TestCookieBridge_NilHostsRefusesEverything(t *testing.T) {
	b := startBridge(t, func(string) *httpserver.HostAllowlist { return nil })
	b.expectRefused(t, "chrome-extension://abcdefghijklmnopabcdefghijklmnop", "")
}

func TestCookieBridge_PlainHTTPDoesNotReachCache(t *testing.T) {
	b := startAllowedBridge(t)
	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:"+b.port+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "https://evil.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status %d, want 403", resp.StatusCode)
	}
}

// TestGuardBridge checks the pre-upgrade guard on its own; the
// upgrader's CheckOrigin would otherwise mask a hole in it.
func TestGuardBridge(t *testing.T) {
	hosts := loopbackHosts(t, "9377")
	ext := "chrome-extension://abcdefghijklmnopabcdefghijklmnop"
	cases := []struct {
		name, host, origin string
		nilHosts           bool
		wantCode           string // "" = passed to next
	}{
		{"extension", "127.0.0.1:9377", ext, false, ""},
		{"extension via localhost", "localhost:9377", "moz-extension://x", false, ""},
		{"foreign origin", "127.0.0.1:9377", "https://evil.example", false, "CROSS_ORIGIN_REQUEST"},
		{"missing origin", "127.0.0.1:9377", "", false, "CROSS_ORIGIN_REQUEST"},
		{"rebinding host", "evil.example:9377", ext, false, "HOST_NOT_ALLOWED"},
		{"wrong port", "127.0.0.1:8080", ext, false, "HOST_NOT_ALLOWED"},
		{"nil hosts", "127.0.0.1:9377", ext, true, "HOST_NOT_ALLOWED"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })
			h := hosts
			if tc.nilHosts {
				h = nil
			}
			req := httptest.NewRequest(http.MethodGet, "http://"+tc.host+"/", nil)
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			rec := httptest.NewRecorder()
			guardBridge(h, next).ServeHTTP(rec, req)
			if tc.wantCode == "" {
				if !called || rec.Code != http.StatusOK {
					t.Fatalf("want pass-through, got status %d called=%v body=%s", rec.Code, called, rec.Body)
				}
				return
			}
			if called {
				t.Fatal("next handler reached")
			}
			var env httpserver.ErrorEnvelope
			if rec.Code != http.StatusForbidden || json.Unmarshal(rec.Body.Bytes(), &env) != nil || env.Error.Code != tc.wantCode {
				t.Fatalf("want 403 %s, got %d %s", tc.wantCode, rec.Code, rec.Body)
			}
		})
	}
}

func TestUpgraderCheckOrigin(t *testing.T) {
	for origin, want := range map[string]bool{
		"chrome-extension://abcdefghijklmnopabcdefghijklmnop": true,
		"":                     false,
		"https://evil.example": false,
		// gorilla's default admits Origin matching Host; this must not.
		"http://127.0.0.1:9377": false,
	} {
		req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:9377/", nil)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if got := upgrader.CheckOrigin(req); got != want {
			t.Errorf("CheckOrigin(%q) = %v, want %v", origin, got, want)
		}
	}
}
