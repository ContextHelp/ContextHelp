package rssfeed_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	rssfeed "github.com/ideacrafterslabs/ctxt-plugin-rss-feed"
)

// ─── compile-time interface check ────────────────────────────────────────────

var _ pluginapi.Plugin = (*rssfeed.Plugin)(nil)

// ─── helpers ─────────────────────────────────────────────────────────────────

const sampleRSS = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Test Feed</title>
    <item>
      <title>Item One</title>
      <link>https://example.com/1</link>
      <guid>guid-1</guid>
      <pubDate>Mon, 01 Jan 2024 12:00:00 +0000</pubDate>
      <description>Summary one</description>
    </item>
    <item>
      <title>Item Two</title>
      <link>https://example.com/2</link>
      <guid>guid-2</guid>
      <pubDate>Tue, 02 Jan 2024 12:00:00 +0000</pubDate>
      <description>Summary two</description>
    </item>
  </channel>
</rss>`

const sampleAtom = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <entry>
    <title>Atom Entry One</title>
    <id>urn:uuid:atom-1</id>
    <updated>2024-01-01T12:00:00Z</updated>
    <link rel="alternate" href="https://example.com/atom/1"/>
    <summary>Atom summary one</summary>
  </entry>
</feed>`

// stubBus records published events.
type stubBus struct {
	events []pluginapi.Event
}

func (b *stubBus) Publish(_ context.Context, e pluginapi.Event) error {
	b.events = append(b.events, e)
	return nil
}
func (b *stubBus) Subscribe(_ string, _ func(context.Context, pluginapi.Event) error) {}
func (b *stubBus) Close() error                                                        { return nil }

// ─── unit tests ───────────────────────────────────────────────────────────────

func TestPlugin_Name(t *testing.T) {
	p := rssfeed.New()
	assert.Equal(t, "rss-feed", p.Name())
}

func TestPlugin_Version(t *testing.T) {
	p := rssfeed.New()
	assert.Equal(t, "1.0.0", p.Version())
}

func TestPlugin_PipelineSteps_Empty(t *testing.T) {
	p := rssfeed.New()
	assert.Empty(t, p.PipelineSteps())
}

func TestPlugin_Close_Uninitialised(t *testing.T) {
	p := rssfeed.New()
	require.NoError(t, p.Close(context.Background()))
}

func TestPlugin_Init_IngestsRSSFeed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(sampleRSS))
	}))
	defer srv.Close()

	bus := &stubBus{}
	p := rssfeed.New()
	err := p.Init(context.Background(), map[string]interface{}{
		"feeds":    []interface{}{srv.URL},
		"interval": "1h", // long interval — only initial poll runs
	}, pluginapi.Deps{Bus: bus})
	require.NoError(t, err)

	// Allow the initial poll goroutine to complete.
	time.Sleep(100 * time.Millisecond)

	require.NoError(t, p.Close(context.Background()))

	itemEvents := 0
	for _, e := range bus.events {
		if e.Type == "ctxt.plugin.rss-feed.item" {
			itemEvents++
		}
	}
	assert.Equal(t, 2, itemEvents, "expected 2 item events for 2 RSS items")
}

func TestPlugin_Init_IngestsAtomFeed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/atom+xml")
		_, _ = w.Write([]byte(sampleAtom))
	}))
	defer srv.Close()

	bus := &stubBus{}
	p := rssfeed.New()
	err := p.Init(context.Background(), map[string]interface{}{
		"feeds":    []interface{}{srv.URL},
		"interval": "1h",
	}, pluginapi.Deps{Bus: bus})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)
	require.NoError(t, p.Close(context.Background()))

	itemEvents := 0
	for _, e := range bus.events {
		if e.Type == "ctxt.plugin.rss-feed.item" {
			itemEvents++
		}
	}
	assert.Equal(t, 1, itemEvents, "expected 1 item event for Atom feed")
}

func TestPlugin_MaxItems(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(sampleRSS))
	}))
	defer srv.Close()

	bus := &stubBus{}
	p := rssfeed.New()
	err := p.Init(context.Background(), map[string]interface{}{
		"feeds":     []interface{}{srv.URL},
		"interval":  "1h",
		"max_items": 1,
	}, pluginapi.Deps{Bus: bus})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)
	require.NoError(t, p.Close(context.Background()))

	itemEvents := 0
	for _, e := range bus.events {
		if e.Type == "ctxt.plugin.rss-feed.item" {
			itemEvents++
		}
	}
	assert.Equal(t, 1, itemEvents, "max_items=1 should cap at 1 event")
}

func TestPlugin_DeduplicatesItems(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(sampleRSS))
	}))
	defer srv.Close()

	bus := &stubBus{}
	p := rssfeed.New()
	// Short interval so two polls run quickly.
	err := p.Init(context.Background(), map[string]interface{}{
		"feeds":    []interface{}{srv.URL},
		"interval": "50ms",
	}, pluginapi.Deps{Bus: bus})
	require.NoError(t, err)

	time.Sleep(200 * time.Millisecond)
	require.NoError(t, p.Close(context.Background()))

	itemEvents := 0
	for _, e := range bus.events {
		if e.Type == "ctxt.plugin.rss-feed.item" {
			itemEvents++
		}
	}
	// Feed has 2 items; even with multiple polls, items must not be duplicated.
	assert.Equal(t, 2, itemEvents, "duplicate GUIDs must not re-emit events")
}
