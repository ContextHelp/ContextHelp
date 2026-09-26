// Package browserhistory is the ambient browser-history Source
// (per ADR-066 + US-0214).
//
// The source polls one or more browsers' history at a configurable interval,
// emits RawEvents for visits newer than the last-seen timestamp, applies
// URL-pattern allow/deny rules (package urlfilter), and persists the last-seen timestamp per
// browser so daemon restart doesn't replay old visits.
//
// Browser-side history reading is abstracted behind the BrowserClient
// interface so:
//
//  1. Tests inject a fakeBrowserClient with controlled visit lists.
//  2. Per-browser SQLite-poll implementations (Chrome, Firefox, Safari,
//     Edge, Brave, Arc) plug in behind build tags without restructuring
//     the Source.
//  3. The substrate is testable without CGO sqlite or per-OS path
//     resolution.
//
// Routing per ADR-066 + US-0214:
//
//	URL host matches github.com|gitlab.com|bitbucket.org → url.repo
//	otherwise                                            → url.generic
//
// Search-engine URLs (google.com/search, duckduckgo.com/?q=, etc.) are
// tagged with subtype=search-query so search-engine visits are
// distinguishable from content visits.
package browserhistory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/ambient"
	"github.com/ideacrafterslabs/ctxt/internal/urlfilter"
)

// Defaults per ADR-066 / US-0214.
const (
	SourceName           = "browserhistory"
	DefaultPollInterval  = 5 * time.Minute
	defaultEventChanSize = 32
)

// Visit is a single browser history entry. BrowserClient implementations
// emit these; Source consumes them.
type Visit struct {
	URL       string
	Title     string
	VisitedAt time.Time
	// Browser identifies the source browser ("chrome", "firefox", ...).
	Browser string
	// Source is the Name() of the client that read the visit, e.g.
	// "brave:Profile 3", naming the profile as well as the browser.
	// Empty when the client does not set it.
	Source string
}

// BrowserClient is the abstraction over per-browser SQLite history reads.
//
// VisitsSince returns visits with VisitedAt > since, ordered oldest-first.
// Implementations should return a small batch and let the Source poll
// again — no cursor/streaming needed.
type BrowserClient interface {
	// Name identifies the browser (e.g. "chrome", "firefox", "safari").
	// Source uses this for last-seen-timestamp persistence and for routing
	// metadata.
	Name() string
	// VisitsSince returns visits newer than `since`, oldest-first.
	VisitsSince(ctx context.Context, since time.Time) ([]Visit, error)
}

// Config configures a Source.
type Config struct {
	// Browsers is the set of BrowserClients to poll. At least one required.
	Browsers []BrowserClient
	// PollInterval is how often each browser is polled. Default: 5 minutes.
	PollInterval time.Duration
	// Filter drops visits before emit. Compile it with urlfilter.New or
	// urlfilter.Config.For. Nil means the builtin deny rules only;
	// unparseable URLs are dropped either way.
	Filter *urlfilter.Filter
}

// Source is the browser-history ambient source.
type Source struct {
	cfg       Config
	events    chan ambient.RawEvent
	publisher ambient.Publisher

	mu       sync.Mutex
	started  bool
	stopped  bool
	stopFunc context.CancelFunc

	// lastSeen[browser_name] = last visit timestamp emitted.
	lastSeen map[string]time.Time
}

// New constructs a browser-history Source.
func New(cfg Config) (*Source, error) {
	if len(cfg.Browsers) == 0 {
		return nil, fmt.Errorf("browserhistory: at least one browser is required")
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = DefaultPollInterval
	}
	if cfg.Filter == nil {
		f, err := urlfilter.Config{}.For("")
		if err != nil {
			return nil, fmt.Errorf("browserhistory: builtin url filter: %w", err)
		}
		cfg.Filter = f
	}
	return &Source{
		cfg:      cfg,
		events:   make(chan ambient.RawEvent, defaultEventChanSize),
		lastSeen: make(map[string]time.Time),
	}, nil
}

// Name implements ambient.Source.
func (s *Source) Name() string { return SourceName }

// Start implements ambient.Source. Spawns one poll goroutine per browser.
func (s *Source) Start(ctx context.Context, b ambient.Publisher) error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return fmt.Errorf("browserhistory: already started")
	}
	s.started = true
	s.publisher = b
	pollCtx, cancel := context.WithCancel(ctx)
	s.stopFunc = cancel
	s.mu.Unlock()

	for _, browser := range s.cfg.Browsers {
		go s.pollLoop(pollCtx, browser)
	}

	if b != nil {
		_ = b.Publish(ctx, ambient.SourceLifecycleTopic("ready"), SourceName, nil)
	}
	return nil
}

// Events implements ambient.Source.
func (s *Source) Events() <-chan ambient.RawEvent { return s.events }

// Drain implements ambient.Source.
func (s *Source) Drain(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopFunc != nil {
		s.stopFunc()
	}
	return nil
}

