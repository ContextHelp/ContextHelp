// Package screenshot is the ambient screenshot-on-demand Source
// (per ADR-066 + US-0212).
//
// Per ADR-066 §Rationale 5, ctxt explicitly REJECTS continuous periodic
// screen capture (the OpenChronicle model). This source is **explicit-
// trigger only**: events fire when the user presses a hotkey, runs the
// CLI, or (opt-in, future) acts on an auto-detect prompt. There is no
// always-on / interval-poll mode in v1.
//
// Capture is abstracted behind the Capturer interface so:
//   1. Tests inject a fakeCapturer.
//   2. Per-OS implementations (ScreenCaptureKit on macOS, Graphics
//      Capture on Windows, xdg-desktop-portal Screenshot on Linux Wayland,
//      XGetImage on X11) plug in behind build tags.
//   3. Substrate is testable without OS permission prompts.
//
// The Source emits a RawEvent with the captured image file path (not the
// image bytes themselves) so the substrate's image.ocr pipeline can read
// from disk. Files land under <media_dir>/screenshots/ and are evicted
// per the meeting-style retention tier (T-0506) once that ships.
package screenshot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/ambient"
)

const (
	SourceName     = "screenshot"
	DefaultMinOCR  = 80
)

// ErrNotSupported is returned by Capturer implementations on platforms
// where screenshot capture isn't yet implemented.
var ErrNotSupported = errors.New("screenshot: not supported on this platform")

// CaptureRequest describes one screenshot-on-demand. Tests / CLI / hotkey
// listeners populate these fields; the Source feeds them into the
// Capturer.
type CaptureRequest struct {
	// Mode is the capture target.
	Mode CaptureMode
	// WindowTitle, when Mode is CaptureWindow, picks the window via case-
	// insensitive substring match on the active window list.
	WindowTitle string
	// Region, when Mode is CaptureRegion, is the rectangle to capture.
	// (0,0,0,0) prompts the OS for an interactive region picker.
	Region Rect
	// Label is an optional user-supplied label (e.g. "Q3 dashboard").
	// Carried in RawEvent.Metadata for downstream use.
	Label string
	// ForegroundBundleID is the bundle id of the currently-focused app.
	// The Source uses this for the deny-list check BEFORE invoking the
	// Capturer (which would otherwise show an OS permission prompt).
	ForegroundBundleID string
}

// CaptureMode selects what to capture.
type CaptureMode string

const (
	CaptureFullScreen   CaptureMode = "full-screen"
	CaptureActiveWindow CaptureMode = "active-window"
	CaptureWindow       CaptureMode = "window"
	CaptureRegion       CaptureMode = "region"
)

// Rect is a screen-space rectangle in pixels.
type Rect struct {
	X, Y, W, H int
}

// Capturer is the abstraction over OS screenshot APIs.
//
// Capture writes the screenshot image to disk and returns the path + a
// perceptual hash of the image. Implementations decide the file format
// (PNG on macOS Screenshot framework, etc.). Returning ErrNotSupported
// signals an unsupported platform; the Source surfaces it as a failed
// bus event.
type Capturer interface {
	Capture(ctx context.Context, req CaptureRequest) (CaptureResult, error)
}

// CaptureResult describes what the Capturer wrote.
type CaptureResult struct {
	// Path is the absolute path to the image file the Capturer wrote.
	// The substrate enqueues this path; the image.ocr pipeline reads from disk.
	Path string
	// PerceptualHash is a dHash-style fingerprint over the image content.
	// The substrate uses this for dedup (identical-looking captures within
	// the dedup window collapse).
	PerceptualHash string
	// Width / Height in pixels. Optional — used for metadata.
	Width, Height int
}

// Config configures a Source.
type Config struct {
	// MediaDir is the absolute path under which screenshots are written.
	// Capturer impls write to <MediaDir>/screenshots/.
	MediaDir string
	// MinOCRLength filters out empty captures: if the image OCR result
	// (computed downstream by the image.ocr pipeline) is shorter than this,
	// the substrate's post-pipeline logic should skip it. The Source
	// itself doesn't run OCR; this field is carried in RawEvent.Metadata
	// so the pipeline can apply the threshold. Default: 80.
	MinOCRLength int
	// ExcludeBundles is a list of bundle ids that should NEVER produce a
	// screenshot. Source-side deny-list runs BEFORE invoking the Capturer
	// (which would show an OS permission prompt).
	ExcludeBundles []string
}

// Source is the screenshot-on-demand ambient source.
type Source struct {
	capturer Capturer
	cfg      Config
	events   chan ambient.RawEvent
	publisher ambient.Publisher

	mu       sync.Mutex
	started  bool
	stopped  bool
}

