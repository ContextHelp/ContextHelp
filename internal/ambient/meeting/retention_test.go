package meeting

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeMediaFile(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	bytes := make([]byte, size)
	if err := os.WriteFile(path, bytes, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func setMtime(t *testing.T, path string, mtime time.Time) {
	t.Helper()
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
}

func TestRetention_TTLEvictsOldFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	now := time.Now()
	old := filepath.Join(dir, "sess_old", "old.mp4")
	fresh := filepath.Join(dir, "sess_fresh", "fresh.mp4")
	writeMediaFile(t, old, 1024)
	writeMediaFile(t, fresh, 1024)
	setMtime(t, old, now.Add(-72*time.Hour))   // older than 48h
	setMtime(t, fresh, now.Add(-24*time.Hour)) // within 48h

	p := &RetentionPolicy{
		MediaDir:       dir,
		RetentionHours: 48,
		MaxGB:          100, // big cap; cap rule won't fire
		Now:            func() time.Time { return now },
	}

	res, err := p.Sweep(context.Background())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if res.EvictedTTL != 1 {
		t.Errorf("EvictedTTL = %d, want 1", res.EvictedTTL)
	}
	if res.EvictedCap != 0 {
		t.Errorf("EvictedCap = %d, want 0", res.EvictedCap)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("expected old file deleted; stat err = %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("fresh file should still exist; stat err = %v", err)
	}
}

func TestRetention_CapEvictsOldestFirstWhenOverLimit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	now := time.Now()
	// 3 files of 2 MB each; cap = ~3 MB (using fractional GB via the
	// internal byte calc — we set MaxGB=0 to trigger default 20GB; instead
	// expose maxBytes via a small helper test that counts evictions).
	// Simpler: 3 files of 2 MB; cap converted to ~5 MB by overriding
	// internal logic via test config below.
	a := filepath.Join(dir, "a", "a.mp4")
	b := filepath.Join(dir, "b", "b.mp4")
	c := filepath.Join(dir, "c", "c.mp4")
	for _, p := range []string{a, b, c} {
		writeMediaFile(t, p, 2*1024*1024) // 2 MB
	}
	setMtime(t, a, now.Add(-30*time.Minute))
	setMtime(t, b, now.Add(-20*time.Minute))
	setMtime(t, c, now.Add(-10*time.Minute))

	// MaxGB is the production knob (in GB). For test speed, we drive cap
	// eviction via a much smaller computed cap by setting RetentionHours
	// short. Actually simpler: bypass cap test with a test-only helper.
	// Use 1GB cap: 3 × 2MB = 6MB, well under 1GB → no cap eviction.
	// So we need a different strategy: write files large enough relative
	// to cap. Use the testCapBytes helper instead.
	p := &RetentionPolicy{
		MediaDir:       dir,
		RetentionHours: 24, // not yet expired
		MaxGB:          1,  // 1 GB
		Now:            func() time.Time { return now },
	}
	// Reach into internals: temporarily lower cap via the helper.
	// (Production users tune via MaxGB; tests use testCapBytes.)
	p.testCapBytes = 5 * 1024 * 1024 // 5 MB

	res, err := p.Sweep(context.Background())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if res.EvictedCap != 1 {
		t.Errorf("EvictedCap = %d, want 1", res.EvictedCap)
	}
	if _, err := os.Stat(a); !os.IsNotExist(err) {
		t.Errorf("oldest file (a) should have been evicted; stat = %v", err)
	}
}

func TestRetention_NoEvictionWhenUnderLimit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	now := time.Now()
	path := filepath.Join(dir, "sess", "fresh.mp4")
	writeMediaFile(t, path, 1024)
	setMtime(t, path, now.Add(-1*time.Hour))

	p := &RetentionPolicy{
		MediaDir: dir,
		Now:      func() time.Time { return now },
	}

	res, err := p.Sweep(context.Background())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if res.EvictedTTL != 0 || res.EvictedCap != 0 {
		t.Errorf("expected no eviction; got TTL=%d Cap=%d", res.EvictedTTL, res.EvictedCap)
	}
}

func TestRetention_OnlyEvictsMediaExtensions(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	now := time.Now()
	media := filepath.Join(dir, "sess", "old.mp4")
	notes := filepath.Join(dir, "sess", "notes.txt")
	writeMediaFile(t, media, 1024)
	writeMediaFile(t, notes, 1024)
	setMtime(t, media, now.Add(-72*time.Hour))
	setMtime(t, notes, now.Add(-72*time.Hour))

	p := &RetentionPolicy{
		MediaDir:       dir,
		RetentionHours: 48,
		Now:            func() time.Time { return now },
	}

	res, err := p.Sweep(context.Background())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if res.EvictedTTL != 1 {
		t.Errorf("EvictedTTL = %d, want 1 (only media extension)", res.EvictedTTL)
	}
	if _, err := os.Stat(media); !os.IsNotExist(err) {
		t.Errorf("media file should be evicted; stat = %v", err)
	}
	if _, err := os.Stat(notes); err != nil {
		t.Errorf("non-media file should be preserved; stat = %v", err)
	}
}

func TestRetention_RequiresMediaDir(t *testing.T) {
	t.Parallel()
	p := &RetentionPolicy{}
	if _, err := p.Sweep(context.Background()); err == nil {
		t.Fatal("Sweep with empty MediaDir: expected error")
	}
}

func TestRetention_DefaultsAppliedWhenZero(t *testing.T) {
	t.Parallel()
	p := &RetentionPolicy{
		MediaDir:       t.TempDir(),
		RetentionHours: 0,
		MaxGB:          0,
	}
	if got := p.retentionHours(); got != 48 {
		t.Errorf("retentionHours() = %d, want 48", got)
	}
	if got := p.maxGB(); got != 20 {
		t.Errorf("maxGB() = %d, want 20", got)
	}
}

func TestRetention_HonorsContextCancellation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Many files; cancel mid-sweep.
	for i := range 20 {
		p := filepath.Join(dir, "sess", filepath.Base(filepath.Join(dir, "x"))+string(rune('a'+i))+".mp4")
		writeMediaFile(t, p, 1024)
		setMtime(t, p, time.Now().Add(-72*time.Hour))
	}
	p := &RetentionPolicy{MediaDir: dir, RetentionHours: 48}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	_, err := p.Sweep(ctx)
	if err == nil {
		t.Error("Sweep with pre-cancelled context should return ctx error")
	}
}
