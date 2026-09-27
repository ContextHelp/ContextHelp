package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Every /ui response forbids framing (clickjacking on the SPA's delete
// and retry buttons), MIME sniffing and Referer leaks: the SPA shell,
// its assets, the client-side route fallback and the /ui redirect.
func TestUIResponsesCarrySecurityHeaders(t *testing.T) {
	bundle := newTestServerBundle(t)
	defer bundle.Close()
	router := NewRouterWithConfig(bundle.svc, RouterConfig{})

	for _, path := range []string{"/ui", "/ui/", "/ui/index.html", "/ui/favicon.svg", "/ui/objects/some-id", "/ui/assets/missing.js"} {
		t.Run(path, func(t *testing.T) {
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
			h := rr.Header()
			assert.Equal(t, "frame-ancestors 'none'", h.Get("Content-Security-Policy"))
			assert.Equal(t, "DENY", h.Get("X-Frame-Options"))
			assert.Equal(t, "nosniff", h.Get("X-Content-Type-Options"))
			assert.Equal(t, "no-referrer", h.Get("Referrer-Policy"))
		})
	}
}

// The SPA still loads: its shell and assets answer 200.
func TestUIStillServesSPA(t *testing.T) {
	bundle := newTestServerBundle(t)
	defer bundle.Close()
	router := NewRouterWithConfig(bundle.svc, RouterConfig{})

	for _, path := range []string{"/ui/", "/ui/icons.svg", "/ui/favicon.svg"} {
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		assert.Equal(t, http.StatusOK, rr.Code, path)
	}
}
