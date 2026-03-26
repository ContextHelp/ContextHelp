package watcher

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
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
	// StateFile is the path for persisted dedup state. Defaults to
	// ~/.local/share/contexthelp/watcher-state.json.
	StateFile string
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

// clipboardState is the persisted dedup record for the clipboard watcher.
// Raw content is never stored — only the hash and a timestamp.
type clipboardState struct {
	LastHash string    `json:"last_hash"`
	LastSeen time.Time `json:"last_seen"`
}

// clipboardStateFile is the top-level JSON envelope persisted on disk.
type clipboardStateFile struct {
	Clipboard clipboardState `json:"clipboard"`
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
	mu       sync.Mutex
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
// from the last enqueued hash. The hash is persisted to StateFile across
// restarts — raw content is never logged or stored.
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

	// Load persisted state (last hash survives restarts).
	lastHash := cw.loadLastHash()

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

			if !qualifiesForIngest(text, minLen) {
				// Update hash so we don't re-check same short content on every tick.
				lastHash = h
				continue
			}

			lastHash = h
			// Persist hash immediately (before ingest) to prevent re-ingest on restart.
			cw.saveLastHash(h)

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

// loadLastHash reads the persisted clipboard hash from StateFile.
// Returns "" on any error (fresh start).
func (cw *ClipboardWatcher) loadLastHash() string {
	path := cw.resolvedStatePath()
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var sf clipboardStateFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return ""
	}
	return sf.Clipboard.LastHash
}

// saveLastHash persists only the content hash (never raw content) to StateFile.
func (cw *ClipboardWatcher) saveLastHash(h string) {
	cw.mu.Lock()
	defer cw.mu.Unlock()

	path := cw.resolvedStatePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}

	// Read existing file to preserve other watcher states.
	var sf clipboardStateFile
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &sf)
	}

	sf.Clipboard = clipboardState{
		LastHash: h,
		LastSeen: time.Now().UTC(),
	}

	data, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o600)
}

// resolvedStatePath returns the state file path, using default if not set.
func (cw *ClipboardWatcher) resolvedStatePath() string {
	if cw.cfg.StateFile != "" {
		return cw.cfg.StateFile
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "watcher-state.json"
	}
	return filepath.Join(home, ".local", "share", "contexthelp", "watcher-state.json")
}

// detectClipboardType infers a content type for clipboard text.
func detectClipboardType(text string) string {
	trimmed := strings.TrimSpace(text)
	if urlRE.MatchString(trimmed) {
		return "url"
	}
	return "text"
}