// Stop implements ambient.Source.
func (s *Source) Stop(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return nil
	}
	s.stopped = true
	if s.stopFunc != nil {
		s.stopFunc()
	}
	close(s.events)
	return nil
}

// pollLoop drives one browser's poll cadence.
func (s *Source) pollLoop(ctx context.Context, browser BrowserClient) {
	// First poll fires immediately so visits accumulated while ctxd was off
	// don't wait the full interval.
	s.tick(ctx, browser)
	ticker := time.NewTicker(s.cfg.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick(ctx, browser)
		}
	}
}

// tick polls one browser and emits any new visits.
func (s *Source) tick(ctx context.Context, browser BrowserClient) {
	since := s.getLastSeen(browser.Name())
	visits, err := browser.VisitsSince(ctx, since)
	if err != nil {
		if s.publisher != nil {
			_ = s.publisher.Publish(ctx, ambient.SourceLifecycleTopic("failed"), SourceName,
				map[string]any{"browser": browser.Name(), "error": err.Error()})
		}
		return
	}
	for _, v := range visits {
		if d := s.cfg.Filter.Evaluate(v.URL); !d.Allowed {
			// The payload names the rule, never the URL: a denied URL is
			// exactly what must not leave the source.
			if s.publisher != nil {
				_ = s.publisher.Publish(ctx, ambient.EventTopic("filtered"), SourceName,
					map[string]any{
						"browser": browser.Name(),
						"reason":  string(d.Reason),
						"rule":    d.Rule.Pattern,
						"scope":   d.Rule.Scope,
					})
			}
			s.bumpLastSeen(browser.Name(), v.VisitedAt)
			continue
		}
		ev := s.toRawEvent(v)
		select {
		case s.events <- ev:
		case <-ctx.Done():
			return
		}
		s.bumpLastSeen(browser.Name(), v.VisitedAt)
	}
}

// toRawEvent converts a Visit into a substrate RawEvent.
func (s *Source) toRawEvent(v Visit) ambient.RawEvent {
	pipeline := routePipeline(v.URL)
	subtype := ""
	if isSearchQuery(v.URL) {
		subtype = "search-query"
	}
	meta := map[string]any{
		"browser":    v.Browser,
		"title":      v.Title,
		"visited_at": v.VisitedAt.Unix(),
	}
	if subtype != "" {
		meta["subtype"] = subtype
	}
	if v.Source != "" {
		meta["browser_profile"] = v.Source
	}
	return ambient.RawEvent{
		Source:            SourceName,
		OccurredAt:        v.VisitedAt,
		Kind:              ambient.KindURL,
		Payload:           []byte(v.URL),
		Fingerprint:       fingerprint(v.URL, v.VisitedAt),
		SuggestedPipeline: pipeline,
		Metadata:          meta,
	}
}

// routePipeline picks url.repo for known code-host domains, url.generic
// otherwise.
func routePipeline(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "url.generic"
	}
	host := strings.ToLower(u.Host)
	switch {
	case strings.HasSuffix(host, "github.com"),
		strings.HasSuffix(host, "gitlab.com"),
		strings.HasSuffix(host, "bitbucket.org"):
		return "url.repo"
	}
	return "url.generic"
}

// isSearchQuery reports whether the URL is a search-engine query result.
func isSearchQuery(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Host)
	switch {
	case strings.Contains(host, "google.") && strings.HasPrefix(u.Path, "/search"):
		return true
	case strings.Contains(host, "duckduckgo.") && u.Query().Get("q") != "":
		return true
	case strings.Contains(host, "bing.") && strings.HasPrefix(u.Path, "/search"):
		return true
	case strings.Contains(host, "kagi.") && strings.HasPrefix(u.Path, "/search"):
		return true
	}
	return false
}

// fingerprint returns a stable identifier for (url, visit_minute_bucket).
// Bucketing to the minute means polling the same visit twice (within the
// minute) produces identical fingerprints — substrate dedup catches the
// duplicate.
func fingerprint(rawURL string, visitedAt time.Time) string {
	bucket := visitedAt.Truncate(time.Minute).Unix()
	h := sha256.New()
	fmt.Fprintf(h, "%s|%d", rawURL, bucket)
	return hex.EncodeToString(h.Sum(nil))
}

func (s *Source) getLastSeen(browser string) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastSeen[browser]
}

func (s *Source) bumpLastSeen(browser string, t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t.After(s.lastSeen[browser]) {
		s.lastSeen[browser] = t
	}
}

// SetLastSeen seeds the last-seen timestamp for a browser. Used by callers
// that persist state across daemon restarts: load from disk on start, then
// SetLastSeen so the first poll doesn't re-emit historical visits.
func (s *Source) SetLastSeen(browser string, t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastSeen[browser] = t
}

// LastSeen returns the current last-seen timestamp for a browser. Used for
// status / persistence.
func (s *Source) LastSeen(browser string) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastSeen[browser]
}

// Compile-time check.
var _ ambient.Source = (*Source)(nil)
