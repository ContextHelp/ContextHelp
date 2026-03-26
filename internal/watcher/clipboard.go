package watcher

import (
	"context"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/ideacrafterslabs/ctxt/internal/service"
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
	ingester Ingester
	read     func() (string, error) // injectable for tests
}

// NewClipboardWatcher creates a ClipboardWatcher.
// reader is the clipboard-read function; pass nil to use the real clipboard.
func NewClipboardWatcher(cfg ClipboardConfig, ingester Ingester, reader func() (string, error)) *ClipboardWatcher {
	if reader == nil {
		reader = clipboard.ReadAll
	}
	return &ClipboardWatcher{cfg: cfg, ingester: ingester, read: reader}
}

// Run polls the clipboard until ctx is cancelled.
// Deduplication: only enqueues when the SHA-256 hash of the content differs
// from the last enqueued hash. The hash is stored in memory only — raw content
// is never logged.
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

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var lastHash string

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

			if !qualifiesForIngest(text, minLen) {
				// Update hash so we don't re-check same short content forever.
				lastHash = h
				continue
			}

			lastHash = h

			if !cw.cfg.AutoIngest || cw.ingester == nil {
				continue
			}

			contentType := detectClipboardType(text)
			req := service.AnalyzeRequest{
				Content:   text,
				Type:      contentType,
				Source:    "watcher/clipboard",
				KnownHash: h,
			}
			_, _ = cw.ingester.Analyze(ctx, req)
		}
	}
}

// detectClipboardType infers a content type for clipboard text.
func detectClipboardType(text string) string {
	trimmed := strings.TrimSpace(text)
	if urlRE.MatchString(trimmed) {
		return "url"
	}
	return "text"
}
