// Package rssfeed is a reference ingestion plugin that polls one or more
// RSS 2.0 / Atom 1.0 feeds on a configurable schedule and publishes each
// new item as a KnowledgeObject via the pluginapi event bus.
//
// Config (under plugins.rss-feed):
//
//	feeds:
//	  - https://example.com/feed.xml
//	interval: 15m          # polling interval (default 15m)
//	max_items: 50          # cap per feed per cycle (0 = unlimited)
//	user_agent: "my-bot/1" # HTTP User-Agent (optional)
//	timeout_seconds: 30    # HTTP timeout (default 30)
package rssfeed

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// Plugin implements pluginapi.Plugin.
type Plugin struct {
	cfg  Config
	deps pluginapi.Deps

	mu   sync.Mutex
	seen map[string]struct{} // GUIDs ingested this session

	cancel context.CancelFunc
	done   chan struct{}
}

// New returns an uninitialised Plugin for registration.
func New() *Plugin { return &Plugin{} }

// ── pluginapi.Plugin ──────────────────────────────────────────────────────────

func (p *Plugin) Name() string    { return "rss-feed" }
func (p *Plugin) Version() string { return "1.0.0" }

// Init reads config and starts the polling goroutine.
func (p *Plugin) Init(ctx context.Context, raw map[string]interface{}, deps pluginapi.Deps) error {
	cfg, err := ConfigFromMap(raw)
	if err != nil {
		return fmt.Errorf("rss-feed: config: %w", err)
	}
	p.cfg = cfg
	p.deps = deps
	p.seen = make(map[string]struct{})
	p.done = make(chan struct{})

	pctx, cancel := context.WithCancel(ctx)
	p.cancel = cancel
	go p.loop(pctx)
	return nil
}

// PipelineSteps returns nothing — this plugin is ingestion-only.
func (p *Plugin) PipelineSteps() []pluginapi.PipelineStep { return nil }

// Close stops the polling loop and waits for it to exit.
func (p *Plugin) Close(_ context.Context) error {
	if p.cancel != nil {
		p.cancel()
		<-p.done
	}
	return nil
}

// ── internals ─────────────────────────────────────────────────────────────────

func (p *Plugin) loop(ctx context.Context) {
	defer close(p.done)

	// Run immediately on start, then tick.
	p.poll(ctx)

	ticker := time.NewTicker(p.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.poll(ctx)
		}
	}
}

func (p *Plugin) poll(ctx context.Context) {
	for _, url := range p.cfg.Feeds {
		if err := p.ingestFeed(ctx, url); err != nil {
			// Log-level error — don't crash the loop.
			_ = p.publish(ctx, pluginapi.Event{
				ID:          sha256hex(url + "error"),
				Source:      "plugin/rss-feed",
				SpecVersion: "1.0",
				Type:        "ctxt.plugin.rss-feed.error",
				Time:        time.Now().UTC(),
				Data:        mustJSON(map[string]string{"url": url, "error": err.Error()}),
			})
		}
	}
}

func (p *Plugin) ingestFeed(ctx context.Context, feedURL string) error {
	items, err := p.fetchFeed(ctx, feedURL)
	if err != nil {
		return err
	}

	cap := p.cfg.MaxItems
	count := 0
	for _, item := range items {
		if cap > 0 && count >= cap {
			break
		}
		if p.alreadySeen(item.GUID) {
			continue
		}
		obj := itemToObject(item, feedURL)
		if err := p.publish(ctx, objectEvent(obj)); err != nil {
			return err
		}
		p.markSeen(item.GUID)
		count++
	}
	return nil
}

func (p *Plugin) fetchFeed(ctx context.Context, url string) ([]feedItem, error) {
	client := &http.Client{
		Timeout: time.Duration(p.cfg.TimeoutSeconds) * time.Second,
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("rss-feed: build request for %s: %w", url, err)
	}
	req.Header.Set("User-Agent", p.cfg.UserAgent)
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/xml, text/xml")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rss-feed: fetch %s: %w", url, err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rss-feed: fetch %s: HTTP %d", url, resp.StatusCode)
	}
	return parseFeed(resp.Body)
}

func (p *Plugin) alreadySeen(guid string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := p.seen[guid]
	return ok
}

func (p *Plugin) markSeen(guid string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.seen[guid] = struct{}{}
}

func (p *Plugin) publish(ctx context.Context, e pluginapi.Event) error {
	if p.deps.Bus == nil {
		return nil
	}
	return p.deps.Bus.Publish(ctx, e)
}

// ── helpers ───────────────────────────────────────────────────────────────────

func itemToObject(item feedItem, feedURL string) pluginapi.KnowledgeObject {
	now := time.Now().UTC()
	pub := item.Published
	if pub.IsZero() {
		pub = now
	}

	// Build text content: title + summary/content.
	parts := []string{}
	if item.Title != "" {
		parts = append(parts, item.Title)
	}
	body := item.Content
	if body == "" {
		body = item.Summary
	}
	if body != "" {
		parts = append(parts, body)
	}
	text := strings.Join(parts, "\n\n")

	meta := map[string]any{
		"feed_url":   feedURL,
		"item_url":   item.Link,
		"guid":       item.GUID,
		"published":  pub.Format(time.RFC3339),
	}
	if item.Author != "" {
		meta["author"] = item.Author
	}

	obj := pluginapi.KnowledgeObject{
		ID:          sha256hex(item.GUID),
		Type:        "url",
		Subtype:     "rss-item",
		RawContent:  item.Link,
		TextContent: text,
		ContentType: "text/html",
		Source:      feedURL,
		Metadata:    meta,
		ContentHash: sha256hex(text),
		CreatedAt:   pub,
		UpdatedAt:   now,
	}
	if item.Title != "" {
		obj.Sections = []pluginapi.Section{
			{Title: "Title", Content: item.Title, Order: 0},
		}
		if body != "" {
			obj.Sections = append(obj.Sections, pluginapi.Section{
				Title: "Body", Content: body, Order: 1,
			})
		}
	}
	return obj
}

func objectEvent(obj pluginapi.KnowledgeObject) pluginapi.Event {
	return pluginapi.Event{
		ID:              obj.ID,
		Source:          "plugin/rss-feed",
		SpecVersion:     "1.0",
		Type:            "ctxt.plugin.rss-feed.item",
		DataContentType: "application/json",
		Time:            time.Now().UTC(),
		Data:            mustJSON(obj),
	}
}

func sha256hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", sum)
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v) //nolint:errcheck
	return b
}
