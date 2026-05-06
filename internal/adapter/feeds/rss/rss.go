// Package rss is the rss backend of the feeds protocol slot.
//
// It is a fetch-only sensor adapter that polls one or more RSS / Atom
// feed URLs and emits one ingest.Object per feed item. Declared
// capabilities: fetch + emit-events. Lifecycle methods are no-ops —
// every Fetch issues a fresh HTTP GET, no persistent connection.
//
// The parser handles both RSS 2.0 (<rss><channel><item>...) and Atom
// 1.0 (<feed><entry>...) by trying RSS first and falling back to Atom
// when the document doesn't decode as RSS. No third-party dependency
// — encoding/xml + a small struct pair is sufficient for the Phase 2
// sensor catalog.
//
// Dedup happens at the runner / storage layer per the existing ingest
// pipeline; the adapter only stamps each Object with a stable ID
// derived from the feed item's guid/id (or content hash if absent).
package rss

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/adapter"
	"github.com/ideacrafterslabs/ctxt/internal/adapter/feeds"
	"github.com/ideacrafterslabs/ctxt/internal/ingest"
)

// Config carries the RSS adapter settings. Zero-value Config returns
// no objects from Fetch (empty FeedURLs is not an error — operators
// may have an entry in policy/ambient.yaml with no feeds yet).
type Config struct {
	// FeedURLs is the list of RSS / Atom feed URLs to poll on each
	// Fetch. Order is preserved in the returned objects.
	FeedURLs []string
	// HTTPTimeout bounds each per-feed GET. Zero means use the default
	// (10s) — generous enough for slow feeds, tight enough that one
	// hung feed doesn't block the sweep indefinitely.
	HTTPTimeout time.Duration
}

// New constructs a typed rss Adapter. The HTTP client is built per
// adapter so test code can override Config.HTTPTimeout without racing
// a package-global.
func New(cfg Config) *Adapter {
	timeout := cfg.HTTPTimeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	return &Adapter{
		cfg:    cfg,
		client: &http.Client{Timeout: timeout},
	}
}

// Adapter is the typed rss sensor adapter.
type Adapter struct {
	cfg    Config
	client *http.Client
}

// Compile-time assertion: Adapter satisfies the typed adapter contract.
var _ adapter.Adapter = (*Adapter)(nil)

// Protocol returns "feeds" — the slot identity defined in
// internal/adapter/feeds/slot.go.
func (a *Adapter) Protocol() string { return feeds.Protocol }

// Backend returns "rss" — the canonical backend identifier (covers
// both RSS and Atom; the operator-facing name biases to RSS since it
// is the more widely-recognized format).
func (a *Adapter) Backend() string { return "rss" }

// Capabilities returns the rss backend's declared capabilities:
// fetch (HTTP GET each feed URL) and emit-events (lifecycle topics).
func (a *Adapter) Capabilities() []adapter.Capability {
	return []adapter.Capability{adapter.CapFetch, adapter.CapEmitEvents}
}

// Start is a no-op — every Fetch issues fresh HTTP GETs, no
// persistent state to set up.
func (a *Adapter) Start(_ context.Context, _ bus.Bus) error { return nil }

// Ready returns true once the adapter is constructed.
func (a *Adapter) Ready() bool { return a.client != nil }

// Drain is a no-op (no in-flight state to flush).
func (a *Adapter) Drain(_ context.Context) error { return nil }

// Stop is a no-op (no resources held).
func (a *Adapter) Stop(_ context.Context) error { return nil }

// Fetch polls every FeedURL in order and returns one ingest.Object per
// feed item. A failure on any single feed is collected via errors.Join
// rather than aborting the sweep — one broken feed should not silence
// the rest.
func (a *Adapter) Fetch(ctx context.Context) ([]ingest.Object, error) {
	var (
		objs []ingest.Object
		errs []error
	)
	for _, url := range a.cfg.FeedURLs {
		items, err := a.fetchFeed(ctx, url)
		if err != nil {
			errs = append(errs, fmt.Errorf("feed %s: %w", url, err))
			continue
		}
		objs = append(objs, items...)
	}
	if len(errs) > 0 {
		return objs, errors.Join(errs...)
	}
	return objs, nil
}

// Submit returns ErrCapabilityNotDeclared — rss is fetch-only.
func (a *Adapter) Submit(_ context.Context, _ ingest.Object) error {
	return adapter.ErrCapabilityNotDeclared
}

// Serve returns ErrCapabilityNotDeclared — rss is fetch-only.
func (a *Adapter) Serve(_ context.Context, _ net.Listener) error {
	return adapter.ErrCapabilityNotDeclared
}

