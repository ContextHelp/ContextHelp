package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHostAllowlistAllows(t *testing.T) {
	a, err := NewHostAllowlist([]string{"127.0.0.1:18947", "localhost:18947", "dpkms.lan", "[::1]:9000"})
	require.NoError(t, err)

	for _, h := range []string{
		"127.0.0.1:18947",
		"localhost:18947",
		"LocalHost:18947",
		"dpkms.lan",
		"dpkms.lan:8443",
		"DPKMS.LAN:18947",
		"[::1]:9000",
	} {
		assert.True(t, a.Allows(h, false), "%s must be allowed", h)
	}
	for _, h := range []string{
		"",
		"evil.example:18947",
		"evil.example",
		"localhost:8080",
		"127.0.0.1:18948",
		"127.0.0.1",
		"localhost",
		"[::1]:9001",
		"dpkms.lan.evil.example",
		"127.0.0.1.nip.io:18947",
	} {
		assert.False(t, a.Allows(h, false), "%s must be rejected", h)
	}
}

// A Host without a port means the scheme default, so an exact
// host:80 / host:443 entry still matches a port-less Host.
func TestHostAllowlistDefaultPort(t *testing.T) {
	a, err := NewHostAllowlist([]string{"proxy.example:443", "plain.example:80"})
	require.NoError(t, err)
	assert.True(t, a.Allows("proxy.example", true))
	assert.False(t, a.Allows("proxy.example", false))
	assert.True(t, a.Allows("plain.example", false))
}

func TestNewHostAllowlistRejectsBadEntry(t *testing.T) {
	_, err := NewHostAllowlist([]string{"http://dpkms.lan"})
	require.Error(t, err)
}

// Every route answers 403 HOST_NOT_ALLOWED to a rebinding Host: API,
// SSE, MCP, federation, static UI, health, unknown paths, and routes
// mounted on the router after construction (dpkms serve adds /ws/bus).
func TestRouterRejectsForeignHostOnEveryRoute(t *testing.T) {
	bundle := newTestServerBundle(t)
	defer bundle.Close()

	hosts, err := NewHostAllowlist([]string{"127.0.0.1:18947", "localhost:18947"})
	require.NoError(t, err)
	router := NewRouterWithConfig(bundle.svc, RouterConfig{Hosts: hosts})
	router.Handle("/ws/bus", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	routes := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/objects"},
		{http.MethodGet, "/api/v1/events"},
		{http.MethodPost, "/api/v1/mcp"},
		{http.MethodPost, "/api/v1/federation/push"},
		{http.MethodPost, "/api/v1/inbox"},
		{http.MethodOptions, "/api/v1/inbox"},
		{http.MethodGet, "/ui/"},
		{http.MethodGet, "/ui"},
		{http.MethodGet, "/health"},
		{http.MethodGet, "/healthz"},
		{http.MethodGet, "/manifest.json"},
		{http.MethodGet, "/ws/bus"},
		{http.MethodGet, "/no/such/route"},
	}
	for _, rt := range routes {
		req := httptest.NewRequest(rt.method, rt.path, strings.NewReader(`{}`))
		req.Host = "evil.example:18947"
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		require.Equal(t, http.StatusForbidden, rr.Code, "%s %s", rt.method, rt.path)
		var env ErrorEnvelope
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &env), "%s %s", rt.method, rt.path)
		assert.Equal(t, "HOST_NOT_ALLOWED", env.Error.Code)
		assert.Contains(t, env.Error.Message, "server.allowed_hosts")
	}

	for _, host := range []string{"127.0.0.1:18947", "localhost:18947"} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/objects", nil)
		req.Host = host
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code, host)

		req = httptest.NewRequest(http.MethodGet, "/ws/bus", nil)
		req.Host = host
		rr = httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code, host)
	}
}

// Without an allowlist the router keeps answering any Host (protected
// and public instances with no server.allowed_hosts).
func TestRouterWithoutHostsAnswersAnyHost(t *testing.T) {
	bundle := newTestServerBundle(t)
	defer bundle.Close()

	router := NewRouterWithConfig(bundle.svc, RouterConfig{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/objects", nil)
	req.Host = "192.168.1.20:8080"
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}
