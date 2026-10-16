package watcher

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/atotto/clipboard"
)

// ClipboardConfig holds clipboard watcher configuration.
type ClipboardConfig struct {
	// Enabled activates clipboard polling (also blocked by CTXT_NO_CLIPBOARD=1).
	Enabled bool
	// PollInterval is how often to check the clipboard. Defaults to 2s.
	PollInterval time.Duration
	// MinLength is the minimum character count for plain-text content to qualify.
	// Defaults to 80.
	MinLength int
	// AutoIngest enqueues content immediately without user prompt.
	AutoIngest bool
	// Dedup keeps the hash of the last clip handed off across restarts.
	// nil keeps it in memory only.
	Dedup ClipboardDedup
	// OnError is told about each failed enqueue; nil ignores them.
	OnError func(error)
}

// ClipboardDedup persists the hash of the last clip handed off. Raw
// clipboard content is never stored.
type ClipboardDedup interface {
	ClipboardHash() string
	SetClipboardHash(hash string) error
}

// DefaultClipboardConfig returns a sensible default config.
func DefaultClipboardConfig() ClipboardConfig {
	return ClipboardConfig{
		Enabled:      false,
		PollInterval: 2 * time.Second,
		MinLength:    80,
		AutoIngest:   true,
	}
}

// urlRE matches http(s) URLs.
var urlRE = regexp.MustCompile(`(?i)^https?://\S+$`)

// fencedCodeRE detects fenced code blocks (``` or ~~~).
var fencedCodeRE = regexp.MustCompile("(?m)^(```|~~~)")

// qualifiesForIngest reports whether the clipboard text passes heuristics:
//   - valid URL, OR
//   - fenced code block, OR
//   - length >= minLength
func qualifiesForIngest(text string, minLength int) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	if urlRE.MatchString(trimmed) {
		return true
	}
	if fencedCodeRE.MatchString(trimmed) {
		return true
	}
	return len([]rune(trimmed)) >= minLength
}

// ClipboardWatcher polls the OS clipboard and enqueues analyze jobs when
// content changes and passes heuristics.
type ClipboardWatcher struct {
	cfg      ClipboardConfig
	analyzer Analyzer
	read     func() (string, error) // injectable for tests
}

// NewClipboardWatcher creates a ClipboardWatcher.
// reader is the clipboard-read function; pass nil to use the real clipboard.
func NewClipboardWatcher(cfg ClipboardConfig, analyzer Analyzer, reader func() (string, error)) *ClipboardWatcher {
	if reader == nil {
		reader = clipboard.ReadAll
	}
	return &ClipboardWatcher{cfg: cfg, analyzer: analyzer, read: reader}
}

// Run polls the clipboard until ctx is cancelled.
// Deduplication: only enqueues when the SHA-256 hash of the content differs
// from the last one seen. The hash of the last clip handed off persists
// through Dedup, and only after a successful enqueue, so a clip whose
// enqueue failed is sent again on the next start. Raw content is never
// logged or stored.
func (cw *ClipboardWatcher) Run(ctx context.Context) {
	if os.Getenv("CTXT_NO_CLIPBOARD") == "1" {
		return
	}
	if !cw.cfg.Enabled {
		return
	}

	interval := cw.cfg.PollInterval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	minLen := cw.cfg.MinLength
	if minLen <= 0 {
		minLen = 80
	}

	var lastHash string
	if cw.cfg.Dedup != nil {
		lastHash = cw.cfg.Dedup.ClipboardHash()
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			text, err := cw.read()
			if err != nil || text == "" {
				continue
			}

			h := sha256hex([]byte(text))
			if h == lastHash {
				continue // no change
			}
			// Seen: not re-checked on every tick, whatever happens next.
			lastHash = h

			if !qualifiesForIngest(text, minLen) {
				continue
			}
			if !cw.cfg.AutoIngest || cw.analyzer == nil {
				continue
			}
			cw.handOff(ctx, text, h)
		}
	}
}

// handOff enqueues a qualifying clip whose hash is h, then records h
// through Dedup so the next start does not send it again. A failed
// enqueue records nothing.
func (cw *ClipboardWatcher) handOff(ctx context.Context, text, h string) {
	req := Request{
		Content:   text,
		Type:      detectClipboardType(text),
		Source:    "watcher/clipboard",
		KnownHash: h,
	}
	if _, err := cw.analyzer.Analyze(ctx, req); err != nil {
		if ctx.Err() == nil && cw.cfg.OnError != nil {
			cw.cfg.OnError(fmt.Errorf("clipboard: enqueue: %w", err))
		}
		return
	}
	if cw.cfg.Dedup != nil {
		if err := cw.cfg.Dedup.SetClipboardHash(h); err != nil && cw.cfg.OnError != nil {
			cw.cfg.OnError(fmt.Errorf("clipboard: record hash: %w", err))
		}
	}
}

// detectClipboardType infers a content type for clipboard text.
func detectClipboardType(text string) string {
	trimmed := strings.TrimSpace(text)
	if urlRE.MatchString(trimmed) {
		return "url"
	}
	return contentTypeText
}
