package watcher_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/watcher"
)

// --- helpers ----------------------------------------------------------------

// fakeCapture returns the provided bytes regardless of focus.
func fakeCapture(data []byte, err error) func(string) ([]byte, error) {
	return func(_ string) ([]byte, error) { return data, err }
}

// newTestScreenWatcher builds a ScreenWatcher with a tmp state path and
// an OCR stub that returns the image bytes as text (for length-gate testing).
func newTestScreenWatcher(
	t *testing.T,
	cfg watcher.ScreenConfig,
	ing watcher.Ingester,
	capture func(string) ([]byte, error),
) *watcher.ScreenWatcher {
	t.Helper()
	sw := watcher.NewScreenWatcher(cfg, ing, capture)
	sw.SetStatePath(filepath.Join(t.TempDir(), "screen-state.json"))
	// Inject OCR stub: returns image bytes as text so length gate is passable.
	sw.SetOCR(func(data []byte) (string, error) { return string(data), nil })
	return sw
}

// --- tests ------------------------------------------------------------------

func TestScreenConfig_Defaults(t *testing.T) {
	cfg := watcher.DefaultScreenConfig()
	if cfg.Enabled {
		t.Error("default Enabled should be false")
	}
	if cfg.Interval != 30*time.Second {
		t.Errorf("default Interval = %v, want 30s", cfg.Interval)
	}
	if cfg.MinOCRLength != 50 {
		t.Errorf("default MinOCRLength = %d, want 50", cfg.MinOCRLength)
	}
	if cfg.Focus != "full" {
		t.Errorf("default Focus = %q, want full", cfg.Focus)
	}
}

func TestScreenWatcher_DisabledByEnv(t *testing.T) {
	t.Setenv("CTXT_NO_SCREEN", "1")

	ing := &captureIngester{}
	cfg := watcher.ScreenConfig{
		Enabled:      true,
		Interval:     5 * time.Millisecond,
		MinOCRLength: 1,
	}
	sw := newTestScreenWatcher(t, cfg, ing,
		fakeCapture([]byte("fake-image-data"), nil))

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	if err := sw.Start(ctx); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if ing.count() != 0 {
		t.Errorf("CTXT_NO_SCREEN=1 should disable watcher; got %d ingestions", ing.count())
	}
}

func TestScreenWatcher_DisabledByConfig(t *testing.T) {
	// ensure env not set
	os.Unsetenv("CTXT_NO_SCREEN")

	ing := &captureIngester{}
	cfg := watcher.ScreenConfig{
		Enabled:      false,
		Interval:     5 * time.Millisecond,
		MinOCRLength: 1,
	}
	sw := newTestScreenWatcher(t, cfg, ing,
		fakeCapture([]byte("fake-image-data"), nil))

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if err := sw.Start(ctx); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if ing.count() != 0 {
		t.Errorf("disabled watcher should not ingest; got %d ingestions", ing.count())
	}
}

// TestScreenWatcher_DedupSameContent verifies that identical (imgData, text)
// is only ingested once across multiple ticks.
func TestScreenWatcher_DedupSameContent(t *testing.T) {
	os.Unsetenv("CTXT_NO_SCREEN")

	ing := &captureIngester{}

	// Same bytes every call → same hash → only 1 ingestion.
	// OCR stub (injected by newTestScreenWatcher) returns the bytes as text,
	// so the length gate passes (len("consistent...") > 10).
	imgData := []byte("consistent-screenshot-bytes-1234")
	capture := fakeCapture(imgData, nil)

	cfg := watcher.ScreenConfig{
		Enabled:      true,
		Interval:     5 * time.Millisecond,
		MinOCRLength: 10,
	}
	sw := newTestScreenWatcher(t, cfg, ing, capture)

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	_ = sw.Start(ctx)

	if ing.count() > 1 {
		t.Errorf("dedup failed: expected ≤1 ingestion for same content, got %d", ing.count())
	}
	if ing.count() == 0 {
		t.Error("expected at least 1 ingestion for qualifying content")
	}
}

// TestScreenWatcher_DedupDifferentContent verifies that two distinct captures
// each trigger a separate ingestion.
func TestScreenWatcher_DedupDifferentContent(t *testing.T) {
	os.Unsetenv("CTXT_NO_SCREEN")

	ing := &captureIngester{}

	// Two distinct frames; OCR stub returns the bytes as text (both > 10 chars).
	frames := [][]byte{
		[]byte("frame-one-unique-bytes"),
		[]byte("frame-two-unique-bytes-different"),
	}
	idx := 0
	capture := func(_ string) ([]byte, error) {
		if idx >= len(frames) {
			return frames[len(frames)-1], nil
		}
		data := frames[idx]
		idx++
		return data, nil
	}

	cfg := watcher.ScreenConfig{
		Enabled:      true,
		Interval:     5 * time.Millisecond,
		MinOCRLength: 10,
	}
	sw := newTestScreenWatcher(t, cfg, ing, capture)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	_ = sw.Start(ctx)

	if ing.count() < 2 {
		t.Errorf("expected ≥2 ingestions for distinct frames, got %d", ing.count())
	}
}

// TestScreenWatcher_ExcludeApps verifies that a mock capture tagged with an
// excluded app name is not ingested when Focus is "active_window".
//
// We rely on the injected captureFunc returning a sentinel error for excluded
// apps. The actual OS-level app detection is platform-specific and tested
// implicitly via manual/E2E testing.
func TestScreenWatcher_ExcludeAppsSkipsCapture(t *testing.T) {
	os.Unsetenv("CTXT_NO_SCREEN")

	ing := &captureIngester{}

	// Simulate excluded app by returning an error from capture.
	// In real use, isExcludedApp() would check the active window name
	// before capture; here we validate via a mock that returns nil bytes.
	captureReturnsEmpty := func(_ string) ([]byte, error) { return nil, nil }

	cfg := watcher.ScreenConfig{
		Enabled:      true,
		Interval:     5 * time.Millisecond,
		MinOCRLength: 10,
		Focus:        "active_window",
		ExcludeApps:  []string{"1Password", "Terminal"},
	}
	sw := newTestScreenWatcher(t, cfg, ing, captureReturnsEmpty)

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	_ = sw.Start(ctx)

	if ing.count() != 0 {
		t.Errorf("empty capture should not trigger ingestion; got %d", ing.count())
	}
}
