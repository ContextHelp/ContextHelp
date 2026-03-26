package steps

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// capture records the last request received by a test server.
type capture struct {
	req *http.Request
}

func (c *capture) handler(code int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c.req = r.Clone(context.Background())
		w.WriteHeader(code)
	}
}

func TestClientBridge_DefaultUserAgent(t *testing.T) {
	var cap capture
	srv := httptest.NewServer(cap.handler(http.StatusOK))
	defer srv.Close()

	cb := NewClientBridge(WithBridgeHTTPClient(srv.Client()))
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := cb.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()

	if got := cap.req.Header.Get("User-Agent"); got != DefaultUserAgent {
		t.Errorf("user-agent: got %q, want %q", got, DefaultUserAgent)
	}
}

func TestClientBridge_CustomUserAgent(t *testing.T) {
	var cap capture
	srv := httptest.NewServer(cap.handler(http.StatusOK))
	defer srv.Close()

	cb := NewClientBridge(
		WithBridgeHTTPClient(srv.Client()),
		WithBridgeUserAgent("MyBot/2.0"),
	)
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := cb.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()

	if got := cap.req.Header.Get("User-Agent"); got != "MyBot/2.0" {
		t.Errorf("user-agent: got %q, want %q", got, "MyBot/2.0")
	}
}

func TestClientBridge_PresetUserAgentNotOverridden(t *testing.T) {
	var cap capture
	srv := httptest.NewServer(cap.handler(http.StatusOK))
	defer srv.Close()

	cb := NewClientBridge(WithBridgeHTTPClient(srv.Client()))
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("User-Agent", "CallerBot/1.0")
	resp, err := cb.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()

	if got := cap.req.Header.Get("User-Agent"); got != "CallerBot/1.0" {
		t.Errorf("user-agent: got %q, want %q", got, "CallerBot/1.0")
	}
}

func TestClientBridge_ExtraHeader(t *testing.T) {
	var cap capture
	srv := httptest.NewServer(cap.handler(http.StatusOK))
	defer srv.Close()

	cb := NewClientBridge(
		WithBridgeHTTPClient(srv.Client()),
		WithBridgeHeader("X-Custom", "hello"),
	)
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := cb.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()

	if got := cap.req.Header.Get("X-Custom"); got != "hello" {
		t.Errorf("X-Custom: got %q, want %q", got, "hello")
	}
}

func TestClientBridge_DomainCredential_Injected(t *testing.T) {
	var cap capture
	srv := httptest.NewServer(cap.handler(http.StatusOK))
	defer srv.Close()

	host := "127.0.0.1"
	cb := NewClientBridge(
		WithBridgeHTTPClient(srv.Client()),
		WithBridgeDomainCredential(DomainCredential{
			Host:   host,
			Header: "Authorization",
			Value:  "Bearer tok-abc",
		}),
	)
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := cb.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()

	if got := cap.req.Header.Get("Authorization"); got != "Bearer tok-abc" {
		t.Errorf("Authorization: got %q, want %q", got, "Bearer tok-abc")
	}
}

func TestClientBridge_DomainCredential_NoMatchSkipped(t *testing.T) {
	var cap capture
	srv := httptest.NewServer(cap.handler(http.StatusOK))
	defer srv.Close()

	cb := NewClientBridge(
		WithBridgeHTTPClient(srv.Client()),
		WithBridgeDomainCredential(DomainCredential{
			Host:  "other.example.com",
			Value: "Bearer should-not-appear",
		}),
	)
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := cb.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()

	if got := cap.req.Header.Get("Authorization"); got != "" {
		t.Errorf("Authorization: expected empty, got %q", got)
	}
}

func TestClientBridge_DomainCredential_ExistingHeaderNotOverridden(t *testing.T) {
	var cap capture
	srv := httptest.NewServer(cap.handler(http.StatusOK))
	defer srv.Close()

	host := "127.0.0.1"
	cb := NewClientBridge(
		WithBridgeHTTPClient(srv.Client()),
		WithBridgeDomainCredential(DomainCredential{
			Host:  host,
			Value: "Bearer injected",
		}),
	)
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("Authorization", "Bearer caller-set")
	resp, err := cb.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()

	if got := cap.req.Header.Get("Authorization"); got != "Bearer caller-set" {
		t.Errorf("Authorization: got %q, want %q", got, "Bearer caller-set")
	}
}

func TestClientBridge_Timeout(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until client disconnects (simulates slow server).
		<-r.Context().Done()
	}))
	defer slow.Close()

	cb := NewClientBridge(WithBridgeTimeout(50 * time.Millisecond))
	req, _ := http.NewRequest(http.MethodGet, slow.URL, nil)
	_, err := cb.Do(req)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

func TestClientBridge_CookieJar(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "abc"})
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cb := NewClientBridge(
		WithBridgeHTTPClient(srv.Client()),
		WithBridgeCookieJar(nil), // create a fresh jar
	)
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := cb.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()

	// Cookie jar should have stored the cookie.
	jar := cb.HTTPClient().Jar
	if jar == nil {
		t.Fatal("expected non-nil cookie jar")
	}
}

func TestClientBridge_HTTPClient(t *testing.T) {
	inner := &http.Client{Timeout: 5 * time.Second}
	cb := NewClientBridge(WithBridgeHTTPClient(inner))
	if cb.HTTPClient() != inner {
		t.Error("HTTPClient() should return the injected client")
	}
}
