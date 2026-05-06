// Package clipboard is the ambient clipboard Source (per ADR-066 + US-0211).
//
// The clipboard source watches the OS clipboard for text/URL/image changes,
// applies routing heuristics, and emits RawEvents through the substrate's
// AmbientSource interface. The substrate handles redaction → policy filter
// → fingerprint dedup → session-tag → buffer → enqueue.
//
// OS clipboard reading is abstracted behind the Reader interface so:
//
//	1. The first ctxd build can ship without the cgo-required
//	   golang.design/x/clipboard binding (which adds platform-specific
//	   build complexity); a polling Reader stub does the right shape.
//	2. Tests inject a fakeReader that drives clipboard contents without
//	   touching the OS.
//	3. Future Wayland/X11/Windows-specific bindings can replace the macOS
//	   default behind build tags without restructuring the source.
//
// Routing per ADR-066 + US-0211:
//
//	1. Valid http(s) URL → SuggestedPipeline=url.generic, Kind=KindURL
//	2. Fenced code block (``` prefix) → SuggestedPipeline=text.short, Kind=KindText
//	3. Length ≥ MinLength chars → SuggestedPipeline=text.short, Kind=KindText
//	4. Otherwise: skip (no RawEvent emitted)
//
// Fingerprint = SHA-256 hex of normalized (trimmed) payload bytes. Used by
// the substrate's enqueue-boundary dedup to drop re-copies of identical
// content within the configured window.
package clipboard

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/ambient"
)

// Default config values per ADR-066 + US-0211.
const (
	DefaultPollInterval = 2 * time.Second
	DefaultMinLength    = 80
	SourceName          = "clipboard"
)

// Reader is the abstraction over OS clipboard access. Production builds
// wrap golang.design/x/clipboard or a platform-specific binding; tests
// inject a stub.
//
// ReadText returns the current clipboard text content. An empty string
// means "no text on clipboard right now" (image-only clipboards, etc.).
// Returning an error is a hard failure (lib not initialised, OS denied,
// etc.) and bubbles up to the caller.
type Reader interface {
	ReadText(ctx context.Context) (string, error)
}

// Config holds tunables for the clipboard source. Zero values fall back to
// the documented defaults.
type Config struct {
	// PollInterval is the cadence at which the source checks the OS
	// clipboard. Default: 2s.
	PollInterval time.Duration
	// MinLength filters out short copies (single-word selections, accidental
	// triggers). Length is measured in runes after trimming whitespace.
	// Default: 80.
	MinLength int
	// RouteUrlsTo overrides the pipeline name for URL-shaped payloads.
	// Default: "url.generic".
	RouteUrlsTo string
	// RouteTextTo overrides the pipeline name for text payloads.
	// Default: "text.short".
	RouteTextTo string
}

// Source is the clipboard ambient source. It polls Reader at the configured
// interval, applies routing heuristics, and emits RawEvents until Stop is
// called.
//
// Source satisfies ambient.Source; the Runner consumes it via
// runner.Register(clipboard.New(...)).
type Source struct {
	reader   Reader
	cfg      Config
	events   chan ambient.RawEvent
	publisher ambient.Publisher

	mu       sync.Mutex
	started  bool
	stopped  bool
	stopFunc context.CancelFunc

	lastEmitted string // last emitted normalized payload (Source-side suppression)
}

// New constructs a clipboard Source. Reader is required; cfg may be the
// zero value (defaults applied).
func New(reader Reader, cfg Config) (*Source, error) {
	if reader == nil {
		return nil, fmt.Errorf("clipboard: reader is required")
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = DefaultPollInterval
	}
	if cfg.MinLength <= 0 {
		cfg.MinLength = DefaultMinLength
	}
	if cfg.RouteUrlsTo == "" {
		cfg.RouteUrlsTo = "url.generic"
	}
	if cfg.RouteTextTo == "" {
		cfg.RouteTextTo = "text.short"
	}
	return &Source{
		reader: reader,
		cfg:    cfg,
		events: make(chan ambient.RawEvent, 8),
	}, nil
}

// Name implements ambient.Source. Returns SourceName ("clipboard").
func (s *Source) Name() string { return SourceName }

