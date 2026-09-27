package http

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

const testHost = "127.0.0.1:18947"

type csrfCase struct {
	name        string
	method      string
	path        string
	body        string
	contentType string
	origin      string
	fetchSite   string
	wantStatus  int
	wantCode    string
}

func serveCase(t *testing.T, router http.Handler, tc csrfCase) *httptest.ResponseRecorder {
	t.Helper()
	var body io.Reader
	if tc.body != "" {
		body = strings.NewReader(tc.body)
	}
	req := httptest.NewRequest(tc.method, tc.path, body)
	req.Host = testHost
	if tc.contentType != "" {
		req.Header.Set("Content-Type", tc.contentType)
	}
	if tc.origin != "" {
		req.Header.Set("Origin", tc.origin)
	}
	if tc.fetchSite != "" {
		req.Header.Set("Sec-Fetch-Site", tc.fetchSite)
	}
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	return rr
}

func envelopeCode(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()
	var env ErrorEnvelope
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		return ""
	}
	return env.Error.Code
}

const inboxBody = `{"content":"csrf-probe"}`

// A web page can send a cross-site "simple" POST (text/plain, form or
// no Content-Type) without a CORS preflight. Such writes must never
// reach a handler, whatever Content-Type they claim.
func TestCrossSiteWritesRefused(t *testing.T) {
	bundle := newTestServerBundle(t)
	defer bundle.Close()
	router := NewRouterWithConfig(bundle.svc, RouterConfig{})

	cases := []csrfCase{
		{name: "text/plain no-cors from another site", method: http.MethodPost, path: "/api/v1/inbox", body: inboxBody,
			contentType: "text/plain;charset=UTF-8", origin: "https://evil.example", fetchSite: "cross-site",
			wantStatus: http.StatusForbidden, wantCode: "CROSS_ORIGIN_REQUEST"},
		{name: "json from another site", method: http.MethodPost, path: "/api/v1/inbox", body: inboxBody,
			contentType: "application/json", origin: "https://evil.example", fetchSite: "cross-site",
			wantStatus: http.StatusForbidden, wantCode: "CROSS_ORIGIN_REQUEST"},
		{name: "another localhost port (same-site)", method: http.MethodPost, path: "/api/v1/capture/page",
			body: `{"url":"https://a.example","title":"t","content":"c"}`, contentType: "application/json",
			origin: "http://localhost:3000", fetchSite: "same-site",
			wantStatus: http.StatusForbidden, wantCode: "CROSS_ORIGIN_REQUEST"},
		{name: "old browser, foreign Origin only", method: http.MethodPost, path: "/api/v1/inbox", body: inboxBody,
			contentType: "text/plain", origin: "https://evil.example",
			wantStatus: http.StatusForbidden, wantCode: "CROSS_ORIGIN_REQUEST"},
		{name: "cross-site delete", method: http.MethodDelete, path: "/api/v1/objects/x",
			origin: "https://evil.example", fetchSite: "cross-site",
			wantStatus: http.StatusForbidden, wantCode: "CROSS_ORIGIN_REQUEST"},
		{name: "cross-site bodyless retry", method: http.MethodPost, path: "/api/v1/jobs/x/retry",
			origin: "https://evil.example", fetchSite: "cross-site",
			wantStatus: http.StatusForbidden, wantCode: "CROSS_ORIGIN_REQUEST"},
		{name: "cross-site MCP call", method: http.MethodPost, path: "/api/v1/mcp",
			body: `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, contentType: "text/plain",
			origin: "https://evil.example", fetchSite: "cross-site",
			wantStatus: http.StatusForbidden, wantCode: "CROSS_ORIGIN_REQUEST"},
		{name: "vite dev origin without --dev", method: http.MethodPost, path: "/api/v1/inbox", body: inboxBody,
			contentType: "application/json", origin: "http://localhost:5173", fetchSite: "same-site",
			wantStatus: http.StatusForbidden, wantCode: "CROSS_ORIGIN_REQUEST"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := serveCase(t, router, tc)
			assert.Equal(t, tc.wantStatus, rr.Code, rr.Body.String())
			assert.Equal(t, tc.wantCode, envelopeCode(t, rr))
		})
	}

	items, _, err := bundle.svc.Store.Objects().List(t.Context(), storage.ObjectFilter{})
	require.NoError(t, err)
	assert.Empty(t, items, "a refused request must not store anything")
}

// A JSON body must say so. text/plain, form encodings and a missing
// Content-Type are what a page can send without a preflight.
func TestWriteBodiesMustBeJSON(t *testing.T) {
	bundle := newTestServerBundle(t)
	defer bundle.Close()
	router := NewRouterWithConfig(bundle.svc, RouterConfig{})

	for _, ct := range []string{"", "text/plain", "text/plain;charset=UTF-8", "application/x-www-form-urlencoded", "multipart/form-data; boundary=x", "application/jsonx", "not a media type"} {
		t.Run("inbox "+ct, func(t *testing.T) {
			rr := serveCase(t, router, csrfCase{method: http.MethodPost, path: "/api/v1/inbox", body: inboxBody, contentType: ct})
			assert.Equal(t, http.StatusUnsupportedMediaType, rr.Code, rr.Body.String())
			assert.Equal(t, "UNSUPPORTED_MEDIA_TYPE", envelopeCode(t, rr))
		})
	}
	for _, path := range []string{"/api/v1/mcp", "/api/v1/federation/push", "/api/v1/objects/x"} {
		method := http.MethodPost
		if path == "/api/v1/objects/x" {
			method = http.MethodPatch
		}
		t.Run(path, func(t *testing.T) {
			rr := serveCase(t, router, csrfCase{method: method, path: path, body: `{}`, contentType: "text/plain"})
			assert.Equal(t, http.StatusUnsupportedMediaType, rr.Code, rr.Body.String())
		})
	}

	items, _, err := bundle.svc.Store.Objects().List(t.Context(), storage.ObjectFilter{})
	require.NoError(t, err)
	assert.Empty(t, items, "a refused request must not store anything")
}

// The clients dpkms serves keep working: the ctxt CLI and federation
// peers (no Origin, JSON), the web UI (same-origin), the browser
// extension (extension origin), the Vite dev server under --dev, and
// bodyless or safe requests.
func TestLegitimateClientsPass(t *testing.T) {
	bundle := newTestServerBundle(t)
	defer bundle.Close()
	router := NewRouterWithConfig(bundle.svc, RouterConfig{})
	devRouter := NewRouterWithConfig(bundle.svc, RouterConfig{DevCORS: true})

	cases := []struct {
		router http.Handler
		csrfCase
	}{
		{router, csrfCase{name: "CLI json", method: http.MethodPost, path: "/api/v1/inbox", body: inboxBody,
			contentType: "application/json", wantStatus: http.StatusCreated}},
		{router, csrfCase{name: "json with charset", method: http.MethodPost, path: "/api/v1/inbox", body: inboxBody,
			contentType: "Application/JSON; charset=utf-8", wantStatus: http.StatusCreated}},
		{router, csrfCase{name: "web UI same-origin", method: http.MethodPost, path: "/api/v1/inbox", body: inboxBody,
			contentType: "application/json", origin: "http://" + testHost, fetchSite: "same-origin", wantStatus: http.StatusCreated}},
		{router, csrfCase{name: "web UI old browser, matching Origin", method: http.MethodPost, path: "/api/v1/inbox", body: inboxBody,
			contentType: "application/json", origin: "http://" + testHost, wantStatus: http.StatusCreated}},
		{router, csrfCase{name: "chrome extension", method: http.MethodPost, path: "/api/v1/capture/page",
			body: `{"url":"https://a.example/p","title":"t","content":"c"}`, contentType: "application/json",
			origin: "chrome-extension://abcdefghijklmnop", fetchSite: "cross-site", wantStatus: http.StatusAccepted}},
		{router, csrfCase{name: "firefox extension", method: http.MethodPost, path: "/api/v1/inbox", body: inboxBody,
			contentType: "application/json", origin: "moz-extension://0f1e2d3c", fetchSite: "cross-site", wantStatus: http.StatusCreated}},
		{router, csrfCase{name: "web UI bodyless delete", method: http.MethodDelete, path: "/api/v1/objects/missing",
			contentType: "application/json", origin: "http://" + testHost, fetchSite: "same-origin", wantStatus: http.StatusNotFound}},
		{router, csrfCase{name: "CLI bodyless retry, no Content-Type", method: http.MethodPost, path: "/api/v1/jobs/missing/retry",
			wantStatus: http.StatusBadRequest, wantCode: "INVALID_REQUEST"}},
		{router, csrfCase{name: "MCP client", method: http.MethodPost, path: "/api/v1/mcp",
			body: `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, contentType: "application/json", wantStatus: http.StatusOK}},
		{router, csrfCase{name: "cross-site read", method: http.MethodGet, path: "/api/v1/objects",
			origin: "https://evil.example", fetchSite: "cross-site", wantStatus: http.StatusOK}},
		{devRouter, csrfCase{name: "vite dev origin with --dev", method: http.MethodPost, path: "/api/v1/inbox", body: inboxBody,
			contentType: "application/json", origin: "http://localhost:5173", fetchSite: "same-site", wantStatus: http.StatusCreated}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.body == inboxBody {
				// Distinct content per case: identical captures collide
				// on the content hash.
				b, err := json.Marshal(map[string]string{"content": tc.name})
				require.NoError(t, err)
				tc.body = string(b)
			}
			rr := serveCase(t, tc.router, tc.csrfCase)
			assert.Equal(t, tc.wantStatus, rr.Code, rr.Body.String())
			if tc.wantCode != "" {
				assert.Equal(t, tc.wantCode, envelopeCode(t, rr))
			}
		})
	}
}

// Federation peers push JSON with no Origin; the guard lets them through
// to the handler.
func TestFederationPushPassesGuard(t *testing.T) {
	ts := newFedTestBundle(t, "")
	defer ts.Close()

	body := makePushBody(t, []storage.KnowledgeObject{makeKO("o1", "h1")}, nil)
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/federation/push", bytes.NewReader(body.Bytes()))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
