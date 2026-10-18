package vision

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const geminiTestKey = "gemini-test-key-0123456789"

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// toServer returns a client that sends every request, whatever its host,
// to srv, so no request leaves the machine.
func toServer(srv *httptest.Server) *http.Client {
	target, _ := url.Parse(srv.URL)
	base := srv.Client().Transport
	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		r = r.Clone(r.Context())
		r.URL.Scheme, r.URL.Host, r.Host = target.Scheme, target.Host, ""
		return base.RoundTrip(r)
	})}
}

// The key goes in the x-goog-api-key header Google documents, never in
// the request URL, where proxies, access logs and client errors repeat it.
func TestGeminiSendsKeyInHeaderNotURL(t *testing.T) {
	var header, rawURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header = r.Header.Get("x-goog-api-key")
		rawURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"a cat Labels: cat"}]}}]}`))
	}))
	defer srv.Close()

	p := NewGeminiProvider(geminiTestKey, "").WithHTTPClient(toServer(srv))
	if _, err := p.Analyze(context.Background(), []byte{0x89, 'P', 'N', 'G'}, "image/png"); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if header != geminiTestKey {
		t.Errorf("x-goog-api-key = %q, want the API key", header)
	}
	if strings.Contains(rawURL, geminiTestKey) {
		t.Errorf("request URL carries the API key: %s", rawURL)
	}
}

// A transport failure is reported without the key: net/http quotes the
// request URL in every client error.
func TestGeminiErrorOmitsKey(t *testing.T) {
	failing := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("connection refused")
	})}
	p := NewGeminiProvider(geminiTestKey, "").WithHTTPClient(failing)
	_, err := p.Analyze(context.Background(), []byte{0x89, 'P', 'N', 'G'}, "image/png")
	if err == nil {
		t.Fatal("Analyze over a failing transport succeeded")
	}
	if strings.Contains(err.Error(), geminiTestKey) {
		t.Errorf("error carries the API key: %v", err)
	}
}