// fetchFeed issues a GET against url and parses the body as RSS 2.0
// or Atom 1.0. The XML root element disambiguates: <rss> → rssDoc,
// <feed> → atomDoc.
func (a *Adapter) fetchFeed(ctx context.Context, url string) ([]ingest.Object, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/xml;q=0.9, */*;q=0.5")
	req.Header.Set("User-Agent", "ctxt-rss-sensor/1.0 (+https://github.com/ideacrafterslabs/ctxt)")
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return parseFeed(url, body)
}

// rssDoc is the minimal RSS 2.0 envelope shape we care about.
type rssDoc struct {
	XMLName xml.Name `xml:"rss"`
	Channel rssChan  `xml:"channel"`
}

type rssChan struct {
	Title string    `xml:"title"`
	Items []rssItem `xml:"item"`
}

type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	GUID        string `xml:"guid"`
	PubDate     string `xml:"pubDate"`
	Description string `xml:"description"`
}

// atomDoc is the minimal Atom 1.0 envelope shape we care about.
type atomDoc struct {
	XMLName xml.Name    `xml:"feed"`
	Title   string      `xml:"title"`
	Entries []atomEntry `xml:"entry"`
}

type atomEntry struct {
	Title   string     `xml:"title"`
	ID      string     `xml:"id"`
	Updated string     `xml:"updated"`
	Summary string     `xml:"summary"`
	Content string     `xml:"content"`
	Links   []atomLink `xml:"link"`
}

type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
}

// parseFeed disambiguates RSS vs Atom by trying RSS first; if the
// outer element doesn't match (xml.UnmarshalTypeError or empty
// channel), retry as Atom. Item-shaped objects emit with Type
// "feed-item".
func parseFeed(feedURL string, body []byte) ([]ingest.Object, error) {
	var rss rssDoc
	if err := xml.Unmarshal(body, &rss); err == nil && len(rss.Channel.Items) > 0 {
		out := make([]ingest.Object, 0, len(rss.Channel.Items))
		for _, it := range rss.Channel.Items {
			out = append(out, rssItemToObject(feedURL, rss.Channel.Title, it))
		}
		return out, nil
	}

	var atom atomDoc
	if err := xml.Unmarshal(body, &atom); err == nil && len(atom.Entries) > 0 {
		out := make([]ingest.Object, 0, len(atom.Entries))
		for _, e := range atom.Entries {
			out = append(out, atomEntryToObject(feedURL, atom.Title, e))
		}
		return out, nil
	}

	return nil, fmt.Errorf("not a recognized RSS or Atom feed")
}

func rssItemToObject(feedURL, channelTitle string, it rssItem) ingest.Object {
	id := strings.TrimSpace(it.GUID)
	if id == "" {
		id = strings.TrimSpace(it.Link)
	}
	if id == "" {
		id = contentHash(it.Title + it.Description)
	}
	return ingest.Object{
		ID:      stableID(feedURL, id),
		Type:    "feed-item",
		Content: it.Description,
		Metadata: map[string]any{
			"feed_url":      feedURL,
			"feed_title":    channelTitle,
			"item_title":    it.Title,
			"item_link":     it.Link,
			"item_guid":     it.GUID,
			"item_pub_date": it.PubDate,
			"format":        "rss",
		},
	}
}

func atomEntryToObject(feedURL, feedTitle string, e atomEntry) ingest.Object {
	id := strings.TrimSpace(e.ID)
	if id == "" {
		// fall back to the first alternate link
		for _, l := range e.Links {
			if l.Rel == "" || l.Rel == "alternate" {
				id = strings.TrimSpace(l.Href)
				break
			}
		}
	}
	if id == "" {
		id = contentHash(e.Title + e.Summary + e.Content)
	}
	body := e.Content
	if body == "" {
		body = e.Summary
	}
	link := ""
	for _, l := range e.Links {
		if l.Rel == "" || l.Rel == "alternate" {
			link = l.Href
			break
		}
	}
	return ingest.Object{
		ID:      stableID(feedURL, id),
		Type:    "feed-item",
		Content: body,
		Metadata: map[string]any{
			"feed_url":     feedURL,
			"feed_title":   feedTitle,
			"item_title":   e.Title,
			"item_link":    link,
			"item_id":      e.ID,
			"item_updated": e.Updated,
			"format":       "atom",
		},
	}
}

// stableID combines feed URL and item identity into a stable, opaque
// ID. The runner / storage layer dedups by this string; the feed URL
// prefix prevents collisions when two unrelated feeds happen to use
// the same guid.
func stableID(feedURL, itemID string) string {
	return contentHash(feedURL + "\x00" + itemID)
}

// contentHash returns a stable, short hex-encoded sha256 prefix of s
// — long enough to make collisions vanishingly rare across realistic
// feed sets, short enough to fit in operator surfaces (object listings,
// log lines).
func contentHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:16])
}