// Start implements ambient.Source. Begins the poll loop in a goroutine that
// runs until ctx is cancelled or Stop is called. Multiple Starts on the
// same Source are rejected.
func (s *Source) Start(ctx context.Context, b ambient.Publisher) error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return fmt.Errorf("clipboard: already started")
	}
	s.started = true
	s.publisher = b
	pollCtx, cancel := context.WithCancel(ctx)
	s.stopFunc = cancel
	s.mu.Unlock()

	if b != nil {
		_ = b.Publish(ctx, ambient.SourceLifecycleTopic("ready"), SourceName, nil)
	}
	go s.pollLoop(pollCtx)
	return nil
}

// Events implements ambient.Source.
func (s *Source) Events() <-chan ambient.RawEvent { return s.events }

// Drain implements ambient.Source. Stops the poll loop and lets in-flight
// events drain through Events(). Channel closure is deferred to Stop so
// the Runner's fan-in goroutine sees a clean close.
func (s *Source) Drain(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopFunc != nil {
		s.stopFunc()
	}
	return nil
}

// Stop implements ambient.Source. Hard-stop; closes the events channel and
// idempotent across multiple calls.
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

// pollLoop is the main work loop. Ticks at PollInterval, reads the OS
// clipboard, applies heuristics, and emits RawEvents.
func (s *Source) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

// tick performs one clipboard read + emit. Side effects: updates
// lastEmitted; sends on s.events; never blocks the OS reader thread.
func (s *Source) tick(ctx context.Context) {
	text, err := s.reader.ReadText(ctx)
	if err != nil {
		// Reader-side failure (OS denied, lib uninitialised). Surface via
		// bus event but don't crash the loop.
		if s.publisher != nil {
			_ = s.publisher.Publish(ctx, ambient.SourceLifecycleTopic("failed"), SourceName,
				map[string]any{"error": err.Error()})
		}
		return
	}
	normalized := strings.TrimSpace(text)
	if normalized == "" {
		return
	}
	// Source-side suppression: skip if we just emitted the same content.
	// (Substrate dedup also catches this; Source-side suppression saves a
	// cycle of policy-filter + dedup-lookup in the hot path.)
	s.mu.Lock()
	if normalized == s.lastEmitted {
		s.mu.Unlock()
		return
	}
	s.lastEmitted = normalized
	s.mu.Unlock()

	kind, pipeline, ok := s.route(normalized)
	if !ok {
		return // skipped per heuristic (length / shape)
	}

	ev := ambient.RawEvent{
		Source:            SourceName,
		OccurredAt:        time.Now(),
		Kind:              kind,
		Payload:           []byte(normalized),
		Fingerprint:       fingerprint([]byte(normalized)),
		SuggestedPipeline: pipeline,
	}

	select {
	case s.events <- ev:
	case <-ctx.Done():
	}
}

// route applies the four-step heuristic from US-0211 / ADR-066. Returns
// (Kind, pipeline, true) when the payload should be emitted; (_, _, false)
// when it should be skipped (length / no shape match).
func (s *Source) route(payload string) (ambient.Kind, string, bool) {
	// 1. URL: starts with http:// or https://, no spaces.
	if isURL(payload) {
		return ambient.KindURL, s.cfg.RouteUrlsTo, true
	}
	// 2. Fenced code block: starts with ``` (with or without language tag).
	if strings.HasPrefix(payload, "```") {
		return ambient.KindText, s.cfg.RouteTextTo, true
	}
	// 3. Length threshold.
	if len([]rune(payload)) >= s.cfg.MinLength {
		return ambient.KindText, s.cfg.RouteTextTo, true
	}
	// 4. Skip.
	return "", "", false
}

// isURL is the light-touch URL check matching US-0211's heuristic. We
// don't full-parse to avoid emitting unwanted RawEvents for things that
// merely contain a URL (e.g. an email body); the substrate-side enricher
// (text.short pipeline) will extract URLs from longer text on its own.
func isURL(s string) bool {
	if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
		return false
	}
	if strings.ContainsAny(s, " \t\n") {
		return false
	}
	return len(s) >= len("http://x")
}

// fingerprint returns the SHA-256 hex of payload. Substrate uses this for
// enqueue-boundary dedup.
func fingerprint(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

// Compile-time assertion: *Source satisfies ambient.Source.
var _ ambient.Source = (*Source)(nil)
