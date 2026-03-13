package http_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	httpserver "github.com/ideacrafterslabs/ctxt/internal/server/http"
)

func TestCORS_ExtensionOrigin(t *testing.T) {
	handler := httpserver.CORS(false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/objects", nil)
	req.Host = "127.0.0.1:8080"
	req.Header.Set("Origin", "chrome-extension://abcdefghijklmnop")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	got := rr.Header().Get("Access-Control-Allow-Origin")
	if got != "chrome-extension://abcdefghijklmnop" {
		t.Errorf("expected CORS header for extension origin, got %q", got)
	}
}

func TestCORS_ExternalOriginBlocked(t *testing.T) {
	handler := httpserver.CORS(false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/objects", nil)
	req.Host = "127.0.0.1:8080"
	req.Header.Set("Origin", "https://evil.example.com")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("expected no CORS header for external origin, got %q", got)
	}
}

func TestCORS_DevOriginBlockedWithoutFlag(t *testing.T) {
	handler := httpserver.CORS(false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/objects", nil)
	req.Host = "127.0.0.1:8080"
	req.Header.Set("Origin", "http://localhost:5173")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("expected no CORS header without --dev flag, got %q", got)
	}
}

func TestCORS_DevOriginAllowedWithFlag(t *testing.T) {
	handler := httpserver.CORS(true)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/objects", nil)
	req.Host = "127.0.0.1:8080"
	req.Header.Set("Origin", "http://localhost:5173")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Errorf("expected CORS header with --dev flag, got %q", got)
	}
}

func TestCORS_RemoteHostIgnored(t *testing.T) {
	handler := httpserver.CORS(true)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/objects", nil)
	req.Host = "example.com"
	req.Header.Set("Origin", "chrome-extension://abc")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("expected no CORS header for remote host, got %q", got)
	}
}
