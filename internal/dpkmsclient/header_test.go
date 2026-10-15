package dpkmsclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// WithHeader rides on every request and never replaces the
// credential or the content headers.
func TestWithHeaderOnEveryRequest(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c, err := New(Endpoint{URL: srv.URL, Token: "tok"}, WithHeader("X-Ctxt-View-As", "prn_x"), WithHeader("Authorization", "Bearer nope"))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Get(context.Background(), "/api/v1/objects", nil, nil); err != nil {
		t.Fatal(err)
	}
	if got.Get("X-Ctxt-View-As") != "prn_x" {
		t.Errorf("view-as header = %q", got.Get("X-Ctxt-View-As"))
	}
	if got.Get("Authorization") != "Bearer tok" {
		t.Errorf("authorization = %q, want the endpoint's token", got.Get("Authorization"))
	}
}