// New constructs a Source.
func New(capturer Capturer, cfg Config) (*Source, error) {
	if capturer == nil {
		return nil, fmt.Errorf("screenshot: capturer is required")
	}
	if cfg.MediaDir == "" {
		return nil, fmt.Errorf("screenshot: MediaDir is required")
	}
	if cfg.MinOCRLength <= 0 {
		cfg.MinOCRLength = DefaultMinOCR
	}
	return &Source{
		capturer: capturer,
		cfg:      cfg,
		events:   make(chan ambient.RawEvent, 4),
	}, nil
}

// Name implements ambient.Source.
func (s *Source) Name() string { return SourceName }

// Start implements ambient.Source. The screenshot source is event-driven
// (Trigger calls), not poll-driven, so Start does no real work — it just
// records readiness.
func (s *Source) Start(_ context.Context, b ambient.Publisher) error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return fmt.Errorf("screenshot: already started")
	}
	s.started = true
	s.publisher = b
	s.mu.Unlock()
	if b != nil {
		_ = b.Publish(context.Background(), ambient.SourceLifecycleTopic("ready"), SourceName, nil)
	}
	return nil
}

// Events implements ambient.Source.
func (s *Source) Events() <-chan ambient.RawEvent { return s.events }

// Drain implements ambient.Source.
func (s *Source) Drain(_ context.Context) error { return nil }

// Stop implements ambient.Source.
func (s *Source) Stop(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return nil
	}
	s.stopped = true
	close(s.events)
	return nil
}

// Trigger is the explicit entry point: a hotkey listener / CLI / opt-in
// auto-detect prompt calls Trigger to take a screenshot. Returns the
// emitted RawEvent's fingerprint (or empty string if the request was
// vetoed by the bundle deny-list).
func (s *Source) Trigger(ctx context.Context, req CaptureRequest) (string, error) {
	if s.isExcluded(req.ForegroundBundleID) {
		// Source-side deny: log via bus, do NOT invoke Capturer (which
		// would show an OS permission prompt for nothing).
		if s.publisher != nil {
			_ = s.publisher.Publish(ctx, ambient.EventTopic("filtered"), SourceName,
				map[string]any{"foreground_bundle_id": req.ForegroundBundleID, "reason": "exclude_bundles"})
		}
		return "", nil
	}

	result, err := s.capturer.Capture(ctx, req)
	if err != nil {
		if s.publisher != nil {
			_ = s.publisher.Publish(ctx, ambient.SourceLifecycleTopic("failed"), SourceName,
				map[string]any{"error": err.Error()})
		}
		return "", err
	}

	fp := result.PerceptualHash
	if fp == "" {
		// Capturer didn't supply a perceptual hash; fall back to file-
		// content fingerprint so the substrate's dedup still works.
		fileHash, err := hashFile(result.Path)
		if err == nil {
			fp = fileHash
		}
	}

	meta := map[string]any{
		"file_path":      result.Path,
		"min_ocr_length": s.cfg.MinOCRLength,
	}
	if req.Label != "" {
		meta["label"] = req.Label
	}
	if req.ForegroundBundleID != "" {
		meta["foreground_bundle_id"] = req.ForegroundBundleID
	}
	if result.Width > 0 {
		meta["width"] = result.Width
	}
	if result.Height > 0 {
		meta["height"] = result.Height
	}

	ev := ambient.RawEvent{
		Source:            SourceName,
		OccurredAt:        time.Now(),
		Kind:              ambient.KindImage,
		Payload:           []byte(result.Path),
		Fingerprint:       fp,
		SuggestedPipeline: "image.ocr",
		Metadata:          meta,
	}

	select {
	case s.events <- ev:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	return fp, nil
}

func (s *Source) isExcluded(bundleID string) bool {
	if bundleID == "" {
		return false
	}
	for _, denied := range s.cfg.ExcludeBundles {
		if matchBundle(denied, bundleID) {
			return true
		}
	}
	return false
}

// matchBundle is the same trailing/leading wildcard matcher as the
// foreground source. Reused here for parity in deny-list semantics.
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
	parts := strings.SplitN(pattern, "*", 2)
	return strings.HasPrefix(bundleID, parts[0]) && strings.HasSuffix(bundleID, parts[1])
}

// hashFile returns SHA-256 of file contents. Used as a fingerprint
// fallback when the Capturer doesn't supply a perceptual hash.
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Compile-time check.
var _ ambient.Source = (*Source)(nil)
