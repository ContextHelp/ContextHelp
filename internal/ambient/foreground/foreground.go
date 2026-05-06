// Package foreground is the ambient foreground-window Source
// (per ADR-066 + ADR-067 + US-0215).
//
// The source emits a RawEvent whenever the user's foreground window
// changes (app focus shift). Captures bundle id, app name, and window
// title only — strictly NOT the AX tree, focused-element values, visible
// text, or keystrokes. This matches ctxt's "capture signals, not
// recordings" stance from ADR-066 §Rationale 5.
//
// The source is **macOS-only in v1.** Linux X11/Wayland and Windows
// implementations are tracked separately and return ErrNotSupported via
// build-tagged files.
//
// The session cutter (ADR-067) consumes this source's events to drive
// soft-cut detection (single-app focus) and idle-cut detection (no
// foreground change AND no other capture-worthy events).
//
// Reader interface abstracts OS access so:
//   1. Tests inject a fakeReader.
//   2. macOS implementation lives behind GOOS=darwin build tag.
//   3. Substrate is testable without cgo or AX permissions.
package foreground

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/ambient"
)

// Defaults per ADR-066 / US-0215.
const (
	SourceName            = "foreground"
	DefaultDebounceWindow = 1 * time.Second
)

// ErrNotSupported is returned by Reader implementations on platforms where
// foreground-window detection isn't yet implemented (Linux, Windows).
var ErrNotSupported = errors.New("foreground: not supported on this platform")

// Window is a snapshot of the current foreground window. Reader
// implementations populate these fields; the source maps them into
// RawEvents.
type Window struct {
	BundleID    string
	AppName     string
	WindowTitle string
	// FocusedAt is when the focus change occurred. Reader implementations
	// may use time.Now() if the OS API doesn't expose a precise timestamp.
	FocusedAt time.Time
}

// Reader is the abstraction over OS foreground-window detection. The
// concrete implementation depends on GOOS.
//
// Subscribe registers a callback the OS calls on every focus change. The
// returned cancel function unsubscribes; the source calls it on Stop.
//
// Implementations MUST NOT include AX tree contents, focused-element
// values, visible text, or keystrokes in the Window struct — only bundle
// id, app name, and window title.
type Reader interface {
	Subscribe(ctx context.Context, onChange func(Window)) (cancel func(), err error)
}

// Config configures a Source.
type Config struct {
	// DebounceWindow collapses rapid focus-change bursts (e.g. window
	// cyclers) into one event. Default: 1 second.
	DebounceWindow time.Duration
	// CaptureWindowTitle toggles whether window titles flow through to
	// RawEvent.Metadata. Some users prefer bundle-id only for stronger
	// privacy posture. Default: true.
	CaptureWindowTitle bool
	// ExcludeBundles is a list of bundle ids that should NEVER produce an
	// event (e.g. password managers). Source-side deny-list runs before
	// any bus emission.
	ExcludeBundles []string
}

// Source is the foreground-window ambient source.
type Source struct {
	reader  Reader
	cfg     Config
	events  chan ambient.RawEvent
	publisher ambient.Publisher

	mu        sync.Mutex
	started   bool
	stopped   bool
	stopFunc  context.CancelFunc
	cancelSub func()

	lastBundle    string
	lastEmittedAt time.Time
}

// New constructs a foreground source.
func New(reader Reader, cfg Config) (*Source, error) {
	if reader == nil {
		return nil, fmt.Errorf("foreground: reader is required")
	}
	if cfg.DebounceWindow <= 0 {
		cfg.DebounceWindow = DefaultDebounceWindow
	}
	// CaptureWindowTitle defaults to true; only an explicit false leaves it false.
	// Since bool zero is false, document this as: zero = false. Callers wanting
	// titles must set true. (Mirrors filewatch's MoveAfterEnqueue.)
	return &Source{
		reader: reader,
		cfg:    cfg,
		events: make(chan ambient.RawEvent, 16),
	}, nil
}

// Name implements ambient.Source.
func (s *Source) Name() string { return SourceName }

