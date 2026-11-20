package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
	"github.com/ideacrafterslabs/ctxt/internal/watcher"
)

// watch attributes what it submits to the profile it runs under.
func TestWatchIngesterStampsProfile(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"job_id":"j1"}`))
	}))
	defer srv.Close()
	c, err := dpkmsclient.New(dpkmsclient.Endpoint{URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"growth", ""} {
		body = nil
		if _, err := (watchIngester{c: c, profile: want}).Analyze(context.Background(), watcher.Request{Content: "x", Type: "text", Source: "f"}); err != nil {
			t.Fatal(err)
		}
		got, _ := body["profile"].(string)
		if got != want {
			t.Errorf("profile = %q, want %q", got, want)
		}
	}
}
