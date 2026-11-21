package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/contentenc"
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

// A binary file the watcher reads reaches dpkms byte for byte: the
// ingester base64-encodes it and the server-side decode restores it.
func TestWatchIngesterSendsBinaryIntact(t *testing.T) {
	bin := []byte{0x25, 'P', 'D', 'F', 0x2d, 0xff, 0xfe, 0x00, 0x80, 0xc3, 0x28}
	path := filepath.Join(t.TempDir(), "doc.pdf")
	if err := os.WriteFile(path, bin, 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
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
	if _, err := (watchIngester{c: c}).Analyze(context.Background(), watcher.Request{Content: string(data), Type: "file", Source: "watch:x"}); err != nil {
		t.Fatal(err)
	}
	enc, _ := body["content_encoding"].(string)
	content, _ := body["content"].(string)
	got, err := contentenc.Decode(content, enc)
	if err != nil || enc != "base64" || !bytes.Equal([]byte(got), bin) {
		t.Fatalf("encoding %q, decoded %x, err %v; want base64 %x", enc, got, err, bin)
	}
}