// Start implements ambient.Source. Subscribes to OS focus-change events.
func (s *Source) Start(ctx context.Context, b ambient.Publisher) error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return fmt.Errorf("foreground: already started")
	}
	s.started = true
	s.publisher = b
	subCtx, cancel := context.WithCancel(ctx)
	s.stopFunc = cancel
	s.mu.Unlock()

	cancelSub, err := s.reader.Subscribe(subCtx, s.onWindowChanged)
	if err != nil {
		s.mu.Lock()
		s.started = false
		s.mu.Unlock()
		if errors.Is(err, ErrNotSupported) {
			if b != nil {
				_ = b.Publish(ctx, ambient.SourceLifecycleTopic("failed"), SourceName,
					map[string]any{"reason": "not_supported_on_platform"})
			}
			return ErrNotSupported
		}
		return fmt.Errorf("foreground: subscribe: %w", err)
	}
	s.mu.Lock()
	s.cancelSub = cancelSub
	s.mu.Unlock()

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
	if s.cancelSub != nil {
		s.cancelSub()
	}
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
	if s.cancelSub != nil {
		s.cancelSub()
	}
	if s.stopFunc != nil {
		s.stopFunc()
	}
	close(s.events)
	return nil
}

// onWindowChanged is the Reader callback. Applies debounce, deny-list,
// and emits a RawEvent.
func (s *Source) onWindowChanged(w Window) {
	if s.isExcluded(w.BundleID) {
		return
	}
	s.mu.Lock()
	now := time.Now()
	if w.BundleID == s.lastBundle && now.Sub(s.lastEmittedAt) < s.cfg.DebounceWindow {
		s.mu.Unlock()
		return
	}
	s.lastBundle = w.BundleID
	s.lastEmittedAt = now
	s.mu.Unlock()

	meta := map[string]any{
		"bundle_id": w.BundleID,
		"app_name":  w.AppName,
	}
	if s.cfg.CaptureWindowTitle && w.WindowTitle != "" {
		meta["window_title"] = w.WindowTitle
	}

	occurredAt := w.FocusedAt
	if occurredAt.IsZero() {
		occurredAt = now
	}

	ev := ambient.RawEvent{
		Source:            SourceName,
		OccurredAt:        occurredAt,
		Kind:              ambient.KindWindowFocus,
		Payload:           []byte(w.BundleID),
		Fingerprint:       fingerprint(w.BundleID, occurredAt),
		SuggestedPipeline: "", // foreground events feed the cutter, not pipelines
		Metadata:          meta,
	}

	// Best-effort send: if substrate is back-pressured, drop and emit
	// failed bus event so observers see the loss. We don't block the OS
	// callback thread.
	select {
	case s.events <- ev:
	default:
		if s.publisher != nil {
			_ = s.publisher.Publish(context.Background(),
				ambient.SourceLifecycleTopic("failed"), SourceName,
				map[string]any{"reason": "events_channel_full"})
		}
	}
}

func (s *Source) isExcluded(bundleID string) bool {
	for _, denied := range s.cfg.ExcludeBundles {
		if matchBundle(denied, bundleID) {
			return true
		}
	}
	return false
}

// matchBundle supports glob-ish patterns ("com.example.*" matches
// "com.example.app"). Trailing-only wildcards keep the implementation
// simple — full glob isn't needed for bundle-id matching.
func matchBundle(pattern, bundleID string) bool {
	if !strings.Contains(pattern, "*") {
		return pattern == bundleID
	}
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(bundleID, prefix)
	}
	if strings.HasPrefix(pattern, "*") {
		suffix := strings.TrimPrefix(pattern, "*")
		return strings.HasSuffix(bundleID, suffix)
	}
	// Middle wildcard.
	parts := strings.SplitN(pattern, "*", 2)
	return strings.HasPrefix(bundleID, parts[0]) && strings.HasSuffix(bundleID, parts[1])
}

// fingerprint returns a stable identifier for (bundle_id, second-bucket).
// Foreground events repeat rapidly when a user toggles between two apps;
// per-second bucketing means the cutter sees one event per app-second.
func fingerprint(bundleID string, t time.Time) string {
	bucket := t.Truncate(time.Second).Unix()
	h := sha256.New()
	fmt.Fprintf(h, "%s|%d", bundleID, bucket)
	return hex.EncodeToString(h.Sum(nil))
}

// Compile-time check.
var _ ambient.Source = (*Source)(nil)
