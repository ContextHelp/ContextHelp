package rss

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/adapter"
	"github.com/ideacrafterslabs/ctxt/internal/ingest"
)

// TestAdapterIdentity locks the protocol and backend identifiers the
// substrate's Registry uses to enforce one-platform-per-protocol.
func TestAdapterIdentity(t *testing.T) {
	a := New(Config{})
	if got := a.Protocol(); got != "feeds" {
		t.Errorf("Protocol = %q, want feeds", got)
	}
	if got := a.Backend(); got != "rss" {
		t.Errorf("Backend = %q, want rss", got)
	}
}

// TestAdapterDeclaresFetchOnly confirms the rss adapter is
// capability-honest: it only declares fetch + emit-events. The
// substrate's gating depends on this honesty so undeclared methods
// fail loud rather than silently no-oping.
func TestAdapterDeclaresFetchOnly(t *testing.T) {
	a := New(Config{})
	if !adapter.HasCapability(a, adapter.CapFetch) {
		t.Error("missing CapFetch")
	}
	if !adapter.HasCapability(a, adapter.CapEmitEvents) {
		t.Error("missing CapEmitEvents")
	}
	for _, c := range a.Capabilities() {
		if c == adapter.CapServe || c == adapter.CapSubmit {
			t.Errorf("unexpected capability %q on fetch-only sensor", c)
		}
	}
}

// TestAdapterRejectsUndeclaredCapabilities pins the substrate contract:
// methods for capabilities NOT declared MUST return
// ErrCapabilityNotDeclared (errors.Is matches).
func TestAdapterRejectsUndeclaredCapabilities(t *testing.T) {
	a := New(Config{})
	if err := a.Submit(context.Background(), ingest.Object{ID: "x"}); !errors.Is(err, adapter.ErrCapabilityNotDeclared) {
		t.Errorf("Submit: want ErrCapabilityNotDeclared, got %v", err)
	}
	if err := a.Serve(context.Background(), nil); !errors.Is(err, adapter.ErrCapabilityNotDeclared) {
		t.Errorf("Serve: want ErrCapabilityNotDeclared, got %v", err)
	}
}

// TestFetchRSS exercises the happy path against a fake httptest.Server
// that returns a fixture RSS 2.0 feed. The adapter should produce one
// ingest.Object per <item>.
func TestFetchRSS(t *testing.T) {
	const body = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Example Feed</title>
    <link>https://example.com/</link>
    <description>An example feed.</description>
    <item>
      <title>First post</title>
      <link>https://example.com/1</link>
      <guid>https://example.com/1</guid>
      <pubDate>Mon, 05 May 2026 10:00:00 GMT</pubDate>
      <description>First body.</description>
    </item>
    <item>
      <title>Second post</title>
      <link>https://example.com/2</link>
      <guid>https://example.com/2</guid>
      <pubDate>Mon, 05 May 2026 11:00:00 GMT</pubDate>
      <description>Second body.</description>
    </item>
  </channel>
</rss>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	a := New(Config{FeedURLs: []string{srv.URL}})
	objs, err := a.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(objs) != 2 {
		t.Fatalf("Fetch: got %d objects, want 2", len(objs))
	}
	if objs[0].Type != "feed-item" {
		t.Errorf("Type = %q, want feed-item", objs[0].Type)
	}
	if objs[0].ID == "" || objs[0].ID == objs[1].ID {
		t.Errorf("expected distinct non-empty IDs, got %q and %q", objs[0].ID, objs[1].ID)
	}
}

// TestFetchAtom exercises the parser on an Atom 1.0 feed. RSS and Atom
// share the same protocol slot per the spec; the adapter handles both.
func TestFetchAtom(t *testing.T) {
	const body = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>Atom Example</title>
  <id>https://example.com/atom</id>
  <updated>2026-05-05T10:00:00Z</updated>
  <entry>
    <title>Atom post</title>
    <id>tag:example.com,2026:atom-1</id>
    <link href="https://example.com/atom-1"/>
    <updated>2026-05-05T10:00:00Z</updated>
    <summary>Atom body.</summary>
  </entry>
</feed>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/atom+xml")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	a := New(Config{FeedURLs: []string{srv.URL}})
	objs, err := a.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(objs) != 1 {
		t.Fatalf("Fetch: got %d objects, want 1", len(objs))
	}
	if objs[0].Type != "feed-item" {
		t.Errorf("Type = %q, want feed-item", objs[0].Type)
	}
}
