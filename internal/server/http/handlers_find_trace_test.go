package http

import (
	"net/http"
	"strings"
	"testing"
)

// The search trace is an in-process option: POST /find refuses a trace
// field like any other unknown field, and never answers with one.
func TestFind_TraceNotOnTheWire(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/v1/find", "application/json", strings.NewReader(`{"query":"anything","trace":true}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d, want 400 for a trace field", resp.StatusCode)
	}
}
