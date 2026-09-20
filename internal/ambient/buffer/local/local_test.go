package local

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/ambient"
)

type fakeClock struct{ ns atomic.Int64 }

func newClock(t time.Time) *fakeClock {
	c := &fakeClock{}
	c.ns.Store(t.UnixNano())
	return c
}
func (c *fakeClock) Now() time.Time          { return time.Unix(0, c.ns.Load()) }
func (c *fakeClock) Advance(d time.Duration) { c.ns.Add(int64(d)) }

func newBuffer(t *testing.T, cfg Config) *Buffer {
	t.Helper()
	if cfg.RootDir == "" {
		cfg.RootDir = t.TempDir()
	}
	b, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return b
}

func TestNew_CreatesRootDir(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "fresh")
	_, err := New(Config{RootDir: dir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("RootDir not created: %v", err)
	}
}

func TestNew_AppliesDefaults(t *testing.T) {
	t.Parallel()
	b := newBuffer(t, Config{RetentionHours: 0, MaxGB: 0})
	if b.cfg.RetentionHours != DefaultRetentionHours {
		t.Errorf("RetentionHours = %d, want %d", b.cfg.RetentionHours, DefaultRetentionHours)
	}
	if b.cfg.MaxGB != DefaultMaxGB {
		t.Errorf("MaxGB = %d, want %d", b.cfg.MaxGB, DefaultMaxGB)
	}
}

func TestAppendThenPopReturnsSameEvent(t *testing.T) {
	t.Parallel()
	b := newBuffer(t, Config{})
	when := time.Date(2026, 5, 5, 14, 0, 0, 0, time.UTC)
	ev := ambient.RawEvent{
		Source:      "clipboard",
		Fingerprint: "fp-1",
		OccurredAt:  when,
		Kind:        ambient.KindText,
		Payload:     []byte("hello"),
	}
	if err := b.Append(context.Background(), ev); err != nil {
		t.Fatalf("Append: %v", err)
	}
	got, err := b.Pop(context.Background())
	if err != nil {
		t.Fatalf("Pop: %v", err)
	}
	if got.Fingerprint != "fp-1" || string(got.Payload) != "hello" {
		t.Errorf("round-trip mismatch: got %+v", got)
	}
}

func TestPopEmptyReturnsErrBufferEmpty(t *testing.T) {
	t.Parallel()
	b := newBuffer(t, Config{})
	if _, err := b.Pop(context.Background()); !errors.Is(err, ambient.ErrBufferEmpty) {
		t.Errorf("Pop on empty: got %v, want ErrBufferEmpty", err)
	}
}

func TestPopReturnsOldestFirst(t *testing.T) {
	t.Parallel()
	b := newBuffer(t, Config{})
	now := time.Date(2026, 5, 5, 14, 0, 0, 0, time.UTC)
	for i, fp := range []string{"a", "b", "c"} {
		ev := ambient.RawEvent{
			Source:      "test",
			Fingerprint: fp,
			OccurredAt:  now.Add(time.Duration(i) * time.Second),
		}
		if err := b.Append(context.Background(), ev); err != nil {
			t.Fatalf("Append: %v", err)
		}
		// Sleep ensures ModTime ordering on filesystems with second resolution.
		time.Sleep(20 * time.Millisecond)
	}
	first, _ := b.Pop(context.Background())
	if first.Fingerprint != "a" {
		t.Errorf("first Pop = %q, want a (oldest)", first.Fingerprint)
	}
}

func TestRangeIteratesWithoutRemoving(t *testing.T) {
	t.Parallel()
	b := newBuffer(t, Config{})
	now := time.Now()
	for _, fp := range []string{"a", "b"} {
		_ = b.Append(context.Background(), ambient.RawEvent{Source: "t", Fingerprint: fp, OccurredAt: now})
	}
	var seen []string
	_ = b.Range(context.Background(), func(ev ambient.RawEvent) bool {
		seen = append(seen, ev.Fingerprint)
		return true
	})
	if len(seen) != 2 {
		t.Errorf("Range yielded %d, want 2", len(seen))
	}
	if got := b.Len(); got != 2 {
		t.Errorf("Range removed events: Len = %d, want 2", got)
	}
}

func TestSweepEvictsExpired(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	clk := newClock(time.Date(2026, 5, 5, 14, 0, 0, 0, time.UTC))
	b := newBuffer(t, Config{
		RootDir:        dir,
		RetentionHours: 24,
		MaxGB:          100,
		Now:            clk.Now,
	})

	// Old event.
	old := ambient.RawEvent{Source: "t", Fingerprint: "old", OccurredAt: clk.Now()}
	_ = b.Append(context.Background(), old)
	// Set ModTime back 48h.
	files, _ := b.listFilesWithMtimes(context.Background())
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	oldTime := clk.Now().Add(-48 * time.Hour)
	_ = os.Chtimes(files[0].Path, oldTime, oldTime)

	// Fresh event.
	clk.Advance(time.Hour)
	fresh := ambient.RawEvent{Source: "t", Fingerprint: "fresh", OccurredAt: clk.Now()}
	_ = b.Append(context.Background(), fresh)

	ttl, capCount, err := b.Sweep(context.Background())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if ttl != 1 {
		t.Errorf("evictedTTL = %d, want 1", ttl)
	}
	if capCount != 0 {
		t.Errorf("evictedCap = %d, want 0", capCount)
	}
	if got := b.Len(); got != 1 {
		t.Errorf("after Sweep, Len = %d, want 1", got)
	}
}

func TestStatsReportsExpectedFields(t *testing.T) {
	t.Parallel()
	b := newBuffer(t, Config{})
	_ = b.Append(context.Background(), ambient.RawEvent{
		Source: "t", Fingerprint: "a", OccurredAt: time.Now(),
	})
	stats := b.Stats()
	if stats.Backend != "local-fs" {
		t.Errorf("Backend = %q, want local-fs", stats.Backend)
	}
	if stats.Count != 1 {
		t.Errorf("Count = %d, want 1", stats.Count)
	}
	if stats.AppendedTotal != 1 {
		t.Errorf("AppendedTotal = %d, want 1", stats.AppendedTotal)
	}
}

func TestResolveDefaultDir_HonoursOverride(t *testing.T) {
	// No t.Parallel() — t.Setenv is incompatible with parallel.
	t.Setenv("CTXT_AMBIENT_BUFFER_DIR", "/tmp/explicit")
	got, err := ResolveDefaultDir()
	if err != nil {
		t.Fatalf("ResolveDefaultDir: %v", err)
	}
	if got != "/tmp/explicit" {
		t.Errorf("ResolveDefaultDir = %q, want /tmp/explicit", got)
	}
}

func TestResolveDefaultDir_HonoursXDG(t *testing.T) {
	t.Setenv("CTXT_AMBIENT_BUFFER_DIR", "")
	t.Setenv("XDG_STATE_HOME", "/xdg/state")
	got, err := ResolveDefaultDir()
	if err != nil {
		t.Fatalf("ResolveDefaultDir: %v", err)
	}
	want := filepath.Join("/xdg/state", "ctxt", "ambient")
	if got != want {
		t.Errorf("ResolveDefaultDir = %q, want %q", got, want)
	}
}

// Compile-time assertion.
var _ ambient.Buffer = (*Buffer)(nil)
