package watcher

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/service"
)

// ScreenConfig holds screen watcher configuration.
type ScreenConfig struct {
	// Enabled activates the screen watcher (also blocked by CTXT_NO_SCREEN=1).
	Enabled bool
	// Interval is how often to capture a screenshot. Defaults to 30s.
	Interval time.Duration
	// MinOCRLength is the minimum char count from OCR text to qualify. Defaults to 50.
	MinOCRLength int
	// Focus controls capture scope: "active_window" | "full" | "" (defaults to "full").
	Focus string
	// ExcludeApps lists app names to skip when Focus is "active_window".
	ExcludeApps []string
}

// DefaultScreenConfig returns sensible defaults.
func DefaultScreenConfig() ScreenConfig {
	return ScreenConfig{
		Enabled:      false,
		Interval:     30 * time.Second,
		MinOCRLength: 50,
		Focus:        "full",
	}
}

// screenState is persisted to disk to survive restarts.
type screenState struct {
	SeenHashes map[string]struct{} `json:"-"` // not serialised directly
	Hashes     []string            `json:"hashes"`
}

func loadScreenState(path string) *screenState {
	s := &screenState{SeenHashes: make(map[string]struct{})}
	data, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	var raw struct {
		Hashes []string `json:"hashes"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return s
	}
	for _, h := range raw.Hashes {
		s.SeenHashes[h] = struct{}{}
	}
	s.Hashes = raw.Hashes
	return s
}

func saveScreenState(path string, s *screenState) error {
	hashes := make([]string, 0, len(s.SeenHashes))
	for h := range s.SeenHashes {
		hashes = append(hashes, h)
	}
	s.Hashes = hashes
	data, err := json.Marshal(struct {
		Hashes []string `json:"hashes"`
	}{Hashes: hashes})
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// ScreenWatcher periodically captures a screenshot, runs OCR, deduplicates,
// and enqueues a pipeline job for qualifying content.
type ScreenWatcher struct {
	cfg       ScreenConfig
	ingester  Ingester
	statePath string
	capture   func(focus string) ([]byte, error)  // injectable for tests
	ocr       func(imgData []byte) (string, error) // injectable for tests
}

// NewScreenWatcher creates a ScreenWatcher.
// captureFunc is the screen-capture function; pass nil to use the real one.
func NewScreenWatcher(cfg ScreenConfig, ingester Ingester, captureFunc func(focus string) ([]byte, error)) *ScreenWatcher {
	if captureFunc == nil {
		captureFunc = captureScreen
	}
	home, _ := os.UserHomeDir()
	statePath := filepath.Join(home, ".ctxt", "screen-watcher-state.json")
	return &ScreenWatcher{
		cfg:       cfg,
		ingester:  ingester,
		statePath: statePath,
		capture:   captureFunc,
	}
}

// SetStatePath overrides the default state file path. Useful in tests.
func (sw *ScreenWatcher) SetStatePath(path string) {
	sw.statePath = path
}

// SetOCR overrides the OCR function. Useful in tests to bypass the stub.
func (sw *ScreenWatcher) SetOCR(fn func(imgData []byte) (string, error)) {
	sw.ocr = fn
}

// Start runs the periodic capture loop until ctx is cancelled.
// Returns immediately if CTXT_NO_SCREEN=1 or cfg.Enabled is false.
func (sw *ScreenWatcher) Start(ctx context.Context) error {
	if os.Getenv("CTXT_NO_SCREEN") == "1" {
		return nil
	}
	if !sw.cfg.Enabled {
		return nil
	}

	interval := sw.cfg.Interval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	minLen := sw.cfg.MinOCRLength
	if minLen < 0 {
		minLen = 50
	}

	focus := sw.cfg.Focus
	if focus == "" {
		focus = "full"
	}

	state := loadScreenState(sw.statePath)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			sw.tick(ctx, focus, minLen, state)
		}
	}
}

func (sw *ScreenWatcher) tick(ctx context.Context, focus string, minLen int, state *screenState) {
	imgData, err := sw.capture(focus)
	if err != nil || len(imgData) == 0 {
		return
	}

	if sw.isExcludedApp(focus) {
		return
	}

	var text string
	if sw.ocr != nil {
		var ocrErr error
		text, ocrErr = sw.ocr(imgData)
		if ocrErr != nil {
			return
		}
	} else {
		var extractErr error
		text, extractErr = sw.extractText(imgData)
		if extractErr != nil {
			return
		}
	}

	if len([]rune(strings.TrimSpace(text))) < minLen {
		return
	}

	if sw.dedup(imgData, text, state) {
		return // already seen
	}

	if sw.ingester == nil {
		return
	}

	h := sw.contentHash(imgData, text)
	req := service.AnalyzeRequest{
		Content:   text,
		Type:      "text",
		Source:    "watcher/screen",
		KnownHash: h,
	}
	_, _ = sw.ingester.Analyze(ctx, req)
}

// captureScreen captures a screenshot using platform-native CLI tools.
// On macOS uses `screencapture -x -t png -` (piped to stdout).
// On Linux uses `scrot -` (piped to stdout).
// Screenshots are never written to disk.
func captureScreen(focus string) ([]byte, error) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		args := []string{"-x", "-t", "png", "-"}
		if focus == "active_window" {
			args = append([]string{"-l", "0"}, args...)
		}
		cmd = exec.Command("screencapture", args...)
	case "linux":
		cmd = exec.Command("scrot", "-")
	default:
		return nil, fmt.Errorf("screen capture not supported on %s", runtime.GOOS)
	}
	return cmd.Output()
}

// extractText runs OCR on imgData and returns the extracted text.
// TODO: integrate a real OCR engine (e.g. tesseract via go-tesseract or
// an external process call). Current stub returns empty string.
func (sw *ScreenWatcher) extractText(_ []byte) (string, error) {
	// TODO(T-0155): call tesseract or equivalent OCR engine.
	return "", nil
}

// dedup returns true if this (imgData, text) pair was already seen.
// Uses SHA-256 of text combined with a size-based image fingerprint.
// Persists seen hashes to statePath so dedup survives restarts.
func (sw *ScreenWatcher) dedup(imgData []byte, text string, state *screenState) bool {
	h := sw.contentHash(imgData, text)
	if _, seen := state.SeenHashes[h]; seen {
		return true
	}
	state.SeenHashes[h] = struct{}{}
	_ = saveScreenState(sw.statePath, state)
	return false
}

// contentHash returns a stable hash for dedup: SHA-256(text) + image size fingerprint.
func (sw *ScreenWatcher) contentHash(imgData []byte, text string) string {
	textHash := sha256.Sum256([]byte(text))
	// Image fingerprint: encode size as 8-byte big-endian; cheap, avoids hashing whole image.
	size := len(imgData)
	sizeBytes := []byte{
		byte(size >> 56), byte(size >> 48), byte(size >> 40), byte(size >> 32),
		byte(size >> 24), byte(size >> 16), byte(size >> 8), byte(size),
	}
	combined := append(textHash[:], sizeBytes...)
	final := sha256.Sum256(combined)
	return hex.EncodeToString(final[:])
}

// isExcludedApp checks whether the active app is in the exclusion list.
// Returns false when Focus is not "active_window" or ExcludeApps is empty.
func (sw *ScreenWatcher) isExcludedApp(focus string) bool {
	if focus != "active_window" || len(sw.cfg.ExcludeApps) == 0 {
		return false
	}
	activeApp := activeWindowApp()
	if activeApp == "" {
		return false
	}
	for _, app := range sw.cfg.ExcludeApps {
		if strings.EqualFold(activeApp, app) {
			return true
		}
	}
	return false
}

// activeWindowApp returns the frontmost application name on macOS.
// Returns empty string on unsupported platforms or on error.
func activeWindowApp() string {
	if runtime.GOOS != "darwin" {
		return ""
	}
	script := `tell application "System Events" to get name of first application process whose frontmost is true`
	out, err := exec.Command("osascript", "-e", script).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
